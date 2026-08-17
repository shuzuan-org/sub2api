package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// claudeStreamBlock 是从转换后的 SSE 输出里还原出来的一个 content block。
type claudeStreamBlock struct {
	index     int
	blockType string
	toolName  string
	toolID    string
	text      string
	inputJSON string
	stopped   bool
}

// parseClaudeStreamBlocks 还原 SSE 输出里的各个 content block（按 index 顺序）。
func parseClaudeStreamBlocks(t *testing.T, body string) []claudeStreamBlock {
	t.Helper()

	var blocks []claudeStreamBlock
	byIndex := map[int]int{}
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
			byIndex[idx] = len(blocks)
			blocks = append(blocks, claudeStreamBlock{
				index:     idx,
				blockType: gjson.Get(payload, "content_block.type").String(),
				toolName:  gjson.Get(payload, "content_block.name").String(),
				toolID:    gjson.Get(payload, "content_block.id").String(),
			})
		case "content_block_delta":
			pos, ok := byIndex[idx]
			require.Truef(t, ok, "delta 指向未 start 的块 %d", idx)
			blocks[pos].text += gjson.Get(payload, "delta.text").String()
			blocks[pos].inputJSON += gjson.Get(payload, "delta.partial_json").String()
		case "content_block_stop":
			if pos, ok := byIndex[idx]; ok {
				blocks[pos].stopped = true
			}
		}
	}
	return blocks
}

// runGeminiStreamConversion 把若干 Gemini SSE chunk 喂给流式转换，返回写给客户端的响应体。
func runGeminiStreamConversion(t *testing.T, chunks []string) string {
	t.Helper()

	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	var sb strings.Builder
	for _, chunk := range chunks {
		sb.WriteString("data: " + chunk + "\n\n")
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(sb.String())),
	}

	svc := &GeminiMessagesCompatService{}
	result, err := svc.handleStreamingResponse(c, resp, time.Now(), "gemini-2.5-pro")
	require.NoError(t, err)
	require.NotNil(t, result)
	return rec.Body.String()
}

// text → functionCall → text 交错时，tool 块必须先 stop 再 start 新的 text 块。
// 修复前 tool 块悬空未关闭，输出 start(1,tool_use) delta start(2,text)，
// 与 GLM-5.2 事故完全相同的错误签名（ContentBlockStartWhileBlockOpen）。
func TestGeminiStreamConversion_ClosesToolBlockBeforeText(t *testing.T) {
	body := runGeminiStreamConversion(t, []string{
		`{"candidates":[{"content":{"parts":[{"text":"before"}]}}]}`,
		`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"get_weather","args":{"city":"SF"}}}]}}]}`,
		`{"candidates":[{"content":{"parts":[{"text":"after"}]}}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":7}}`,
	})

	assertAnthropicBlockSequenceValid(t, body)

	blocks := parseClaudeStreamBlocks(t, body)
	require.Len(t, blocks, 3)
	require.Equal(t, "text", blocks[0].blockType)
	require.Equal(t, "before", blocks[0].text)
	require.Equal(t, "tool_use", blocks[1].blockType)
	require.Equal(t, "get_weather", blocks[1].toolName)
	require.Equal(t, "after", blocks[2].text)
	for _, b := range blocks {
		require.Truef(t, b.stopped, "块 %d 没有 content_block_stop", b.index)
	}
}

// 并行调用同一个工具：两次 functionCall 参数互不为前缀，必须拆成两个 tool_use 块，
// 否则参数 JSON 会被拼成 {"city":"SF"}{"city":"NY"} 这样的非法 input。
func TestGeminiStreamConversion_ParallelSameToolCallsAreSeparateBlocks(t *testing.T) {
	body := runGeminiStreamConversion(t, []string{
		`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"get_weather","args":{"city":"SF"}}}]}}]}`,
		`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"get_weather","args":{"city":"NY"}}}]}}]}`,
	})

	assertAnthropicBlockSequenceValid(t, body)

	blocks := parseClaudeStreamBlocks(t, body)
	require.Len(t, blocks, 2)
	require.Equal(t, `{"city":"SF"}`, blocks[0].inputJSON)
	require.Equal(t, `{"city":"NY"}`, blocks[1].inputJSON)
	require.NotEqual(t, blocks[0].toolID, blocks[1].toolID, "两次调用应有各自的 tool_use id")
	for _, b := range blocks {
		require.True(t, gjson.Valid(b.inputJSON), "tool 参数必须是合法 JSON，实得 %q", b.inputJSON)
	}
}

// 同一次调用的参数按累积形式分片重发时，仍然合进同一个 tool_use 块。
func TestGeminiStreamConversion_CumulativeToolArgsStayInOneBlock(t *testing.T) {
	body := runGeminiStreamConversion(t, []string{
		`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"write_file","args":"{\"path\":\"a.txt\""}}]}}]}`,
		`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"write_file","args":"{\"path\":\"a.txt\",\"body\":\"hi\"}"}}]}}]}`,
	})

	assertAnthropicBlockSequenceValid(t, body)

	blocks := parseClaudeStreamBlocks(t, body)
	require.Len(t, blocks, 1)
	require.Equal(t, `{"path":"a.txt","body":"hi"}`, blocks[0].inputJSON)
}

// 纯文本流（最常见形态）不受改动影响。
func TestGeminiStreamConversion_TextOnlyStreamStaysSingleBlock(t *testing.T) {
	body := runGeminiStreamConversion(t, []string{
		`{"candidates":[{"content":{"parts":[{"text":"hello"}]}}]}`,
		`{"candidates":[{"content":{"parts":[{"text":" world"}]}}]}`,
	})

	assertAnthropicBlockSequenceValid(t, body)

	blocks := parseClaudeStreamBlocks(t, body)
	require.Len(t, blocks, 1)
	require.Equal(t, "hello world", blocks[0].text)
}

func TestGeminiToolArgsContinue(t *testing.T) {
	require.True(t, geminiToolArgsContinue("", `{"a":1}`), "首个分片总是续传")
	require.True(t, geminiToolArgsContinue(`{"a":1`, `{"a":1}`), "累积重发")
	require.True(t, geminiToolArgsContinue(`{"a":1}`, `{"a":1}`), "原样重发")
	require.True(t, geminiToolArgsContinue(`{"a":1}`, `{"a":1`), "回退重发")
	require.False(t, geminiToolArgsContinue(`{"a":1}`, `{"a":2}`), "互不为前缀 = 另一次调用")
}
