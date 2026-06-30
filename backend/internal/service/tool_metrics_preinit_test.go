package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/metrics"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// 构造矫正器后，tool 指标的已知 series 应以 0 出现，
// 让 Grafana 显示明确的 0 而非有歧义的 "No data"。
func TestNewCodexToolCorrector_PreInitsToolMetricsAtZero(t *testing.T) {
	_ = NewCodexToolCorrector()

	// 矫正映射有 21 个 from->to kind，预初始化后应至少有这么多 series。
	if n := testutil.CollectAndCount(metrics.ToolCorrectionTotal); n < len(codexToolNameMapping) {
		t.Fatalf("tool_correction_total series=%d, want >= %d (pre-init missing)", n, len(codexToolNameMapping))
	}
	// 具体 kind 应存在且为 0。
	if v := testutil.ToFloat64(metrics.ToolCorrectionTotal.WithLabelValues("apply_patch->edit")); v != 0 {
		t.Fatalf("apply_patch->edit want 0, got %v", v)
	}
	// tool_error 的已知 kind 也应被预置为 0。
	if v := testutil.ToFloat64(metrics.ToolErrorTotal.WithLabelValues(metrics.ToolErrorEmptyName)); v != 0 {
		t.Fatalf("tool_error empty_tool_name want 0, got %v", v)
	}
}
