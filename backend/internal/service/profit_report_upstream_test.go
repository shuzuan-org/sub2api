package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type stubRouteResolver struct {
	mu    sync.Mutex
	calls map[string]int
	resp  map[string][]string // apiKey -> upstreams；不在表里返回错误
}

func (s *stubRouteResolver) ResolveRouteUpstreams(_ context.Context, origin, apiKey string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.calls == nil {
		s.calls = map[string]int{}
	}
	s.calls[origin+"|"+apiKey]++
	if ups, ok := s.resp[apiKey]; ok {
		return ups, nil
	}
	return nil, errors.New("401")
}

func TestBaseURLOrigin(t *testing.T) {
	cases := []struct {
		in, origin string
		loopback   bool
	}{
		{"https://user:pw@KSPMAS.ksyun.com/v1?x=1", "https://kspmas.ksyun.com", false},
		{"http://127.0.0.1:8188", "http://127.0.0.1:8188", true},
		{"http://localhost:8188/", "http://localhost:8188", true},
		{"http://[::1]:8188", "http://[::1]:8188", true},
		{"http://167.172.89.103:3000", "http://167.172.89.103:3000", false},
		{"", "", false},
		{"127.0.0.1:8188", "", false},
		{"ftp://x", "", false},
	}
	for _, c := range cases {
		o, lb := baseURLOrigin(c.in)
		require.Equal(t, c.origin, o, c.in)
		require.Equal(t, c.loopback, lb, c.in)
	}
}

func TestAnnotateUpstreams(t *testing.T) {
	res := &stubRouteResolver{resp: map[string][]string{
		"paratera": {"https://llmapi.paratera.com"},
		"multi":    {"https://a.example.com", "https://b.example.com"},
	}}
	svc := NewProfitReportService(&stubProfitReportRepo{}, res)
	mk := func() []ProfitReportAccount {
		return []ProfitReportAccount{
			{ID: 1, BaseURL: "https://open.bigmodel.cn/api", APIKey: "sk-public"},
			{ID: 2, BaseURL: "http://127.0.0.1:8188", APIKey: "paratera"},
			{ID: 3, BaseURL: "http://127.0.0.1:8188/", APIKey: "paratera"},
			{ID: 4, BaseURL: "http://127.0.0.1:8188", APIKey: "multi"},
			{ID: 5, BaseURL: "http://127.0.0.1:8188", APIKey: "unknown"},
			{ID: 6},
		}
	}

	accts := mk()
	svc.annotateUpstreams(context.Background(), accts)
	want := []struct{ up, src string }{
		{"https://open.bigmodel.cn", UpstreamSourceBaseURL},
		{"https://llmapi.paratera.com", UpstreamSourceProxy},
		{"https://llmapi.paratera.com", UpstreamSourceProxy},
		{"https://a.example.com, https://b.example.com", UpstreamSourceProxy},
		{"http://127.0.0.1:8188", UpstreamSourceProxyUnresolved},
		{"", ""},
	}
	for i, w := range want {
		require.Equal(t, w.up, accts[i].Upstream, "account %d", accts[i].ID)
		require.Equal(t, w.src, accts[i].UpstreamSource, "account %d", accts[i].ID)
		require.Empty(t, accts[i].APIKey, "api key must not leave the service")
	}
	require.Equal(t, 1, res.calls["http://127.0.0.1:8188|paratera"], "same proxy+token is resolved once")
	require.Zero(t, res.calls["https://open.bigmodel.cn|sk-public"], "public base_url is never queried")

	// 第二次走缓存（失败结果也短暂缓存，不会每次刷新都打代理）。
	svc.annotateUpstreams(context.Background(), mk())
	require.Equal(t, 1, res.calls["http://127.0.0.1:8188|paratera"])
	require.Equal(t, 1, res.calls["http://127.0.0.1:8188|unknown"])
}

func TestAnnotateUpstreamsWithoutResolver(t *testing.T) {
	svc := NewProfitReportService(&stubProfitReportRepo{}, nil)
	accts := []ProfitReportAccount{{ID: 1, BaseURL: "http://127.0.0.1:8188", APIKey: "k"}}
	svc.annotateUpstreams(context.Background(), accts)
	require.Equal(t, "http://127.0.0.1:8188", accts[0].Upstream)
	require.Equal(t, UpstreamSourceBaseURL, accts[0].UpstreamSource)
	require.Empty(t, accts[0].APIKey)
}
