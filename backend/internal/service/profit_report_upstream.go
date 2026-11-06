package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"
)

// 收入看板「上游」列：账号实际打到哪个供应商。
//   - base_url 是公网地址 → 直接显示它的 origin（scheme://host[:port]）。
//   - base_url 是本机代理（sglang-proxy 127.0.0.1:8188）→ 用账号自己的 api_key 去问代理
//     GET /route-info，拿到该 token 路由的真实上游；问不到时退回显示代理地址并标记。

const (
	UpstreamSourceBaseURL         = "base_url"
	UpstreamSourceProxy           = "proxy"
	UpstreamSourceProxyUnresolved = "proxy_unresolved"

	upstreamCacheTTL           = 5 * time.Minute
	upstreamFailCacheTTL       = 30 * time.Second
	upstreamResolveTimeout     = 3 * time.Second
	upstreamResolveConcurrency = 8
)

// UpstreamRouteResolver 向本机代理查询某个 token 命中路由的真实上游 origin。
type UpstreamRouteResolver interface {
	ResolveRouteUpstreams(ctx context.Context, proxyOrigin, apiKey string) ([]string, error)
}

type upstreamCacheEntry struct {
	upstreams []string
	ok        bool
	expires   time.Time
}

type upstreamCache struct {
	mu      sync.Mutex
	entries map[string]upstreamCacheEntry
}

// baseURLOrigin 把 base_url 规整成 scheme://host[:port]，去掉 userinfo/路径/查询串；
// loopback 表示它指向本机（需要再问代理才知道真实上游）。
func baseURLOrigin(raw string) (origin string, loopback bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", false
	}
	origin = u.Scheme + "://" + strings.ToLower(u.Host)
	host := strings.ToLower(u.Hostname())
	if host == "localhost" {
		return origin, true
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return origin, true
	}
	return origin, false
}

func upstreamCacheKey(origin, apiKey string) string {
	sum := sha256.Sum256([]byte(origin + "\x00" + apiKey))
	return hex.EncodeToString(sum[:])
}

// annotateUpstreams 填充每个账号的 Upstream/UpstreamSource，并清掉 APIKey（不出服务）。
// 代理查询失败不影响看板主体，只把该账号标成 proxy_unresolved。
func (s *ProfitReportService) annotateUpstreams(ctx context.Context, accounts []ProfitReportAccount) {
	type job struct{ origin, apiKey string }
	jobs := make(map[string]job)
	keys := make([]string, len(accounts))
	for i := range accounts {
		a := &accounts[i]
		origin, loopback := baseURLOrigin(a.BaseURL)
		a.Upstream = origin
		if origin != "" {
			a.UpstreamSource = UpstreamSourceBaseURL
		}
		if loopback && s.routes != nil && a.APIKey != "" {
			a.UpstreamSource = UpstreamSourceProxyUnresolved
			k := upstreamCacheKey(origin, a.APIKey)
			keys[i] = k
			jobs[k] = job{origin: origin, apiKey: a.APIKey}
		}
		a.APIKey = ""
	}
	if len(jobs) == 0 {
		return
	}

	now := time.Now()
	resolved := make(map[string][]string, len(jobs))
	s.upstreams.mu.Lock()
	if s.upstreams.entries == nil {
		s.upstreams.entries = make(map[string]upstreamCacheEntry)
	}
	for k := range jobs {
		if e, ok := s.upstreams.entries[k]; ok && now.Before(e.expires) {
			if e.ok {
				resolved[k] = e.upstreams
			}
			delete(jobs, k)
		}
	}
	s.upstreams.mu.Unlock()

	if len(jobs) > 0 {
		ctx, cancel := context.WithTimeout(ctx, upstreamResolveTimeout)
		defer cancel()
		var (
			wg  sync.WaitGroup
			mu  sync.Mutex
			sem = make(chan struct{}, upstreamResolveConcurrency)
		)
		fresh := make(map[string]upstreamCacheEntry, len(jobs))
		for k, j := range jobs {
			wg.Add(1)
			sem <- struct{}{}
			go func(k string, j job) {
				defer wg.Done()
				defer func() { <-sem }()
				ups, err := s.routes.ResolveRouteUpstreams(ctx, j.origin, j.apiKey)
				e := upstreamCacheEntry{expires: time.Now().Add(upstreamFailCacheTTL)}
				if err == nil && len(ups) > 0 {
					e = upstreamCacheEntry{upstreams: ups, ok: true, expires: time.Now().Add(upstreamCacheTTL)}
				}
				mu.Lock()
				fresh[k] = e
				mu.Unlock()
			}(k, j)
		}
		wg.Wait()
		s.upstreams.mu.Lock()
		for k, e := range fresh {
			s.upstreams.entries[k] = e
			if e.ok {
				resolved[k] = e.upstreams
			}
		}
		s.upstreams.mu.Unlock()
	}

	for i := range accounts {
		if ups, ok := resolved[keys[i]]; ok && keys[i] != "" {
			accounts[i].Upstream = strings.Join(ups, ", ")
			accounts[i].UpstreamSource = UpstreamSourceProxy
		}
	}
}
