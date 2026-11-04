//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/metrics"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

type probeKeyStub struct{ ids map[string]int64 }

func (p probeKeyStub) GetByKey(_ context.Context, key string) (*APIKey, error) {
	id, ok := p.ids[key]
	if !ok {
		return nil, ErrAPIKeyNotFound
	}
	return &APIKey{ID: id}, nil
}

type probeUsageStub struct {
	mu   sync.Mutex
	logs map[int64][]UsageLog
}

func (p *probeUsageStub) ListByAPIKeyAndTimeRange(_ context.Context, id int64, _, _ time.Time) ([]UsageLog, *pagination.PaginationResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.logs[id], nil, nil
}

func newProbeForTest(t *testing.T, cfg config.ProbeConfig, handler http.HandlerFunc, usage *probeUsageStub) *SyntheticProbeService {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	cfg.Enabled = true
	cfg.BaseURL = srv.URL
	cfg.TimeoutSeconds = 5
	cfg.MaxTokens = 8
	s := NewSyntheticProbeService(cfg, probeKeyStub{ids: map[string]int64{"sk-a": 1, "sk-b": 2}}, usage)
	s.sleep = func(context.Context, time.Duration) bool { return true }
	return s
}

func TestSyntheticProbe_SuccessRequiresMetering(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]string{}
	handler := func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		seen[body["model"].(string)] = r.URL.Path + " " + r.Header.Get("Authorization")
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}
	usage := &probeUsageStub{logs: map[int64][]UsageLog{
		1: {{RequestedModel: "claude-haiku", Model: "claude-haiku-upstream"}},
		// sk-b: openai 模型 2xx 但没有 usage_logs → metered=false
	}}
	s := newProbeForTest(t, config.ProbeConfig{
		APIKey:               "sk-a",
		MeteringDelaySeconds: 1,
		Targets: []config.ProbeTarget{
			{Model: "claude-haiku"},
			{Model: "gpt-mini", Protocol: "openai", APIKey: "sk-b"},
		},
	}, handler, usage)

	results := s.RunOnce(context.Background())
	require.Len(t, results, 2)

	require.Equal(t, "/v1/messages Bearer sk-a", seen["claude-haiku"])
	require.Equal(t, "/v1/chat/completions Bearer sk-b", seen["gpt-mini"])

	require.True(t, results[0].HTTPOK)
	require.True(t, results[0].Metered)
	require.True(t, results[1].HTTPOK)
	require.False(t, results[1].Metered, "2xx without a usage_logs row must not count as success")

	require.Equal(t, 1.0, testutil.ToFloat64(metrics.ProbeModelOK.WithLabelValues("claude-haiku")))
	require.Equal(t, 0.0, testutil.ToFloat64(metrics.ProbeModelOK.WithLabelValues("gpt-mini")))
	require.Equal(t, 1.0, testutil.ToFloat64(metrics.ProbeModelHTTPOK.WithLabelValues("gpt-mini")))
	require.Greater(t, testutil.ToFloat64(metrics.ProbeLastRunTimestamp), 0.0)
}

func TestSyntheticProbe_UpstreamErrorRecordsStatus(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"type":"api_error","message":"No available accounts"}}`))
	}
	s := newProbeForTest(t, config.ProbeConfig{
		APIKey:  "sk-a",
		Targets: []config.ProbeTarget{{Model: "probe-503"}},
	}, handler, &probeUsageStub{})

	results := s.RunOnce(context.Background())
	require.Len(t, results, 1)
	require.False(t, results[0].HTTPOK)
	require.Equal(t, 503, results[0].HTTPStatus)
	require.Equal(t, 503.0, testutil.ToFloat64(metrics.ProbeModelStatus.WithLabelValues("probe-503")))
	require.Equal(t, 0.0, testutil.ToFloat64(metrics.ProbeModelOK.WithLabelValues("probe-503")))
}

func TestSyntheticProbe_DisabledIsNoop(t *testing.T) {
	s := NewSyntheticProbeService(config.ProbeConfig{Enabled: false, Targets: []config.ProbeTarget{{Model: "x"}}}, nil, nil)
	require.Nil(t, s.RunOnce(context.Background()))
	s.Start()
	s.Stop()
}

func TestResetProbeModelsDropsRemovedModels(t *testing.T) {
	metrics.ResetProbeModels([]string{"probe-keep", "probe-drop"})
	metrics.RecordProbeRound([]metrics.ProbeResult{
		{Model: "probe-keep", HTTPOK: true, Metered: true},
		{Model: "probe-drop"},
	}, time.Now())
	require.True(t, hasProbeSeries(t, "probe-keep"))
	require.True(t, hasProbeSeries(t, "probe-drop"))

	metrics.ResetProbeModels([]string{"probe-keep"})
	require.True(t, hasProbeSeries(t, "probe-keep"))
	require.False(t, hasProbeSeries(t, "probe-drop"), "a model removed from config must not linger at 0 and keep alerting")
}

func hasProbeSeries(t *testing.T, model string) bool {
	t.Helper()
	ch := make(chan prometheus.Metric, 64)
	metrics.ProbeModelOK.Collect(ch)
	close(ch)
	for m := range ch {
		var pb dto.Metric
		require.NoError(t, m.Write(&pb))
		for _, l := range pb.GetLabel() {
			if l.GetName() == "model" && l.GetValue() == model {
				return true
			}
		}
	}
	return false
}
