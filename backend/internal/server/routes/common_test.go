package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newCommonTestEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// nil db/redis：readiness 在依赖缺省时视为可用（探测被跳过）。
	RegisterCommonRoutes(r, nil, nil)
	return r
}

func TestHealthEndpoint(t *testing.T) {
	r := newCommonTestEngine()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("/health want 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"status":"ok"`) {
		t.Fatalf("/health body unexpected: %s", w.Body.String())
	}
}

func TestReadyEndpoint_NilDepsAreReady(t *testing.T) {
	r := newCommonTestEngine()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("/ready want 200 when deps nil, got %d (%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"status":"ready"`) {
		t.Fatalf("/ready body unexpected: %s", w.Body.String())
	}
}

func TestMetricsEndpoint(t *testing.T) {
	r := newCommonTestEngine()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("/metrics want 200, got %d", w.Code)
	}
	// 默认 registry 暴露 Go 运行时指标，应至少包含 go_goroutines。
	if !strings.Contains(w.Body.String(), "go_goroutines") {
		t.Fatalf("/metrics missing go_goroutines; body head: %s", w.Body.String()[:min(200, len(w.Body.String()))])
	}
}
