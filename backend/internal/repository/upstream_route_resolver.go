package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// upstreamRouteResolver 调本机代理（sglang-proxy）的 GET /route-info，
// 用账号自己的 api_key 查它命中的路由实际打到哪些上游。
type upstreamRouteResolver struct {
	client *http.Client
}

// NewUpstreamRouteResolver 只用于访问本机代理：不走环境代理、不跟随重定向（避免把 key 带到别处）。
func NewUpstreamRouteResolver() service.UpstreamRouteResolver {
	return &upstreamRouteResolver{client: &http.Client{
		Timeout:   3 * time.Second,
		Transport: &http.Transport{Proxy: nil, MaxIdleConnsPerHost: 4, IdleConnTimeout: 30 * time.Second},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

func (r *upstreamRouteResolver) ResolveRouteUpstreams(ctx context.Context, proxyOrigin, apiKey string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(proxyOrigin, "/")+"/route-info", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", apiKey)
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("route-info: status %d", resp.StatusCode)
	}
	var body struct {
		Upstreams []string `json:"upstreams"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body); err != nil {
		return nil, fmt.Errorf("route-info: decode: %w", err)
	}
	out := make([]string, 0, len(body.Upstreams))
	for _, s := range body.Upstreams {
		// 代理本该只给 origin，这里再规整一遍，防止版本不一致时把路径/凭据带上页面。
		u, err := url.Parse(strings.TrimSpace(s))
		if err != nil || u.Host == "" {
			continue
		}
		out = append(out, strings.ToLower(u.Scheme)+"://"+strings.ToLower(u.Host))
	}
	return out, nil
}
