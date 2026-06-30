package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// init() 应已把固定 label 集的截断/中断指标预置出来，
// 使其在尚未发生任何事件时即以 0 出现（而非 Grafana "No data"）。
// 用 CollectAndCount 校验 series 数量——若 init 未预初始化则计数不足，测试失败。
func TestPreInitFixedLabelSeries(t *testing.T) {
	if n := testutil.CollectAndCount(StreamTruncationTotal); n < 2 {
		t.Fatalf("stream_truncation_total series=%d, want >= 2 (upstream/client preinit missing)", n)
	}
	if n := testutil.CollectAndCount(RequestInterruptedTotal); n < 2 {
		t.Fatalf("request_interrupted_total series=%d, want >= 2 (slotwait client/timeout preinit missing)", n)
	}
}
