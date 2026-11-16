package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/metrics"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// newStreamingTestContext 构造一个"流已开始"（已落字节）的 gin 测试上下文。
func newStreamingTestContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	c.Header("Content-Type", "text/event-stream")
	// 写入一些 SSE 字节，模拟流已开始（c.Writer.Written() == true）。
	if _, err := c.Writer.Write([]byte("data: {\"type\":\"message_start\"}\n\n")); err != nil {
		t.Fatalf("seed write failed: %v", err)
	}
	return c, w
}

func forwardErrorFallbacks() map[string]func(*gin.Context, bool) bool {
	return map[string]func(*gin.Context, bool) bool{
		"gateway":         (&GatewayHandler{}).ensureForwardErrorResponse,
		"openai":          (&OpenAIGatewayHandler{}).ensureForwardErrorResponse,
		"openai_messages": (&OpenAIGatewayHandler{}).ensureAnthropicErrorResponse,
	}
}

func TestForwardErrorFallback_PreservesWrittenJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const body = `{"type":"error","error":{"type":"invalid_request_error","message":"image input is not supported by the text-only SGLang route"}}`
	for name, fallback := range forwardErrorFallbacks() {
		for _, status := range []int{http.StatusBadRequest, http.StatusNotFound, http.StatusBadGateway} {
			for _, canceled := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%d/canceled=%t", name, status, canceled), func(t *testing.T) {
					w := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(w)
					c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
					if canceled {
						ctx, cancel := context.WithCancel(c.Request.Context())
						cancel()
						c.Request = c.Request.WithContext(ctx)
					}
					c.Data(status, "application/json; charset=utf-8", []byte(body))
					upstreamBefore := testutil.ToFloat64(metrics.StreamTruncationTotal.WithLabelValues("upstream"))
					clientBefore := testutil.ToFloat64(metrics.StreamTruncationTotal.WithLabelValues("client"))
					// The handler can infer streamStarted=true from a streaming request's JSON response.
					if fallback(c, true) {
						t.Fatal("must not append a fallback to a completed JSON response")
					}
					if w.Code != status || w.Body.String() != body || !json.Valid(w.Body.Bytes()) {
						t.Fatalf("response changed: status=%d body=%q", w.Code, w.Body.String())
					}
					if got := w.Result().Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
						t.Fatalf("content type changed: %q", got)
					}
					if testutil.ToFloat64(metrics.StreamTruncationTotal.WithLabelValues("upstream")) != upstreamBefore ||
						testutil.ToFloat64(metrics.StreamTruncationTotal.WithLabelValues("client")) != clientBefore {
						t.Fatal("completed JSON response must not count as stream truncation")
					}
				})
			}
		}
	}
}

func TestForwardErrorFallback_PreservesSSETruncationHandling(t *testing.T) {
	for name, fallback := range forwardErrorFallbacks() {
		for _, contentType := range []string{"text/event-stream", "text/event-stream; charset=utf-8"} {
			t.Run(name+"/"+contentType, func(t *testing.T) {
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
				c.Header("Content-Type", contentType)
				if _, err := c.Writer.Write([]byte("data: {\"type\":\"message_start\"}\n\n")); err != nil {
					t.Fatal(err)
				}
				before := testutil.ToFloat64(metrics.StreamTruncationTotal.WithLabelValues("upstream"))
				if !fallback(c, true) || !strings.Contains(w.Body.String(), "Upstream stream interrupted") {
					t.Fatalf("missing terminal SSE error: %q", w.Body.String())
				}
				body := w.Body.String()
				if fallback(c, true) || w.Body.String() != body {
					t.Fatal("terminal error emitted twice")
				}
				if testutil.ToFloat64(metrics.StreamTruncationTotal.WithLabelValues("upstream")) != before+1 {
					t.Fatal("expected exactly one upstream truncation")
				}
			})
		}
	}
}

func TestEnsureForwardErrorResponse_UpstreamTruncationEmitsSSE(t *testing.T) {
	h := &GatewayHandler{}
	c, w := newStreamingTestContext(t)

	before := testutil.ToFloat64(metrics.StreamTruncationTotal.WithLabelValues("upstream"))
	wrote := h.ensureForwardErrorResponse(c, true)
	after := testutil.ToFloat64(metrics.StreamTruncationTotal.WithLabelValues("upstream"))

	if !wrote {
		t.Fatalf("expected wrote=true on upstream truncation")
	}
	if !strings.Contains(w.Body.String(), `"type":"error"`) {
		t.Fatalf("expected terminal SSE error event, got body: %s", w.Body.String())
	}
	if after != before+1 {
		t.Fatalf("upstream truncation metric not incremented: %v -> %v", before, after)
	}
}

func TestEnsureForwardErrorResponse_ClientCancelStaysSilent(t *testing.T) {
	h := &GatewayHandler{}
	c, w := newStreamingTestContext(t)
	// 客户端主动断连：把请求 context 取消。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.Request = c.Request.WithContext(ctx)

	before := testutil.ToFloat64(metrics.StreamTruncationTotal.WithLabelValues("client"))
	wrote := h.ensureForwardErrorResponse(c, true)
	after := testutil.ToFloat64(metrics.StreamTruncationTotal.WithLabelValues("client"))

	if wrote {
		t.Fatalf("expected wrote=false on client cancel (don't write dead conn)")
	}
	if strings.Contains(w.Body.String(), `"type":"error"`) {
		t.Fatalf("should not write SSE error to a cancelled client; body: %s", w.Body.String())
	}
	if after != before+1 {
		t.Fatalf("client truncation metric not incremented: %v -> %v", before, after)
	}
}

func TestEnsureForwardErrorResponse_NoDoubleEmit(t *testing.T) {
	h := &GatewayHandler{}
	c, _ := newStreamingTestContext(t)
	c.Set(terminalErrorSentKey, true) // 标记已补发过 terminal 事件

	if wrote := h.ensureForwardErrorResponse(c, true); wrote {
		t.Fatalf("expected wrote=false when terminal already sent")
	}
}
