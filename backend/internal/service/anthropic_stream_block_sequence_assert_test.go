package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// assertAnthropicBlockSequenceValid 按 Anthropic Messages 流协议校验一段 SSE 输出的块顺序，
// 规则与严格客户端（MetaCode）一致：同一时刻最多一个块打开、上一个块 stop 之后才能 start
// 下一个、index 从 0 连续递增、delta 只能落在当前打开的块上。
//
// 网关的两条出流路径（Anthropic 透传 + Gemini 转换）共用这个断言，
// 保证"客户端会拒收的流"在测试里同样会失败。
func assertAnthropicBlockSequenceValid(t *testing.T, body string) {
	t.Helper()

	active := -1
	nextExpected := 0
	for _, line := range strings.Split(body, "\n") {
		data, ok := extractAnthropicSSEDataLine(line)
		if !ok {
			continue
		}
		payload := strings.TrimSpace(data)
		if payload == "" || payload[0] != '{' {
			continue
		}
		idx := int(gjson.Get(payload, "index").Int())
		switch gjson.Get(payload, "type").String() {
		case "content_block_start":
			require.Equalf(t, -1, active, "ContentBlockStartWhileBlockOpen: start(%d) 时块 %d 仍未关闭", idx, active)
			require.Equalf(t, nextExpected, idx, "块 index 不连续：期望 %d，收到 %d", nextExpected, idx)
			active = idx
		case "content_block_delta":
			require.Equalf(t, active, idx, "DeltaAfterStop/OrphanDelta: delta 指向块 %d，当前打开的是 %d", idx, active)
		case "content_block_stop":
			require.Equalf(t, active, idx, "OrphanStop: stop 指向块 %d，当前打开的是 %d", idx, active)
			active = -1
			nextExpected = idx + 1
		case "message_stop":
			require.Equalf(t, -1, active, "MessageStopWhileBlockOpen: 块 %d 未关闭", active)
		}
	}
	require.Equalf(t, -1, active, "流结束时块 %d 仍未关闭", active)
}
