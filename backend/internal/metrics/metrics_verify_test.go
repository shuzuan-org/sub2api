package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestAllMetricsRegistered verifies that all expected Prometheus metrics are
// registered and appear in the /metrics output. This validates Phase 1-3 changes
// without requiring a running server.
func TestAllMetricsRegistered(t *testing.T) {
	// Bootstrap model allowlist to activate normalization.
	SetAllowedModels([]string{
		"claude-sonnet-4-5", "claude-opus-4-8",
		"gpt-5.1", "gpt-5.3-codex",
		"gemini-2.5-pro",
		"deepseek-chat", "deepseek-reasoner",
	})
	BootstrapModelMetrics([]string{
		"claude-sonnet-4-5", "claude-opus-4-8",
		"gpt-5.1", "gpt-5.3-codex",
		"gemini-2.5-pro",
		"deepseek-chat", "deepseek-reasoner",
	})

	// Register account pool gauges (empty callback — won't emit data but metrics will be registered).
	RegisterAccountPoolGauges(func() []AccountPoolStat {
		return []AccountPoolStat{
			{Platform: "deepseek", Model: "deepseek-chat", Total: 3, Available: 2, Unavailable: 1},
			{Platform: "anthropic", Model: "claude-sonnet-4-5", Total: 5, Available: 5, Unavailable: 0},
		}
	})

	// Register upstream pool gauges.
	RegisterUpstreamPoolGauges(func() UpstreamPoolStat {
		return UpstreamPoolStat{ClientsCached: 42, InFlightTotal: 7}
	})

	// Simulate some metric emission to create time series.
	RecordUpstreamStatus("deepseek", "deepseek-chat", 200)
	RecordUpstreamStatus("deepseek", "deepseek-chat", 429)
	RecordUpstreamStatus("anthropic", "claude-sonnet-4-5", 200)
	RecordUpstreamLatency("deepseek", "deepseek-chat", "ttft", 1500)
	RecordUpstreamLatency("deepseek", "deepseek-chat", "total", 8000)
	RecordUpstreamLatency("anthropic", "claude-sonnet-4-5", "ttft", 800)
	RecordUpstreamLatency("anthropic", "claude-sonnet-4-5", "total", 5000)

	OpsErrorTotal.WithLabelValues("deepseek", NormalizeModel("deepseek-chat"), "upstream", "rate_limit_error", "P1", "false").Inc()

	// Also emit a model that is NOT in the allowlist to verify __other__ bucketing.
	RecordUpstreamStatus("anthropic", "some-unknown-model", 500)

	rec := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/metrics", nil)
	Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	t.Logf("/metrics output size: %d bytes", len(body))

	// Verify all expected metric families appear.
	// Note: promauto metrics with zero observations won't emit any lines,
	// so we only check metrics that either have observations or are Gauge/Collector-based.
	requiredMetrics := []string{
		// Phase 1: ops_error_total with model label (has observation from this test)
		"sub2api_ops_error_total",
		// Phase 2: upstream status + latency (have observations)
		"sub2api_upstream_status_total",
		"sub2api_upstream_latency_seconds",
		// Phase 3: account pool + upstream pool (Collector-based, always emit)
		"sub2api_account_pool_available",
		"sub2api_account_pool_total",
		"sub2api_account_pool_unavailable",
		"sub2api_upstream_pool_clients_cached",
		"sub2api_upstream_pool_in_flight",
		// 稳定性看板低频事件指标：init() 预初始化为 0，应恒出现（修 No data）。
		"sub2api_stream_truncation_total",
		"sub2api_request_interrupted_total",
		"sub2api_tool_error_total",
		"sub2api_upstream_error_shaped_total",
	}

	for _, name := range requiredMetrics {
		if !strings.Contains(body, name) {
			t.Errorf("metric %q not found in /metrics output", name)
		}
	}

	// Verify model label is present on OpsErrorTotal.
	if !strings.Contains(body, `sub2api_ops_error_total{`) {
		t.Error("sub2api_ops_error_total not found with labels")
	}
	// Verify the model label appears as a dimension.
	if !strings.Contains(body, `model="deepseek-chat"`) {
		t.Error("expected model label 'deepseek-chat' not found in /metrics output")
	}
	// Verify unknown model is bucketed into __other__.
	if !strings.Contains(body, `model="__other__"`) {
		t.Error("expected model label '__other__' for unknown model not found")
	}

	// Verify upstream latency has correct phase labels.
	if !strings.Contains(body, `phase="ttft"`) {
		t.Error("expected phase label 'ttft' not found in sub2api_upstream_latency_seconds")
	}
	if !strings.Contains(body, `phase="total"`) {
		t.Error("expected phase label 'total' not found in sub2api_upstream_latency_seconds")
	}

	// Verify upstream status has correct buckets.
	if !strings.Contains(body, `upstream_status="429"`) {
		t.Error("expected upstream_status='429' not found")
	}
	if !strings.Contains(body, `upstream_status="200"`) {
		t.Error("expected upstream_status='200' not found")
	}
	if !strings.Contains(body, `upstream_status="500"`) {
		t.Error("expected upstream_status='500' not found")
	}

	// Verify account pool collector emits data.
	if !strings.Contains(body, `sub2api_account_pool_available`) {
		t.Error("sub2api_account_pool_available metric not found in /metrics output")
	}
	if !strings.Contains(body, `sub2api_account_pool_total`) {
		t.Error("sub2api_account_pool_total metric not found in /metrics output")
	}
	if idx := strings.Index(body, "sub2api_account_pool_available"); idx >= 0 {
		t.Logf("account_pool_available snippet: %s", body[idx:idx+200])
	}

	// Verify upstream pool collector emits data.
	if !strings.Contains(body, "sub2api_upstream_pool_clients_cached") {
		t.Error("sub2api_upstream_pool_clients_cached not found in /metrics output")
	}
	if !strings.Contains(body, "sub2api_upstream_pool_in_flight") {
		t.Error("sub2api_upstream_pool_in_flight not found in /metrics output")
	}
	if idx := strings.Index(body, "sub2api_upstream_pool"); idx >= 0 {
		t.Logf("upstream_pool snippet: %s", body[idx:idx+200])
	}

	t.Logf("All %d required metrics verified in /metrics output", len(requiredMetrics))
}

// TestNormalizeModel verifies the model name normalization logic.
func TestNormalizeModel(t *testing.T) {
	SetAllowedModels([]string{"gpt-5.1", "deepseek-chat", "claude-sonnet-4-5"})

	tests := []struct {
		input    string
		expected string
	}{
		{"gpt-5.1", "gpt-5.1"},
		{"deepseek-chat", "deepseek-chat"},
		{"claude-sonnet-4-5", "claude-sonnet-4-5"},
		{"some-random-model", "__other__"},
		{"", ""},
		{"  deepseek-chat  ", "deepseek-chat"},
	}
	for _, tt := range tests {
		got := NormalizeModel(tt.input)
		if got != tt.expected {
			t.Errorf("NormalizeModel(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}

	// Verify AllowedModelCount.
	if n := AllowedModelCount(); n != 3 {
		t.Errorf("AllowedModelCount() = %d, want 3", n)
	}
}

// TestRecordTokenUsage verifies token consumption is accumulated per model+type,
// unknown models bucket to __other__, and non-positive values are skipped.
func TestRecordTokenUsage(t *testing.T) {
	SetAllowedModels([]string{"claude-sonnet-4-5"})

	base := func(model, typ string) float64 {
		return testutil.ToFloat64(LLMTokensTotal.WithLabelValues(model, typ))
	}
	inBefore := base("claude-sonnet-4-5", "input")
	outBefore := base("claude-sonnet-4-5", "output")
	crBefore := base("claude-sonnet-4-5", "cache_read")
	ccBefore := base("claude-sonnet-4-5", "cache_creation")
	otherBefore := base("__other__", "input")

	RecordTokenUsage("claude-sonnet-4-5", 100, 40, 25, 10)
	RecordTokenUsage("claude-sonnet-4-5", 0, -5, 0, 0) // non-positive skipped
	RecordTokenUsage("mystery-model", 7, 0, 0, 0)      // unknown → __other__

	if got := base("claude-sonnet-4-5", "input") - inBefore; got != 100 {
		t.Errorf("input delta = %v, want 100", got)
	}
	if got := base("claude-sonnet-4-5", "output") - outBefore; got != 40 {
		t.Errorf("output delta = %v, want 40", got)
	}
	if got := base("claude-sonnet-4-5", "cache_read") - crBefore; got != 25 {
		t.Errorf("cache_read delta = %v, want 25", got)
	}
	if got := base("claude-sonnet-4-5", "cache_creation") - ccBefore; got != 10 {
		t.Errorf("cache_creation delta = %v, want 10", got)
	}
	if got := base("__other__", "input") - otherBefore; got != 7 {
		t.Errorf("__other__ input delta = %v, want 7", got)
	}
}

// TestUpstreamStatusBucket verifies the status code bucketing.
func TestUpstreamStatusBucket(t *testing.T) {
	tests := []struct {
		code     int
		expected string
	}{
		{200, "200"},
		{400, "400"},
		{401, "401"},
		{403, "403"},
		{429, "429"},
		{500, "500"},
		{502, "502"},
		{503, "503"},
		{201, "2xx"},
		{404, "4xx"},
		{504, "5xx"},
		{302, "other"},
	}
	for _, tt := range tests {
		got := upstreamStatusBucket(tt.code)
		if got != tt.expected {
			t.Errorf("upstreamStatusBucket(%d) = %q, want %q", tt.code, got, tt.expected)
		}
	}
}
