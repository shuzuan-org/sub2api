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
	"github.com/Wei-Shaw/sub2api/internal/pkg/modelsuperset"
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

// SeedAccountUpstreamMetaForTest pre-populates the per-account upstream-models cache so
// tests can exercise the meta path WITHOUT a live httpUpstream (which is nil in unit
// tests, making every account's meta empty). fetchAccountUpstreamModels checks this cache
// first, so a seeded entry flows through buildSupersetModels → /v1/models exactly like a
// real upstream probe. Test-only; never called from production code.
func (s *GatewayService) SeedAccountUpstreamMetaForTest(accountID int64, metas map[string]modelsuperset.ModelMeta) {
	if s == nil || s.modelsListCache == nil {
		return
	}
	s.modelsListCache.Set(accountUpstreamModelsCacheKey(accountID), cloneModelMetaMap(metas), supersetModelsTTL)
}

// cachedAccountUpstreamMeta returns a previously-cached per-account upstream meta map, or
// nil if absent. Read-only; never triggers an upstream call (used by the nil-httpUpstream
// path so seeded test data flows through, and harmless in production where the cache is
// empty until a real probe populates it).
func (s *GatewayService) cachedAccountUpstreamMeta(accountID int64) map[string]modelsuperset.ModelMeta {
	if s == nil || s.modelsListCache == nil {
		return nil
	}
	if cached, found := s.modelsListCache.Get(accountUpstreamModelsCacheKey(accountID)); found {
		if metas, ok := cached.(map[string]modelsuperset.ModelMeta); ok {
			return cloneModelMetaMap(metas)
		}
	}
	return nil
}

// supersetCacheEntry is the cached fusion result for a group.
type supersetCacheEntry struct {
	ids       []string
	origins   map[string]string
	metas     map[string]modelsuperset.ModelMeta // client-facing id → real upstream caps
	upstreams map[string]string                  // client-facing id → real upstream model name
}

// GetSupersetModels returns the deduplicated client-facing model ids for the group's
// anthropic+openai accounts, plus a map of id→origin platform. Returns (nil, nil) when
// the group has no such accounts or every account contributes nothing — the handler
// then falls back to global defaults.
//
// Concurrent misses for the same group collapse through singleflight: the aggregation
// fans out real upstream /v1/models requests (and may trigger OAuth refresh), so without
// this a cache expiry under load would self-DDoS the account pool.
func (s *GatewayService) GetSupersetModels(ctx context.Context, groupID *int64) ([]string, map[string]string, map[string]modelsuperset.ModelMeta, map[string]string) {
	cacheKey := supersetModelsCacheKey(groupID)
	if entry, ok := s.lookupSupersetCache(cacheKey); ok {
		modelsListCacheHitTotal.Add(1)
		return cloneStringSlice(entry.ids), cloneOriginMap(entry.origins), cloneModelMetaMap(entry.metas), cloneOriginMap(entry.upstreams)
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
	return cloneStringSlice(entry.ids), cloneOriginMap(entry.origins), cloneModelMetaMap(entry.metas), cloneOriginMap(entry.upstreams)
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

	// Decide each account's exposed id set and its model_mapping. Every account is
	// fetched now (mapped accounts too) so we can attach REAL upstream metadata
	// (max_input_tokens) to the client-facing id — for a mapped account that means
	// looking up the mapping's VALUE (the true provider model) in the upstream catalog
	// and hanging its caps on the mapping KEY. A mapped account still EXPOSES only its
	// keys; its raw upstream ids never reach the client.
	exposed := make([][]string, len(accounts))
	mappingOf := make([]map[string]string, len(accounts))
	fetched := make([]map[string]modelsuperset.ModelMeta, len(accounts))
	var fetchIdx []int
	for i := range accounts {
		if mapping := accounts[i].GetModelMapping(); len(mapping) > 0 {
			exposed[i] = mapKeys(mapping)
			mappingOf[i] = mapping
		}
		fetchIdx = append(fetchIdx, i)
	}

	// Fetch accounts concurrently with per-account failure isolation: one account's
	// error/timeout must not fail the whole listing. nil httpUpstream (unit tests)
	// short-circuits to no metadata — a mapped account then still exposes its keys
	// (just without real caps), a no-mapping account contributes nothing.
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(supersetFanoutLimit)
	for _, idx := range fetchIdx {
		idx := idx
		acc := accounts[idx]
		g.Go(func() error {
			if s.httpUpstream == nil {
				// No live upstream (unit tests). Still honor a pre-seeded cache entry so
				// tests can inject real meta; an empty cache just yields no metadata.
				if cached := s.cachedAccountUpstreamMeta(acc.ID); cached != nil {
					fetched[idx] = cached
					if mappingOf[idx] == nil {
						exposed[idx] = sortedMetaKeys(cached)
					}
				}
				return nil
			}
			cctx, cancel := context.WithTimeout(gctx, supersetUpstreamTimeout)
			defer cancel()
			metas, ferr := s.fetchAccountUpstreamModels(cctx, &acc)
			if ferr != nil {
				logger.L().Warn("superset: account upstream models fetch failed",
					zap.Int64("account_id", acc.ID),
					zap.String("platform", acc.Platform),
					zap.Error(ferr))
			}
			fetched[idx] = metas
			// No-mapping accounts expose the upstream ids directly.
			if mappingOf[idx] == nil {
				exposed[idx] = sortedMetaKeys(metas)
			}
			return nil // never propagate — failure isolation
		})
	}
	_ = g.Wait() // per-account errors are swallowed above; Wait error is always nil

	// Fuse: dedup across accounts. On an id collision, anthropic wins the origin (drives
	// capability gating). Also attach real upstream metadata to each client-facing id,
	// reverse-looking-up the mapping value for mapped accounts.
	origins := make(map[string]string)
	metaByID := make(map[string]modelsuperset.ModelMeta)
	upstreamByID := make(map[string]string)
	for i := range accounts {
		platform := accounts[i].Platform
		for _, id := range exposed[i] {
			if id == "" {
				continue
			}
			if existing, ok := origins[id]; !ok || (existing != domain.PlatformAnthropic && platform == domain.PlatformAnthropic) {
				origins[id] = platform
			}
			upstreamID := id
			if m := mappingOf[i]; m != nil {
				if v, ok := m[id]; ok {
					upstreamID = v
				}
			}
			// Record the real upstream name this client-facing id resolves to (the mapping
			// value, or the id itself for no-mapping accounts). Used to serve real names to
			// non-Claude/non-Codex clients. First write wins (stable across the fan-out).
			if _, ok := upstreamByID[id]; !ok {
				upstreamByID[id] = upstreamID
			}
			if meta, ok := fetched[i][upstreamID]; ok && meta.MaxInputTokens > 0 {
				// Write first non-zero; prefer anthropic-origin when filling an existing 0.
				if cur, exists := metaByID[id]; !exists || cur.MaxInputTokens == 0 {
					metaByID[id] = meta
				}
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
	return supersetCacheEntry{ids: ids, origins: origins, metas: metaByID, upstreams: upstreamByID}
}

// sortedMetaKeys returns the map's keys sorted, for a stable exposed-id list.
func sortedMetaKeys(m map[string]modelsuperset.ModelMeta) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// fetchAccountUpstreamModels fetches one account's upstream /v1/models catalog with
// per-model metadata (max_input_tokens etc.), dispatched by platform, cached per account.
func (s *GatewayService) fetchAccountUpstreamModels(ctx context.Context, account *Account) (map[string]modelsuperset.ModelMeta, error) {
	cacheKey := accountUpstreamModelsCacheKey(account.ID)
	if s.modelsListCache != nil {
		if cached, found := s.modelsListCache.Get(cacheKey); found {
			if metas, ok := cached.(map[string]modelsuperset.ModelMeta); ok {
				return cloneModelMetaMap(metas), nil
			}
		}
	}

	var metas map[string]modelsuperset.ModelMeta
	var err error
	switch account.Platform {
	case domain.PlatformAnthropic:
		metas, err = s.fetchAnthropicModels(ctx, account)
	case domain.PlatformOpenAI:
		metas, err = s.fetchOpenAIModels(ctx, account)
	default:
		// Should be unreachable: only no-mapping anthropic/openai accounts are fetched.
		// Return an error rather than a dishonest (nil, nil) that reads as "success, empty".
		return nil, fmt.Errorf("superset: unsupported platform for upstream fetch: %s", account.Platform)
	}

	if err == nil && len(metas) > 0 && s.modelsListCache != nil {
		s.modelsListCache.Set(cacheKey, cloneModelMetaMap(metas), supersetModelsTTL)
	}
	return metas, err
}

func (s *GatewayService) fetchAnthropicModels(ctx context.Context, account *Account) (map[string]modelsuperset.ModelMeta, error) {
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

func (s *GatewayService) fetchOpenAIModels(ctx context.Context, account *Account) (map[string]modelsuperset.ModelMeta, error) {
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
func (s *GatewayService) doFetchModels(ctx context.Context, account *Account, endpoint string, setAuth func(http.Header)) (map[string]modelsuperset.ModelMeta, error) {
	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	out := make(map[string]modelsuperset.ModelMeta)
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
			if m.ID == "" {
				continue
			}
			out[m.ID] = modelsuperset.ModelMeta{
				MaxInputTokens:  firstNonZero(m.MaxInputTokens, m.MaxModelLen, m.ContextLength),
				MaxOutputTokens: m.MaxTokens,
			}
		}
		if !body.HasMore || body.LastID == "" {
			break
		}
		afterID = body.LastID
	}
	return out, nil
}

// firstNonZero returns the first non-zero value, or 0 if all are zero.
func firstNonZero(vals ...int) int {
	for _, v := range vals {
		if v != 0 {
			return v
		}
	}
	return 0
}

type modelsPage struct {
	Data []struct {
		ID             string `json:"id"`
		MaxInputTokens int    `json:"max_input_tokens"` // Anthropic /v1/models
		MaxModelLen    int    `json:"max_model_len"`    // OpenAI / SGLang native
		MaxTokens      int    `json:"max_tokens"`       // output cap, when upstream reports it
		ContextLength  int    `json:"context_length"`   // LiteLLM / OpenRouter style
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

// cloneModelMetaMap deep-copies a model-meta map. ModelMeta is a value type, so a
// shallow per-entry copy is a full deep copy. Required because cached/singleflight
// values are shared references that callers must never mutate.
func cloneModelMetaMap(src map[string]modelsuperset.ModelMeta) map[string]modelsuperset.ModelMeta {
	if src == nil {
		return nil
	}
	dst := make(map[string]modelsuperset.ModelMeta, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
