//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// collectGuardOutput 把若干输入行喂给守卫，返回写出的行。
func collectGuardOutput(lines []string, repairs *[]string) []string {
	var out []string
	emit := func(line string) { out = append(out, line) }
	w := newAnthropicBlockGuardWriter(func(reason string, index int) {
		if repairs != nil {
			*repairs = append(*repairs, reason)
		}
	})
	for _, line := range lines {
		w.write(line, emit)
	}
	w.finish(emit)
	return out
}

func sseEventLines(eventName, data string) []string {
	return []string{"event: " + eventName, "data: " + data, ""}
}

// 协议合规的流必须逐行原样透传：守卫不改写、不注入、不丢弃。
func TestAnthropicBlockGuard_CompliantStreamPassesThroughUnchanged(t *testing.T) {
	var in []string
	in = append(in, sseEventLines("message_start", `{"type":"message_start","message":{"id":"msg_1"}}`)...)
	in = append(in, sseEventLines("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)...)
	in = append(in, sseEventLines("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`)...)
	in = append(in, sseEventLines("content_block_stop", `{"type":"content_block_stop","index":0}`)...)
	in = append(in, sseEventLines("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`)...)
	in = append(in, sseEventLines("message_stop", `{"type":"message_stop"}`)...)

	var repairs []string
	out := collectGuardOutput(in, &repairs)

	require.Equal(t, in, out)
	require.Empty(t, repairs, "合规流不应触发任何修正")
}

// 本次事故的形态：块 1 未关闭就 start 块 2。守卫应在 start 之前补发块 1 的 stop。
func TestAnthropicBlockGuard_InjectsStopOnStartWhileBlockOpen(t *testing.T) {
	var in []string
	in = append(in, sseEventLines("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`)...)
	in = append(in, sseEventLines("content_block_stop", `{"type":"content_block_stop","index":0}`)...)
	in = append(in, sseEventLines("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`)...)
	in = append(in, sseEventLines("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"a"}}`)...)
	// 上游漏了块 1 的 content_block_stop，直接 start 块 2。
	in = append(in, sseEventLines("content_block_start", `{"type":"content_block_start","index":2,"content_block":{"type":"text","text":""}}`)...)
	in = append(in, sseEventLines("content_block_delta", `{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"b"}}`)...)
	in = append(in, sseEventLines("content_block_stop", `{"type":"content_block_stop","index":2}`)...)
	in = append(in, sseEventLines("message_stop", `{"type":"message_stop"}`)...)

	var repairs []string
	out := collectGuardOutput(in, &repairs)
	body := strings.Join(out, "\n")

	assertAnthropicBlockSequenceValid(t, body)
	require.Equal(t, []string{blockRepairMissingStop}, repairs)
	require.Equal(t, 3, strings.Count(body, `"type":"content_block_stop"`), "块 0/1/2 各应有一个 stop")
	// 补发的 stop 必须排在下一个 start 之前。
	require.Less(t,
		strings.Index(body, `{"type":"content_block_stop","index":1}`),
		strings.Index(body, `"content_block_start","index":2`),
	)
}

// 往已关闭的块补发 text delta（DeltaAfterStop）：文本不能丢，给它补一个 content_block_start。
func TestAnthropicBlockGuard_SynthesizesStartForTextDeltaAfterStop(t *testing.T) {
	var in []string
	in = append(in, sseEventLines("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)...)
	in = append(in, sseEventLines("content_block_stop", `{"type":"content_block_stop","index":0}`)...)
	in = append(in, sseEventLines("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"late"}}`)...)
	in = append(in, sseEventLines("message_stop", `{"type":"message_stop"}`)...)

	var repairs []string
	out := collectGuardOutput(in, &repairs)
	body := strings.Join(out, "\n")

	assertAnthropicBlockSequenceValid(t, body)
	require.Equal(t, []string{blockRepairSynthesizedStart, blockRepairMissingStop}, repairs)
	require.Contains(t, body, `"text":"late"`, "文本内容不能被吞掉")
	require.Contains(t, body, `"content_block_start","index":1`, "越界的 text delta 应改挂到新块上")
}

// 只发 delta、从不发 content_block_start 的兼容上游：整段文本必须照样送达。
func TestAnthropicBlockGuard_SynthesizesStartForDeltaOnlyStream(t *testing.T) {
	var in []string
	in = append(in, sseEventLines("message_start", `{"type":"message_start","message":{"id":"msg_1"}}`)...)
	in = append(in, sseEventLines("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`)...)
	in = append(in, sseEventLines("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" there"}}`)...)
	in = append(in, sseEventLines("message_stop", `{"type":"message_stop"}`)...)

	var repairs []string
	out := collectGuardOutput(in, &repairs)
	body := strings.Join(out, "\n")

	assertAnthropicBlockSequenceValid(t, body)
	require.Equal(t, []string{blockRepairSynthesizedStart, blockRepairMissingStop}, repairs)
	require.Contains(t, body, `"text":"hi"`)
	require.Contains(t, body, `"text":" there"`)
}

// thinking/tool 类 delta 反推不出合法块头（thinking 缺 signature、tool_use 缺 id/name），
// 越界时只能丢弃，且不留下空事件块。
func TestAnthropicBlockGuard_DropsNonSynthesizableDeltaAfterStop(t *testing.T) {
	var in []string
	in = append(in, sseEventLines("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`)...)
	in = append(in, sseEventLines("content_block_stop", `{"type":"content_block_stop","index":0}`)...)
	in = append(in, sseEventLines("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"late"}}`)...)
	in = append(in, sseEventLines("message_stop", `{"type":"message_stop"}`)...)

	var repairs []string
	out := collectGuardOutput(in, &repairs)
	body := strings.Join(out, "\n")

	assertAnthropicBlockSequenceValid(t, body)
	require.Equal(t, []string{blockRepairOrphanDelta}, repairs)
	require.NotContains(t, body, "late")
	require.NotContains(t, body, "event: content_block_delta", "被丢弃事件的 event: 行也不应写出")
}

// 指向未打开块的 content_block_stop 同样丢弃（否则客户端会看到重复 stop）。
func TestAnthropicBlockGuard_DropsOrphanStop(t *testing.T) {
	var in []string
	in = append(in, sseEventLines("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)...)
	in = append(in, sseEventLines("content_block_stop", `{"type":"content_block_stop","index":0}`)...)
	in = append(in, sseEventLines("content_block_stop", `{"type":"content_block_stop","index":0}`)...)
	in = append(in, sseEventLines("message_stop", `{"type":"message_stop"}`)...)

	var repairs []string
	out := collectGuardOutput(in, &repairs)
	body := strings.Join(out, "\n")

	assertAnthropicBlockSequenceValid(t, body)
	require.Equal(t, []string{blockRepairOrphanStop}, repairs)
	require.Equal(t, 1, strings.Count(body, `"type":"content_block_stop"`))
}

// 上游 index 跳号时改写为连续值，块内的 delta/stop 一并跟随改写。
func TestAnthropicBlockGuard_RemapsNonContiguousIndex(t *testing.T) {
	var in []string
	in = append(in, sseEventLines("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)...)
	in = append(in, sseEventLines("content_block_stop", `{"type":"content_block_stop","index":0}`)...)
	// 上游从 0 直接跳到 5。
	in = append(in, sseEventLines("content_block_start", `{"type":"content_block_start","index":5,"content_block":{"type":"text","text":""}}`)...)
	in = append(in, sseEventLines("content_block_delta", `{"type":"content_block_delta","index":5,"delta":{"type":"text_delta","text":"x"}}`)...)
	in = append(in, sseEventLines("content_block_stop", `{"type":"content_block_stop","index":5}`)...)
	in = append(in, sseEventLines("message_stop", `{"type":"message_stop"}`)...)

	var repairs []string
	out := collectGuardOutput(in, &repairs)
	body := strings.Join(out, "\n")

	assertAnthropicBlockSequenceValid(t, body)
	require.Equal(t, []string{blockRepairIndexRemap}, repairs)
	require.NotContains(t, body, `"index":5`)
	require.Contains(t, body, `"text":"x"`, "改写 index 不应丢内容")
}

// message_stop 之前若还有未关闭的块，补发 stop。
func TestAnthropicBlockGuard_ClosesOpenBlockBeforeMessageStop(t *testing.T) {
	var in []string
	in = append(in, sseEventLines("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)...)
	in = append(in, sseEventLines("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"a"}}`)...)
	in = append(in, sseEventLines("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`)...)
	in = append(in, sseEventLines("message_stop", `{"type":"message_stop"}`)...)

	var repairs []string
	out := collectGuardOutput(in, &repairs)
	body := strings.Join(out, "\n")

	assertAnthropicBlockSequenceValid(t, body)
	require.Equal(t, []string{blockRepairMissingStop}, repairs)
	require.Less(t,
		strings.Index(body, `"type":"content_block_stop"`),
		strings.Index(body, `"type":"message_delta"`),
	)
}

// 流被截断（没有 message_stop）时，finish 补齐未关闭的块。
func TestAnthropicBlockGuard_ClosesUnterminatedBlockOnFinish(t *testing.T) {
	var in []string
	in = append(in, sseEventLines("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)...)
	in = append(in, sseEventLines("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"a"}}`)...)

	var repairs []string
	out := collectGuardOutput(in, &repairs)

	require.Equal(t, []string{blockRepairUnterminated}, repairs)
	require.Contains(t, strings.Join(out, "\n"), `{"type":"content_block_stop","index":0}`)
}

// 只有 data: 行、没有 event: 行的上游（非规范但常见）同样要被守卫覆盖。
func TestAnthropicBlockGuard_HandlesDataOnlyStream(t *testing.T) {
	in := []string{
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		"",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"a"}}`,
		"",
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		"",
		`data: {"type":"content_block_stop","index":1}`,
		"",
		`data: {"type":"message_stop"}`,
		"",
	}

	var repairs []string
	out := collectGuardOutput(in, &repairs)

	assertAnthropicBlockSequenceValid(t, strings.Join(out, "\n"))
	require.Equal(t, []string{blockRepairMissingStop}, repairs)
}

// ping / 未知事件 / [DONE] 原样透传，不进状态机。
func TestAnthropicBlockGuard_PassesThroughUntrackedEvents(t *testing.T) {
	in := []string{
		"event: ping",
		`data: {"type":"ping"}`,
		"",
		": keepalive comment",
		"",
		"data: [DONE]",
		"",
	}

	var repairs []string
	out := collectGuardOutput(in, &repairs)

	require.Equal(t, in, out)
	require.Empty(t, repairs)
}

// 端到端：GLM-5.2 实测形态（thinking 块正常关闭 → text 块输出中 → 未关就 start 下一个 text 块，
// 期间还往已关闭块补发 delta）经透传路径后，应变成协议合规的流。
func TestHandleStreamingResponseAnthropicAPIKeyPassthrough_RepairsMalformedBlockSequence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newMinimalGatewayService()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}

	go func() {
		defer func() { _ = pw.Close() }()
		write := func(event, data string) {
			_, _ = pw.Write([]byte("event: " + event + "\ndata: " + data + "\n\n"))
		}
		write("message_start", `{"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":10}}}`)
		write("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`)
		write("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"think"}}`)
		write("content_block_stop", `{"type":"content_block_stop","index":0}`)
		// 已关闭的 thinking 块又来了一条 delta。
		write("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"stray"}}`)
		write("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`)
		write("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"hello"}}`)
		// 块 1 没有 stop，上游直接 start 块 2。
		write("content_block_start", `{"type":"content_block_start","index":2,"content_block":{"type":"text","text":""}}`)
		write("content_block_delta", `{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"world"}}`)
		write("content_block_stop", `{"type":"content_block_stop","index":2}`)
		write("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`)
		write("message_stop", `{"type":"message_stop"}`)
	}()

	result, err := svc.handleStreamingResponseAnthropicAPIKeyPassthrough(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "glm-5.2")
	_ = pr.Close()
	require.NoError(t, err)
	require.NotNil(t, result)

	body := rec.Body.String()
	assertAnthropicBlockSequenceValid(t, body)
	require.NotContains(t, body, "stray", "指向已关闭块的 delta 应被丢弃")
	require.Contains(t, body, `"text":"hello"`)
	require.Contains(t, body, `"text":"world"`)
	// usage 统计不受守卫影响。
	require.Equal(t, 10, result.usage.InputTokens)
	require.Equal(t, 7, result.usage.OutputTokens)
}

// 通用（非透传）Anthropic 流路径同样受守卫保护：它服务的是 anthropic 兼容的第三方上游，
// 同一类畸形流会以同样方式打死客户端。
func TestHandleStreamingResponse_RepairsMalformedBlockSequence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newMinimalGatewayService()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}

	go func() {
		defer func() { _ = pw.Close() }()
		write := func(event, data string) {
			_, _ = pw.Write([]byte("event: " + event + "\ndata: " + data + "\n\n"))
		}
		write("message_start", `{"type":"message_start","message":{"usage":{"input_tokens":10}}}`)
		write("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		write("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`)
		// 块 0 没有 stop，上游直接 start 块 1。
		write("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`)
		write("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"world"}}`)
		write("content_block_stop", `{"type":"content_block_stop","index":1}`)
		// 已关闭的块又来一条 text delta：内容保留，挂到新块上。
		write("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"stray"}}`)
		write("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`)
		write("message_stop", `{"type":"message_stop"}`)
	}()

	result, err := svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "glm-5.2", "glm-5.2", false)
	_ = pr.Close()
	require.NoError(t, err)
	require.NotNil(t, result)

	body := rec.Body.String()
	assertAnthropicBlockSequenceValid(t, body)
	require.Contains(t, body, `"text":"hello"`)
	require.Contains(t, body, `"text":"world"`)
	require.Contains(t, body, `"text":"stray"`, "越界的 text delta 内容也要保住")
	require.Equal(t, 10, result.usage.InputTokens)
	require.Equal(t, 7, result.usage.OutputTokens)
}

// 合规流经透传路径后必须字节级不变（守卫零行为变化）。
func TestHandleStreamingResponseAnthropicAPIKeyPassthrough_CompliantStreamByteIdentical(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newMinimalGatewayService()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	upstream := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":3}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}
	go func() {
		defer func() { _ = pw.Close() }()
		_, _ = pw.Write([]byte(upstream))
	}()

	_, err := svc.handleStreamingResponseAnthropicAPIKeyPassthrough(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "glm-5.2")
	_ = pr.Close()
	require.NoError(t, err)
	require.Equal(t, upstream, rec.Body.String())
}
