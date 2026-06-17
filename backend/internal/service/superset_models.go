package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

// superset_models.go — GET /v1/models dual-protocol fusion.
//
// GetSupersetModels fuses the REAL upstream model catalogs of a group's anthropic +
// openai accounts into one deduplicated client-facing id set, so /v1/models can emit a
// dual-protocol superset usable by both Claude Code and Codex clients. Unlike
// GetAvailableModels (which only reads each account's configured model_mapping keys),
// this calls each account's upstream /v1/models. Failures are isolated per-account and
// fall back to the account's model_mapping; if everything is empty the handler falls
// back to global defaults.

const (
	supersetModelsTTL       = 60 * time.Second // upstream calls are heavier than mapping reads
	supersetEmptyTTL        = 10 * time.Second // short TTL for empty results: recover fast on misconfig, but don't re-fan-out the upstream on every request for a persistently-empty group
	supersetUpstreamTimeout = 5 * time.Second
	supersetFanoutLimit     = 8
	supersetBodyLimit       = 1 << 20 // 1 MiB cap on upstream /v1/models bodies
	modelsPageLimit         = 10      // backstop: max pages to follow if upstream never sets has_more=false
)

func supersetModelsCacheKey(groupID *int64) string {
	return fmt.Sprintf("superset|%d", derefGroupID(groupID))
}

func accountUpstreamModelsCacheKey(accountID int64) string {
	return fmt.Sprintf("acctmodels|%d", accountID)
}

// supersetCacheEntry is the cached fusion result for a group.
type supersetCacheEntry struct {
	ids     []string
	origins map[string]string
}

// GetSupersetModels returns the deduplicated client-facing model ids for the group's
// anthropic+openai accounts, plus a map of id→origin platform. Returns (nil, nil) when
// the group has no such accounts or every account contributes nothing — the handler
// then falls back to global defaults.
//
// Concurrent misses for the same group collapse through singleflight: the aggregation
// fans out real upstream /v1/models requests (and may trigger OAuth refresh), so without
// this a cache expiry under load would self-DDoS the account pool.
func (s *GatewayService) GetSupersetModels(ctx context.Context, groupID *int64) ([]string, map[string]string) {
	cacheKey := supersetModelsCacheKey(groupID)
	if entry, ok := s.lookupSupersetCache(cacheKey); ok {
		modelsListCacheHitTotal.Add(1)
		return cloneStringSlice(entry.ids), cloneOriginMap(entry.origins)
	}
	modelsListCacheMissTotal.Add(1)

	// singleflight returns the SAME shared value to all callers waiting on this key, so
	// every caller must clone before returning (never hand out the shared slice/map).
	v, _, _ := s.supersetSF.Do(cacheKey, func() (any, error) {
		// Re-check the cache inside the flight: an earlier flight for this key may have
		// just populated it, in which case we skip the upstream fan-out entirely.
		if entry, ok := s.lookupSupersetCache(cacheKey); ok {
			return entry, nil
		}
		entry := s.buildSupersetModels(ctx, groupID)
		if s.modelsListCache != nil {
			// Cache empty results too, but with a short TTL: a persistently-empty group
			// (misconfig / all upstreams down) must not re-fan-out the upstream on every
			// single request. singleflight collapses concurrent misses; this collapses
			// serial ones for the empty case.
			ttl := supersetModelsTTL
			if len(entry.ids) == 0 {
				ttl = supersetEmptyTTL
			}
			s.modelsListCache.Set(cacheKey, entry, ttl)
			modelsListCacheStoreTotal.Add(1)
		}
		return entry, nil
	})
	entry, _ := v.(supersetCacheEntry)
	return cloneStringSlice(entry.ids), cloneOriginMap(entry.origins)
}

func (s *GatewayService) lookupSupersetCache(cacheKey string) (supersetCacheEntry, bool) {
	if s.modelsListCache == nil {
		return supersetCacheEntry{}, false
	}
	cached, found := s.modelsListCache.Get(cacheKey)
	if !found {
		return supersetCacheEntry{}, false
	}
	entry, ok := cached.(supersetCacheEntry)
	return entry, ok
}

// buildSupersetModels does the uncached aggregation: fetch each no-mapping account's
// upstream catalog (concurrently, failure-isolated), reconcile against per-account
// model_mapping, and fuse into one sorted id set with origins. Always called under the
// singleflight flight for its cache key.
func (s *GatewayService) buildSupersetModels(ctx context.Context, groupID *int64) supersetCacheEntry {
	platforms := []string{domain.PlatformAnthropic, domain.PlatformOpenAI}
	var accounts []Account
	var err error
	if groupID != nil {
		accounts, err = s.accountRepo.ListSchedulableByGroupIDAndPlatforms(ctx, *groupID, platforms)
	} else {
		accounts, err = s.accountRepo.ListSchedulable(ctx)
		if err == nil {
			accounts = filterAccountsByPlatforms(accounts, platforms)
		}
	}
	if err != nil || len(accounts) == 0 {
		return supersetCacheEntry{}
	}

	// Decide each account's exposed id set. An account WITH a model_mapping exposes its
	// keys (client-facing names) and is NOT fetched — its raw upstream catalog would be
	// discarded by reconciliation anyway, so fetching it just burns an upstream round
	// trip (and possibly an OAuth refresh). Only no-mapping accounts hit the network.
	exposed := make([][]string, len(accounts))
	var fetchIdx []int
	for i := range accounts {
		if mapping := accounts[i].GetModelMapping(); len(mapping) > 0 {
			exposed[i] = mapKeys(mapping)
		} else {
			fetchIdx = append(fetchIdx, i)
		}
	}

	// Fetch no-mapping accounts concurrently with per-account failure isolation: one
	// account's error/timeout must not fail the whole listing.
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(supersetFanoutLimit)
	for _, idx := range fetchIdx {
		idx := idx
		acc := accounts[idx]
		g.Go(func() error {
			cctx, cancel := context.WithTimeout(gctx, supersetUpstreamTimeout)
			defer cancel()
			ids, ferr := s.fetchAccountUpstreamModels(cctx, &acc)
			if ferr != nil {
				logger.L().Warn("superset: account upstream models fetch failed",
					zap.Int64("account_id", acc.ID),
					zap.String("platform", acc.Platform),
					zap.Error(ferr))
			}
			exposed[idx] = ids
			return nil // never propagate — failure isolation
		})
	}
	_ = g.Wait() // per-account errors are swallowed above; Wait error is always nil

	// Fuse: dedup across accounts. On an id collision, anthropic wins the origin (drives
	// capability gating).
	origins := make(map[string]string)
	for i := range accounts {
		platform := accounts[i].Platform
		for _, id := range exposed[i] {
			if id == "" {
				continue
			}
			if existing, ok := origins[id]; !ok || (existing != domain.PlatformAnthropic && platform == domain.PlatformAnthropic) {
				origins[id] = platform
			}
		}
	}

	if len(origins) == 0 {
		return supersetCacheEntry{}
	}
	ids := make([]string, 0, len(origins))
	for id := range origins {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return supersetCacheEntry{ids: ids, origins: origins}
}

// fetchAccountUpstreamModels fetches one account's upstream /v1/models id list,
// dispatched by platform, cached per account.
func (s *GatewayService) fetchAccountUpstreamModels(ctx context.Context, account *Account) ([]string, error) {
	cacheKey := accountUpstreamModelsCacheKey(account.ID)
	if s.modelsListCache != nil {
		if cached, found := s.modelsListCache.Get(cacheKey); found {
			if ids, ok := cached.([]string); ok {
				return cloneStringSlice(ids), nil
			}
		}
	}

	var ids []string
	var err error
	switch account.Platform {
	case domain.PlatformAnthropic:
		ids, err = s.fetchAnthropicModels(ctx, account)
	case domain.PlatformOpenAI:
		ids, err = s.fetchOpenAIModels(ctx, account)
	default:
		// Should be unreachable: only no-mapping anthropic/openai accounts are fetched.
		// Return an error rather than a dishonest (nil, nil) that reads as "success, empty".
		return nil, fmt.Errorf("superset: unsupported platform for upstream fetch: %s", account.Platform)
	}

	if err == nil && len(ids) > 0 && s.modelsListCache != nil {
		s.modelsListCache.Set(cacheKey, cloneStringSlice(ids), supersetModelsTTL)
	}
	return ids, err
}

func (s *GatewayService) fetchAnthropicModels(ctx context.Context, account *Account) ([]string, error) {
	token, tokenType, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("get access token: %w", err)
	}
	base := account.GetBaseURL()
	if base == "" {
		base = "https://api.anthropic.com"
	}
	base, err = s.validateUpstreamBaseURL(base)
	if err != nil {
		return nil, fmt.Errorf("validate base url: %w", err)
	}
	setAuth := func(h http.Header) {
		h.Set("anthropic-version", "2023-06-01")
		if tokenType == "oauth" {
			h.Set("Authorization", "Bearer "+token)
		} else {
			h.Set("x-api-key", token)
		}
	}
	return s.doFetchModels(ctx, account, strings.TrimRight(base, "/")+"/v1/models", setAuth)
}

func (s *GatewayService) fetchOpenAIModels(ctx context.Context, account *Account) ([]string, error) {
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("get access token: %w", err)
	}
	base := account.GetOpenAIBaseURL()
	if base == "" {
		base = "https://api.openai.com"
	}
	base, err = s.validateUpstreamBaseURL(base)
	if err != nil {
		return nil, fmt.Errorf("validate base url: %w", err)
	}
	setAuth := func(h http.Header) {
		h.Set("Authorization", "Bearer "+token) // openai: always Bearer (oauth or apikey)
	}
	return s.doFetchModels(ctx, account, strings.TrimRight(base, "/")+"/v1/models", setAuth)
}

// doFetchModels fetches the full model id list from an upstream /v1/models endpoint,
// following cursor pagination. Anthropic's /v1/models defaults to limit=20 and returns
// {data:[{id}], has_more, last_id}; we request limit=1000 (its max) and follow
// has_more/last_id via after_id so a catalog larger than one page is fully listed — a
// missed page would make /v1/models disagree with the real upstream catalog (model
// present upstream but 404 on /v1/models/{id}). OpenAI returns the full list in one page
// with has_more absent/false, so the loop terminates after one iteration for it.
// modelsPageLimit caps total pages as a backstop against an upstream that never sets
// has_more=false.
func (s *GatewayService) doFetchModels(ctx context.Context, account *Account, endpoint string, setAuth func(http.Header)) ([]string, error) {
	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	var ids []string
	afterID := ""
	for page := 0; page < modelsPageLimit; page++ {
		u, err := url.Parse(endpoint)
		if err != nil {
			return nil, fmt.Errorf("parse /v1/models url: %w", err)
		}
		q := u.Query()
		q.Set("limit", "1000")
		if afterID != "" {
			q.Set("after_id", afterID)
		}
		u.RawQuery = q.Encode()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		setAuth(req.Header)

		body, err := s.fetchModelsPage(req, proxyURL, account)
		if err != nil {
			return nil, err
		}
		for _, m := range body.Data {
			if m.ID != "" {
				ids = append(ids, m.ID)
			}
		}
		if !body.HasMore || body.LastID == "" {
			break
		}
		afterID = body.LastID
	}
	return ids, nil
}

type modelsPage struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
	HasMore bool   `json:"has_more"`
	LastID  string `json:"last_id"`
}

func (s *GatewayService) fetchModelsPage(req *http.Request, proxyURL string, account *Account) (modelsPage, error) {
	var body modelsPage
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		return body, fmt.Errorf("upstream request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Drain so the connection can be reused (Go won't pool a connection with an
		// unread body).
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, supersetBodyLimit))
		return body, fmt.Errorf("upstream /v1/models status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, supersetBodyLimit)).Decode(&body); err != nil {
		return body, fmt.Errorf("decode /v1/models: %w", err)
	}
	return body, nil
}

func filterAccountsByPlatforms(accounts []Account, platforms []string) []Account {
	allow := make(map[string]struct{}, len(platforms))
	for _, p := range platforms {
		allow[p] = struct{}{}
	}
	out := make([]Account, 0, len(accounts))
	for _, acc := range accounts {
		if _, ok := allow[acc.Platform]; ok {
			out = append(out, acc)
		}
	}
	return out
}

func mapKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func cloneOriginMap(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
