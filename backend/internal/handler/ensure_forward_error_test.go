package handler

import (
	"context"
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
	// 写入一些 SSE 字节，模拟流已开始（c.Writer.Written() == true）。
	if _, err := c.Writer.Write([]byte("data: {\"type\":\"message_start\"}\n\n")); err != nil {
		t.Fatalf("seed write failed: %v", err)
	}
	return c, w
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
