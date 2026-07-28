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

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// --- parseSSEUsage 测试 ---

func newMinimalGatewayService() *GatewayService {
	return &GatewayService{
		cfg: &config.Config{
			Gateway: config.GatewayConfig{
				StreamDataIntervalTimeout: 0,
				MaxLineSize:               defaultMaxLineSize,
			},
		},
		rateLimitService: &RateLimitService{},
	}
}

func TestParseSSEUsage_MessageStart(t *testing.T) {
	svc := newMinimalGatewayService()
	usage := &ClaudeUsage{}

	data := `{"type":"message_start","message":{"usage":{"input_tokens":100,"cache_creation_input_tokens":50,"cache_read_input_tokens":200}}}`
	svc.parseSSEUsage(data, usage)

	require.Equal(t, 100, usage.InputTokens)
	require.Equal(t, 50, usage.CacheCreationInputTokens)
	require.Equal(t, 200, usage.CacheReadInputTokens)
	require.Equal(t, 0, usage.OutputTokens, "message_start 不应设置 output_tokens")
}

func TestParseSSEUsage_MessageDelta(t *testing.T) {
	svc := newMinimalGatewayService()
	usage := &ClaudeUsage{}

	data := `{"type":"message_delta","usage":{"output_tokens":42}}`
	svc.parseSSEUsage(data, usage)

	require.Equal(t, 42, usage.OutputTokens)
	require.Equal(t, 0, usage.InputTokens, "message_delta 的 output_tokens 不应影响已有的 input_tokens")
}

func TestParseSSEUsage_DeltaDoesNotOverwriteStartValues(t *testing.T) {
	svc := newMinimalGatewayService()
	usage := &ClaudeUsage{}

	// 先处理 message_start
	svc.parseSSEUsage(`{"type":"message_start","message":{"usage":{"input_tokens":100}}}`, usage)
	require.Equal(t, 100, usage.InputTokens)

	// 再处理 message_delta（output_tokens > 0, input_tokens = 0）
	svc.parseSSEUsage(`{"type":"message_delta","usage":{"output_tokens":50}}`, usage)
	require.Equal(t, 100, usage.InputTokens, "delta 中 input_tokens=0 不应覆盖 start 中的值")
	require.Equal(t, 50, usage.OutputTokens)
}

func TestParseSSEUsage_DeltaOverwritesWithNonZero(t *testing.T) {
	svc := newMinimalGatewayService()
	usage := &ClaudeUsage{}

	// GLM 等 API 会在 delta 中包含所有 usage 信息
	svc.parseSSEUsage(`{"type":"message_delta","usage":{"input_tokens":200,"output_tokens":100,"cache_creation_input_tokens":30,"cache_read_input_tokens":60}}`, usage)
	require.Equal(t, 200, usage.InputTokens)
	require.Equal(t, 100, usage.OutputTokens)
	require.Equal(t, 30, usage.CacheCreationInputTokens)
	require.Equal(t, 60, usage.CacheReadInputTokens)
}

func TestParseSSEUsage_DeltaDoesNotResetCacheCreationBreakdown(t *testing.T) {
	svc := newMinimalGatewayService()
	usage := &ClaudeUsage{}

	// 先在 message_start 中写入非零 5m/1h 明细
	svc.parseSSEUsage(`{"type":"message_start","message":{"usage":{"input_tokens":100,"cache_creation":{"ephemeral_5m_input_tokens":30,"ephemeral_1h_input_tokens":70}}}}`, usage)
	require.Equal(t, 30, usage.CacheCreation5mTokens)
	require.Equal(t, 70, usage.CacheCreation1hTokens)

	// 后续 delta 带默认 0，不应覆盖已有非零值
	svc.parseSSEUsage(`{"type":"message_delta","usage":{"output_tokens":12,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0}}}`, usage)
	require.Equal(t, 30, usage.CacheCreation5mTokens, "delta 的 0 值不应重置 5m 明细")
	require.Equal(t, 70, usage.CacheCreation1hTokens, "delta 的 0 值不应重置 1h 明细")
	require.Equal(t, 12, usage.OutputTokens)
}

func TestParseSSEUsage_InvalidJSON(t *testing.T) {
	svc := newMinimalGatewayService()
	usage := &ClaudeUsage{}

	// 无效 JSON 不应 panic
	svc.parseSSEUsage("not json", usage)
	require.Equal(t, 0, usage.InputTokens)
	require.Equal(t, 0, usage.OutputTokens)
}

func TestParseSSEUsage_UnknownType(t *testing.T) {
	svc := newMinimalGatewayService()
	usage := &ClaudeUsage{}

	// 不是 message_start 或 message_delta 的类型
	svc.parseSSEUsage(`{"type":"content_block_delta","delta":{"text":"hello"}}`, usage)
	require.Equal(t, 0, usage.InputTokens)
	require.Equal(t, 0, usage.OutputTokens)
}

func TestParseSSEUsage_EmptyString(t *testing.T) {
	svc := newMinimalGatewayService()
	usage := &ClaudeUsage{}

	svc.parseSSEUsage("", usage)
	require.Equal(t, 0, usage.InputTokens)
}

func TestParseSSEUsage_DoneEvent(t *testing.T) {
	svc := newMinimalGatewayService()
	usage := &ClaudeUsage{}

	// [DONE] 事件不应影响 usage
	svc.parseSSEUsage("[DONE]", usage)
	require.Equal(t, 0, usage.InputTokens)
}

// --- 流式响应端到端测试 ---

func TestHandleStreamingResponse_CacheTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newMinimalGatewayService()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}

	go func() {
		defer func() { _ = pw.Close() }()
		_, _ = pw.Write([]byte("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":10,\"cache_creation_input_tokens\":20,\"cache_read_input_tokens\":30}}}\n\n"))
		_, _ = pw.Write([]byte("data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":15}}\n\n"))
		_, _ = pw.Write([]byte("data: [DONE]\n\n"))
	}()

	result, err := svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model", "model", false)
	_ = pr.Close()
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.usage)
	require.Equal(t, 10, result.usage.InputTokens)
	require.Equal(t, 15, result.usage.OutputTokens)
	require.Equal(t, 20, result.usage.CacheCreationInputTokens)
	require.Equal(t, 30, result.usage.CacheReadInputTokens)
}

func TestHandleStreamingResponse_EmptyStream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newMinimalGatewayService()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}

	go func() {
		// 直接关闭，不发送任何事件
		_ = pw.Close()
	}()

	result, err := svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model", "model", false)
	_ = pr.Close()
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing terminal event")
	require.NotNil(t, result)
}

func TestHandleStreamingResponse_SpecialCharactersInJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newMinimalGatewayService()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}

	go func() {
		defer func() { _ = pw.Close() }()
		// 包含特殊字符的 content_block_delta（引号、换行、Unicode）
		_, _ = pw.Write([]byte("data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello \\\"world\\\"\\n你好\"}}\n\n"))
		_, _ = pw.Write([]byte("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":5}}}\n\n"))
		_, _ = pw.Write([]byte("data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":3}}\n\n"))
		_, _ = pw.Write([]byte("data: [DONE]\n\n"))
	}()

	result, err := svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model", "model", false)
	_ = pr.Close()
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.usage)
	require.Equal(t, 5, result.usage.InputTokens)
	require.Equal(t, 3, result.usage.OutputTokens)

	// 验证响应中包含转发的数据
	body := rec.Body.String()
	require.Contains(t, body, "content_block_delta", "响应应包含转发的 SSE 事件")
}

// 上游非规范响应：HTTP 200 + 一行裸 JSON 错误对象（无 SSE 前缀）。
// 典型来自 sglang-proxy 的 max_tokens 超限。客户端需要 HTTP 4xx 才能触发 auto compact。
func TestHandleStreamingResponse_InlineJSONErrorRewritesToHTTPStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newMinimalGatewayService()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}

	errJSON := `{"type":"error","error":{"type":"invalid_request_error","message":"Requested token count exceeds the model's maximum context length of 196608 tokens. You requested a total of 216936 tokens."}}`

	go func() {
		defer func() { _ = pw.Close() }()
		_, _ = pw.Write([]byte(errJSON + "\n"))
	}()

	_, err := svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model", "model", false)
	_ = pr.Close()
	require.Error(t, err)
	require.Contains(t, err.Error(), "upstream inline error")
	require.Equal(t, http.StatusBadRequest, rec.Code, "invalid_request_error 必须映射为 HTTP 400")
	require.Contains(t, rec.Body.String(), "invalid_request_error")
	require.Contains(t, rec.Body.String(), "216936", "body 里应保留上游的 token 计数，供客户端解析")
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"), "必须改为 JSON 响应，而非 text/event-stream")
}

func TestParseInlineAnthropicErrorJSON(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		wantMatch bool
		wantCode  int
	}{
		{"invalid_request", `{"type":"error","error":{"type":"invalid_request_error","message":"x"}}`, true, 400},
		{"authentication", `{"type":"error","error":{"type":"authentication_error","message":"x"}}`, true, 401},
		{"rate_limit", `{"type":"error","error":{"type":"rate_limit_error","message":"x"}}`, true, 429},
		{"overloaded", `{"type":"error","error":{"type":"overloaded_error","message":"x"}}`, true, 529},
		{"empty", ``, false, 0},
		{"sse_data_line", `data: {"type":"error","error":{"type":"invalid_request_error"}}`, false, 0},
		{"not_error_type", `{"type":"message_start","message":{}}`, false, 0},
		{"error_without_subtype", `{"type":"error","error":{}}`, false, 0},
		{"malformed_json", `{"type":"error"`, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body, matched := parseInlineAnthropicErrorJSON(tc.in)
			require.Equal(t, tc.wantMatch, matched)
			if tc.wantMatch {
				require.Equal(t, tc.wantCode, status)
				require.NotEmpty(t, body)
			}
		})
	}
}

// 上游 body 带尾部换行/空白：gateway 应 trim 后再透给客户端，
// 防止客户端流式 JSON parser 偶发解析失败。
func TestHandleStreamingResponse_InlineJSONErrorTrimsTrailingWhitespace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newMinimalGatewayService()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}

	errJSON := `{"type":"error","error":{"type":"invalid_request_error","message":"x"}}`

	go func() {
		defer func() { _ = pw.Close() }()
		// 上游 body 带尾部换行和空白
		_, _ = pw.Write([]byte(errJSON + "\n\r\n  "))
	}()

	_, err := svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model", "model", false)
	_ = pr.Close()
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	// Body 应该完全等于原始 JSON（不含尾部空白）
	require.Equal(t, errJSON, rec.Body.String())
}

// api_error 需映射到 503（而非之前的 502）。
func TestParseInlineAnthropicErrorJSON_APIErrorMapsTo503(t *testing.T) {
	in := `{"type":"error","error":{"type":"api_error","message":"internal"}}`
	status, _, matched := parseInlineAnthropicErrorJSON(in)
	require.True(t, matched)
	require.Equal(t, http.StatusServiceUnavailable, status)
}

// 正常 SSE 流中间行如果偶发含 `{"type":"error"` 的文本片段（例如在 text delta 里），
// 不应被 inline error 检查触发。firstNonEmptyLine 守卫要求检查只发生在首个非空行上。
func TestHandleStreamingResponse_InlineErrorCheckOnlyOnFirstLine(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newMinimalGatewayService()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}

	go func() {
		defer func() { _ = pw.Close() }()
		_, _ = pw.Write([]byte("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":5}}}\n\n"))
		// 第二行 data 内恰好是一个 error 样式 JSON —— 不应触发 inline 重写响应。
		_, _ = pw.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"evil"}}` + "\n"))
		_, _ = pw.Write([]byte("data: {\"type\":\"message_stop\"}\n\n"))
	}()

	result, err := svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model", "model", false)
	_ = pr.Close()
	require.NoError(t, err, "中途的裸 JSON 不应被识别为 inline error")
	require.NotNil(t, result)
	// 响应不应被重置为 HTTP 400
	require.NotEqual(t, http.StatusBadRequest, rec.Code)
}

// Passthrough 路径（Anthropic apikey passthrough 账号）也需识别 inline error。
func TestHandleStreamingResponseAnthropicAPIKeyPassthrough_InlineErrorRewritesToHTTPStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newMinimalGatewayService()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}

	errJSON := `{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens too large"}}`

	go func() {
		defer func() { _ = pw.Close() }()
		_, _ = pw.Write([]byte(errJSON + "\n"))
	}()

	_, err := svc.handleStreamingResponseAnthropicAPIKeyPassthrough(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model")
	_ = pr.Close()
	require.Error(t, err)
	require.Contains(t, err.Error(), "upstream inline error")
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, errJSON, rec.Body.String())
}

// 上游把用户侧错误（prompt 超长）包装成 HTTP 200 + message_start / error / message_stop。
// 客户端（Claude Code）只有拿到真 400 才会触发 auto compact，拿到 200 只会无限重试。
func TestHandleStreamingResponse_MessageStartThenErrorRewritesToHTTP400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newMinimalGatewayService()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}

	errData := `{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 300000 tokens > 200000 maximum"}}`

	go func() {
		defer func() { _ = pw.Close() }()
		_, _ = pw.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"model\":\"model\",\"usage\":{\"input_tokens\":10}}}\n\n"))
		_, _ = pw.Write([]byte("event: error\ndata: " + errData + "\n\n"))
		_, _ = pw.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}()

	_, err := svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model", "model", false)
	_ = pr.Close()
	require.Error(t, err)
	require.Contains(t, err.Error(), "upstream inline error")
	require.Equal(t, http.StatusBadRequest, rec.Code, "invalid_request_error 必须映射为 HTTP 400")
	require.Equal(t, errData, rec.Body.String(), "body 必须是 Anthropic 官方形状的错误 JSON")
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	require.NotContains(t, rec.Body.String(), "message_start", "被扣住的 message_start 不能泄漏到错误响应里")
}

// 错误事件直接作为首个事件（没有 message_start 包装）同样要改写成真 HTTP 状态码。
func TestHandleStreamingResponse_FirstEventErrorRewritesToHTTPStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newMinimalGatewayService()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}

	errData := `{"type":"error","error":{"type":"not_found_error","message":"model not found"}}`

	go func() {
		defer func() { _ = pw.Close() }()
		_, _ = pw.Write([]byte("event: error\ndata: " + errData + "\n\n"))
	}()

	_, err := svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model", "model", false)
	_ = pr.Close()
	require.Error(t, err)
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Equal(t, errData, rec.Body.String())
}

// 正常流：扣住的 message_start 必须原样、按序放行，usage 与转发内容都不受影响。
func TestHandleStreamingResponse_HeldMessageStartReleasedInOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newMinimalGatewayService()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}

	go func() {
		defer func() { _ = pw.Close() }()
		_, _ = pw.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":11}}}\n\n"))
		_, _ = pw.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n"))
		_, _ = pw.Write([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":7}}\n\n"))
		_, _ = pw.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}()

	result, err := svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model", "model", false)
	_ = pr.Close()
	require.NoError(t, err)
	require.Equal(t, 11, result.usage.InputTokens)
	require.Equal(t, 7, result.usage.OutputTokens)

	body := rec.Body.String()
	require.Contains(t, body, "message_start")
	require.Less(t, strings.Index(body, "message_start"), strings.Index(body, "content_block_delta"), "message_start 必须先于后续事件送达")
	require.Contains(t, body, "message_stop")
}

// 上游只发了 message_start 就断开：扣住的事件必须补写出去，不能被吞掉。
func TestHandleStreamingResponse_HeldMessageStartFlushedOnUpstreamClose(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newMinimalGatewayService()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}

	go func() {
		defer func() { _ = pw.Close() }()
		_, _ = pw.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":3}}}\n\n"))
	}()

	_, err := svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model", "model", false)
	_ = pr.Close()
	require.Error(t, err, "缺少终止事件")
	require.Contains(t, rec.Body.String(), "message_start", "扣住的 message_start 不能被吞掉")
}

// Passthrough 路径：同样的 message_start + error 形状也要改写成 HTTP 400。
func TestHandleStreamingResponseAnthropicAPIKeyPassthrough_MessageStartThenErrorRewritesToHTTP400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newMinimalGatewayService()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}

	errData := `{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 300000 tokens > 200000 maximum"}}`

	go func() {
		defer func() { _ = pw.Close() }()
		_, _ = pw.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":10}}}\n\n"))
		_, _ = pw.Write([]byte("event: error\ndata: " + errData + "\n\n"))
		_, _ = pw.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}()

	_, err := svc.handleStreamingResponseAnthropicAPIKeyPassthrough(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model")
	_ = pr.Close()
	require.Error(t, err)
	require.Contains(t, err.Error(), "upstream inline error")
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, errData, rec.Body.String())
}

// Passthrough 路径正常流：逐行透传不能被扣留窗口打乱顺序或丢行。
func TestHandleStreamingResponseAnthropicAPIKeyPassthrough_HeldMessageStartReleasedInOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newMinimalGatewayService()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}

	stream := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":11}}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":7}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	go func() {
		defer func() { _ = pw.Close() }()
		_, _ = pw.Write([]byte(stream))
	}()

	result, err := svc.handleStreamingResponseAnthropicAPIKeyPassthrough(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model")
	_ = pr.Close()
	require.NoError(t, err)
	require.Equal(t, 11, result.usage.InputTokens)
	require.Equal(t, 7, result.usage.OutputTokens)
	require.Equal(t, stream, rec.Body.String(), "passthrough 必须逐字节原样透传")
}

func TestParseAnthropicSSEErrorEvent(t *testing.T) {
	cases := []struct {
		name      string
		eventName string
		dataLine  string
		wantMatch bool
		wantCode  int
	}{
		{"error_event", "error", `{"type":"error","error":{"type":"invalid_request_error","message":"x"}}`, true, 400},
		{"error_event_overloaded", "error", `{"type":"error","error":{"type":"overloaded_error"}}`, true, 529},
		{"no_event_name", "", `{"type":"error","error":{"type":"authentication_error"}}`, true, 401},
		{"message_start", "message_start", `{"type":"message_start","message":{}}`, false, 0},
		{"error_name_but_not_error_payload", "error", `{"type":"message_stop"}`, false, 0},
		{"error_without_subtype", "error", `{"type":"error","error":{}}`, false, 0},
		{"malformed_json", "error", `{"type":"error"`, false, 0},
		{"empty_data", "error", ``, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body, matched := parseAnthropicSSEErrorEvent(tc.eventName, tc.dataLine)
			require.Equal(t, tc.wantMatch, matched)
			if tc.wantMatch {
				require.Equal(t, tc.wantCode, status)
				require.Equal(t, tc.dataLine, string(body))
			}
		})
	}
}

// resetToJSONErrorHeaders 保留 X-Request-Id，删除所有其他 header。
func TestResetToJSONErrorHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	h.Set("Transfer-Encoding", "chunked")
	h.Set("X-Frame-Options", "DENY")
	h.Set("X-Request-Id", "req_abc123")

	resetToJSONErrorHeaders(h)

	require.Equal(t, "req_abc123", h.Get("X-Request-Id"))
	require.Empty(t, h.Get("Content-Type"))
	require.Empty(t, h.Get("Cache-Control"))
	require.Empty(t, h.Get("Connection"))
	require.Empty(t, h.Get("X-Accel-Buffering"))
	require.Empty(t, h.Get("Transfer-Encoding"))
	require.Empty(t, h.Get("X-Frame-Options"))
}
