package service

import (
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Anthropic Messages 流协议对 content block 的生命周期有硬约束：同一时刻最多一个块
// 处于打开状态，上一个块 content_block_stop 之后才允许 content_block_start 下一个，
// 且对客户端呈现的 index 必须从 0 连续递增。
//
// 部分上游不守这个约束。实测 GLM-5.2 在长输出（含 reasoning）时会：
//   - 块 1 还没发 content_block_stop 就 start 块 2（ContentBlockStartWhileBlockOpen）
//   - 往已经关闭的块补发 content_block_delta（DeltaAfterStop）
//
// 严格校验的客户端（MetaCode）会直接判定流非法并中断输出，重试也只是再撞一次同类畸形流，
// 用户侧表现为硬失败。网关的透传路径按行原样转发，畸形流原样到达客户端。
//
// anthropicBlockGuard 在网关出口做块配平，一处收口保护所有下游客户端：
//   - content_block_start 时若还有未关闭的块，先补发那个块的 content_block_stop
//   - content_block_delta 落在没打开的块上时，能安全合成块头的（text_delta）就补一个
//     content_block_start，补不了的（thinking/signature/tool 参数）才丢弃
//   - content_block_stop 指向没打开的块时丢弃
//   - message_delta / message_stop / error 之前若还有未关闭的块，补发 stop
//   - 对客户端呈现的 index 由网关自己按 0,1,2… 分配；上游 index 与之不一致时改写
//
// 上游协议合规时所有分支都不触发：事件原样透传，不做任何 JSON 改写。
type anthropicBlockGuard struct {
	activeIn  int // 上游视角下当前未关闭块的 index，-1 表示没有打开的块
	activeOut int // 该块对客户端呈现的 index
	nextOut   int // 下一个对客户端分配的 index

	onRepair func(reason string, index int)
}

// 修正原因，同时作为 metrics 标签值使用（有界小词表）。
const (
	blockRepairMissingStop      = "missing_stop"       // 上游漏发 content_block_stop
	blockRepairOrphanDelta      = "orphan_delta"       // delta 指向未打开/已关闭的块，且无法合成块头
	blockRepairSynthesizedStart = "synthesized_start"  // delta 指向未打开的块，补了 content_block_start
	blockRepairOrphanStop       = "orphan_stop"        // stop 指向未打开/已关闭的块
	blockRepairIndexRemap       = "index_remap"        // 上游 index 跳号，改写为连续值
	blockRepairUnterminated     = "unterminated_block" // 流结束时仍有未关闭的块
)

func newAnthropicBlockGuard(onRepair func(reason string, index int)) *anthropicBlockGuard {
	return &anthropicBlockGuard{activeIn: -1, activeOut: -1, onRepair: onRepair}
}

// blockGuardAction 描述守卫对单个 SSE 事件的处置。
type blockGuardAction struct {
	// InjectStopIndex >= 0 时，需要在该事件之前补发这个 index 的 content_block_stop。
	InjectStopIndex int
	// InjectStartType 非空时，需要在该事件之前补发一个该类型的 content_block_start
	// （index 取 RewriteIndex 或事件自身的 index）。排在 InjectStopIndex 之后。
	InjectStartType string
	// RewriteIndex >= 0 时，需要把事件里的 index 字段改写成这个值。
	RewriteIndex int
	// Drop 为 true 时丢弃该事件（此时其余字段无意义）。
	Drop bool
}

func passThroughAction() blockGuardAction {
	return blockGuardAction{InjectStopIndex: -1, RewriteIndex: -1}
}

// anthropicSynthesizableBlockType 判断一个 delta 子类型能不能安全地反推出块头。
// text_delta 可以（空 text 块）；thinking/signature 反推出来的是没有 signature 的
// thinking 块，客户端回传时会被上游拒；input_json_delta 缺 tool id/name 根本拼不出来。
func anthropicSynthesizableBlockType(deltaType string) string {
	if deltaType == "text_delta" {
		return "text"
	}
	return ""
}

// anthropicBlockGuardTracksEvent 判断事件类型是否需要走守卫状态机。
// 其余类型（ping / content_block_delta 之外的自定义事件等）原样透传。
func anthropicBlockGuardTracksEvent(eventType string) bool {
	switch eventType {
	case "content_block_start", "content_block_delta", "content_block_stop",
		"message_start", "message_delta", "message_stop", "error":
		return true
	default:
		return false
	}
}

func (g *anthropicBlockGuard) repair(reason string, index int) {
	if g.onRepair != nil {
		g.onRepair(reason, index)
	}
}

// next 推进状态机。index 为事件里的 index 字段，缺失时传 -1；
// deltaType 只在 content_block_delta 上有意义（delta.type），其余事件传空串即可。
func (g *anthropicBlockGuard) next(eventType string, index int, deltaType string) blockGuardAction {
	act := passThroughAction()

	switch eventType {
	case "content_block_start":
		if index < 0 {
			// 没有 index 的 content_block_start 已经不成形，交给客户端自行判断。
			return act
		}
		if g.activeIn >= 0 {
			act.InjectStopIndex = g.activeOut
			g.repair(blockRepairMissingStop, g.activeOut)
			g.activeIn = -1
		}
		out := g.nextOut
		g.nextOut++
		g.activeIn = index
		g.activeOut = out
		if out != index {
			act.RewriteIndex = out
			g.repair(blockRepairIndexRemap, out)
		}

	case "content_block_delta":
		if index < 0 {
			return act
		}
		if g.activeIn < 0 || index != g.activeIn {
			// 上游从没给这个块发过 content_block_start（有的兼容上游干脆只发 delta），
			// 或者块已经关了又补发 delta。能补块头就补，别把内容整段吞掉。
			blockType := anthropicSynthesizableBlockType(deltaType)
			if blockType == "" {
				act.Drop = true
				g.repair(blockRepairOrphanDelta, index)
				return act
			}
			if g.activeIn >= 0 {
				act.InjectStopIndex = g.activeOut
				g.repair(blockRepairMissingStop, g.activeOut)
				g.activeIn = -1
			}
			out := g.nextOut
			g.nextOut++
			g.activeIn = index
			g.activeOut = out
			act.InjectStartType = blockType
			if out != index {
				act.RewriteIndex = out
			}
			g.repair(blockRepairSynthesizedStart, out)
			return act
		}
		if g.activeOut != index {
			act.RewriteIndex = g.activeOut
		}

	case "content_block_stop":
		if index < 0 {
			return act
		}
		if g.activeIn < 0 || index != g.activeIn {
			act.Drop = true
			g.repair(blockRepairOrphanStop, index)
			return act
		}
		if g.activeOut != index {
			act.RewriteIndex = g.activeOut
		}
		g.activeIn = -1

	case "message_start":
		// 同一条流里再来一个 message_start，index 从头开始编号。
		if g.activeIn >= 0 {
			act.InjectStopIndex = g.activeOut
			g.repair(blockRepairMissingStop, g.activeOut)
			g.activeIn = -1
		}
		g.nextOut = 0

	case "message_delta", "message_stop", "error":
		if g.activeIn >= 0 {
			act.InjectStopIndex = g.activeOut
			g.repair(blockRepairMissingStop, g.activeOut)
			g.activeIn = -1
		}
	}

	return act
}

// finish 在流结束时调用：上游没关的块由网关补上 stop，避免客户端看到悬空的块。
func (g *anthropicBlockGuard) finish() (int, bool) {
	if g.activeIn < 0 {
		return 0, false
	}
	idx := g.activeOut
	g.activeIn = -1
	g.repair(blockRepairUnterminated, idx)
	return idx, true
}

// anthropicBlockGuardWriter 把守卫接到逐行透传的写出路径上。
//
// 事件类型要到 data: 行才能确定，而合成的 content_block_stop 必须排在
// event: 行之前，所以这里把 event: 行扣住一行，等 data: 行揭晓后再决定
// 是先补 stop、还是整条事件丢弃。非事件行（空行/注释/其他字段）直接放行。
type anthropicBlockGuardWriter struct {
	guard *anthropicBlockGuard

	pendingEvent string
	hasPending   bool
	// dropBlank 标记上一条事件被丢弃，其结尾空行也要一并吞掉，
	// 免得客户端收到一个空事件块。
	dropBlank bool
}

func newAnthropicBlockGuardWriter(onRepair func(reason string, index int)) *anthropicBlockGuardWriter {
	return &anthropicBlockGuardWriter{guard: newAnthropicBlockGuard(onRepair)}
}

func (w *anthropicBlockGuardWriter) flushPending(emit func(string)) {
	if !w.hasPending {
		return
	}
	line := w.pendingEvent
	w.pendingEvent = ""
	w.hasPending = false
	emit(line)
}

func (w *anthropicBlockGuardWriter) write(line string, emit func(string)) {
	if strings.HasPrefix(line, "event:") {
		w.flushPending(emit)
		w.pendingEvent = line
		w.hasPending = true
		w.dropBlank = false
		return
	}

	data, ok := extractAnthropicSSEDataLine(line)
	if !ok {
		if w.dropBlank && strings.TrimSpace(line) == "" {
			w.dropBlank = false
			return
		}
		w.dropBlank = false
		w.flushPending(emit)
		emit(line)
		return
	}
	w.dropBlank = false

	payload := strings.TrimSpace(data)
	if payload == "" || payload[0] != '{' {
		w.flushPending(emit)
		emit(line)
		return
	}
	eventType := gjson.Get(payload, "type").String()
	if !anthropicBlockGuardTracksEvent(eventType) {
		w.flushPending(emit)
		emit(line)
		return
	}

	index := -1
	if v := gjson.Get(payload, "index"); v.Exists() {
		index = int(v.Int())
	}
	deltaType := ""
	if eventType == "content_block_delta" {
		deltaType = gjson.Get(payload, "delta.type").String()
	}

	act := w.guard.next(eventType, index, deltaType)
	if act.InjectStopIndex >= 0 {
		writeAnthropicBlockStopLines(act.InjectStopIndex, emit)
	}
	if act.Drop {
		w.pendingEvent = ""
		w.hasPending = false
		w.dropBlank = true
		return
	}
	if act.InjectStartType != "" {
		startIndex := index
		if act.RewriteIndex >= 0 {
			startIndex = act.RewriteIndex
		}
		writeAnthropicBlockStartLines(startIndex, act.InjectStartType, emit)
	}
	w.flushPending(emit)
	if act.RewriteIndex >= 0 {
		if patched, err := sjson.Set(payload, "index", act.RewriteIndex); err == nil {
			emit("data: " + patched)
			return
		}
	}
	emit(line)
}

// finish 在上游流读完后调用，补齐未关闭的块。
func (w *anthropicBlockGuardWriter) finish(emit func(string)) {
	w.flushPending(emit)
	w.dropBlank = false
	if idx, ok := w.guard.finish(); ok {
		writeAnthropicBlockStopLines(idx, emit)
	}
}

// writeAnthropicBlockStopLines 写出一个完整的合成 content_block_stop 事件（含结尾空行）。
func writeAnthropicBlockStopLines(index int, emit func(string)) {
	emit("event: content_block_stop")
	emit("data: " + anthropicBlockStopPayload(index))
	emit("")
}

func anthropicBlockStopPayload(index int) string {
	return `{"type":"content_block_stop","index":` + strconv.Itoa(index) + `}`
}

// writeAnthropicBlockStartLines 写出一个完整的合成 content_block_start 事件（含结尾空行）。
func writeAnthropicBlockStartLines(index int, blockType string, emit func(string)) {
	emit("event: content_block_start")
	emit("data: " + anthropicBlockStartPayload(index, blockType))
	emit("")
}

// anthropicBlockStartPayload 目前只用于合成 text 块（见 anthropicSynthesizableBlockType）。
func anthropicBlockStartPayload(index int, blockType string) string {
	return `{"type":"content_block_start","index":` + strconv.Itoa(index) +
		`,"content_block":{"type":"` + blockType + `","text":""}}`
}

// anthropicBlockStartEventBlock 返回一个完整的合成 content_block_start 事件块文本。
func anthropicBlockStartEventBlock(index int, blockType string) string {
	return "event: content_block_start\ndata: " + anthropicBlockStartPayload(index, blockType) + "\n\n"
}

// anthropicBlockStopEventBlock 返回一个完整的合成 content_block_stop 事件块文本。
func anthropicBlockStopEventBlock(index int) string {
	return "event: content_block_stop\ndata: " + anthropicBlockStopPayload(index) + "\n\n"
}

// applyAnthropicBlockGuardToBlocks 给已经组装成整块文本的 SSE 事件套用块配平守卫，
// 供按事件块（而非按行）写出的路径使用：必要时在前面插入合成的 content_block_stop、
// 改写 index，或整块丢弃。data 是该事件最终写出的 JSON（可能已被上层改写过）。
func applyAnthropicBlockGuardToBlocks(guard *anthropicBlockGuard, blocks []string, data string) []string {
	if guard == nil || len(blocks) == 0 {
		return blocks
	}
	payload := strings.TrimSpace(data)
	if payload == "" || payload[0] != '{' {
		return blocks
	}
	eventType := gjson.Get(payload, "type").String()
	if !anthropicBlockGuardTracksEvent(eventType) {
		return blocks
	}
	index := -1
	if v := gjson.Get(payload, "index"); v.Exists() {
		index = int(v.Int())
	}
	deltaType := ""
	if eventType == "content_block_delta" {
		deltaType = gjson.Get(payload, "delta.type").String()
	}

	act := guard.next(eventType, index, deltaType)
	if act.Drop {
		return nil
	}
	out := blocks
	if act.RewriteIndex >= 0 {
		if patched, err := sjson.Set(payload, "index", act.RewriteIndex); err == nil {
			out = make([]string, len(blocks))
			for i, b := range blocks {
				out[i] = strings.Replace(b, "data: "+data, "data: "+patched, 1)
			}
		}
	}
	// 合成事件按 stop → start → 原事件的顺序排在前面。
	if act.InjectStartType != "" {
		startIndex := index
		if act.RewriteIndex >= 0 {
			startIndex = act.RewriteIndex
		}
		out = append([]string{anthropicBlockStartEventBlock(startIndex, act.InjectStartType)}, out...)
	}
	if act.InjectStopIndex >= 0 {
		out = append([]string{anthropicBlockStopEventBlock(act.InjectStopIndex)}, out...)
	}
	return out
}
