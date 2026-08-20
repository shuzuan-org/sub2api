package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestAccountPoolCollectorFoldsDuplicateLabelSets 复现生产 /metrics 返回 500 的场景：
// 账号 model mapping 里同时存在 "GLM-5.2" 与 "glm-5.2" 两种写法，NormalizeModel
// 把它们折叠成同一个标签值。Collector 若逐条发出，registry 会报
// "collected before with the same name and label values"，整个 /metrics 一起 500。
func TestAccountPoolCollectorFoldsDuplicateLabelSets(t *testing.T) {
	SetAllowedModels([]string{"glm-5.2"})
	t.Cleanup(func() { SetAllowedModels(nil) })

	c := newAccountPoolCollector(func() []AccountPoolStat {
		return []AccountPoolStat{
			{Platform: "anthropic", Model: "glm-5.2", Total: 3, Available: 3},
			{Platform: "anthropic", Model: "GLM-5.2", Total: 2, Available: 1, Unavailable: 1},
		}
	})

	reg := prometheus.NewPedanticRegistry()
	if err := reg.Register(c); err != nil {
		t.Fatalf("register collector: %v", err)
	}

	if _, err := reg.Gather(); err != nil {
		t.Fatalf("gather must not fail on folded model names: %v", err)
	}

	expected := `
# HELP sub2api_account_pool_available Number of schedulable (available) accounts by platform and model.
# TYPE sub2api_account_pool_available gauge
sub2api_account_pool_available{model="glm-5.2",platform="anthropic"} 4
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "sub2api_account_pool_available"); err != nil {
		t.Errorf("folded stats should be merged into one series: %v", err)
	}
}

// TestAccountPoolCollectorSkipsOtherBucket 白名单外的模型不进 __other__ 桶，
// 避免账号池指标被无关模型污染。
func TestAccountPoolCollectorSkipsOtherBucket(t *testing.T) {
	SetAllowedModels([]string{"glm-5.2"})
	t.Cleanup(func() { SetAllowedModels(nil) })

	c := newAccountPoolCollector(func() []AccountPoolStat {
		return []AccountPoolStat{
			{Platform: "anthropic", Model: "some-unknown-model", Total: 9, Available: 9},
			{Platform: "anthropic", Model: "", Total: 7, Available: 7},
		}
	})

	if n := testutil.CollectAndCount(c); n != 0 {
		t.Errorf("expected no series for __other__/empty models, got %d", n)
	}
}
