//go:build unit

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	gocache "github.com/patrickmn/go-cache"
)

// realDoUpstream implements HTTPUpstream by actually performing the request with a
// standard client, so fetchAccountUpstreamModels exercises real URL/header/parse logic
// against an httptest.Server. doCalls is atomic so concurrency tests can assert on it.
type realDoUpstream struct {
	client  *http.Client
	doCalls atomic.Int64
}

func (u *realDoUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.doCalls.Add(1)
	return u.client.Do(req)
}

func (u *realDoUpstream) DoWithTLS(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, "", 0, 0)
}

// supersetTestConfig returns a config that lets validateUpstreamBaseURL accept a
// localhost http test server (allowlist disabled, insecure http allowed).
func supersetTestConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	return cfg
}

func apikeyAccount(id int64, platform, baseURL, apiKey string, mapping map[string]any) Account {
	creds := map[string]any{"api_key": apiKey}
	if baseURL != "" {
		creds["base_url"] = baseURL
	}
	if mapping != nil {
		creds["model_mapping"] = mapping
	}
	return Account{
		ID:            id,
		Platform:      platform,
		Type:          AccountTypeAPIKey,
		Status:        StatusActive,
		Schedulable:   true,
		Credentials:   creds,
		AccountGroups: []AccountGroup{{GroupID: supersetTestGroupID}},
	}
}

const supersetTestGroupID int64 = 100

func supersetGroup() *int64 {
	g := supersetTestGroupID
	return &g
}

func newSupersetService(t *testing.T, repo AccountRepository, up HTTPUpstream) *GatewayService {
	t.Helper()
	return &GatewayService{
		cfg:                supersetTestConfig(),
		accountRepo:        repo,
		httpUpstream:       up,
		modelsListCache:    gocache.New(time.Minute, time.Minute),
		modelsListCacheTTL: time.Minute,
	}
}

func TestGetSupersetModels_FusesAndDedups(t *testing.T) {
	anthropicSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":"claude-opus-4-8"},{"id":"shared-model"}]}`))
	}))
	defer anthropicSrv.Close()
	openaiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"object":"list","data":[{"id":"gpt-5"},{"id":"shared-model"}]}`))
	}))
	defer openaiSrv.Close()

	accounts := []Account{
		apikeyAccount(1, PlatformAnthropic, anthropicSrv.URL, "ak-ant", nil),
		apikeyAccount(2, PlatformOpenAI, openaiSrv.URL, "ak-oai", nil),
	}
	repo := newGroupAwareMockRepo(accounts)
	up := &realDoUpstream{client: &http.Client{}}
	s := newSupersetService(t, repo, up)

	ids, origins, _ := s.GetSupersetModels(context.Background(), supersetGroup())
	want := map[string]string{
		"claude-opus-4-8": PlatformAnthropic,
		"gpt-5":           PlatformOpenAI,
		"shared-model":    PlatformAnthropic, // anthropic wins the tie
	}
	if len(ids) != len(want) {
		t.Fatalf("ids=%v want %d entries", ids, len(want))
	}
	for id, wantOrigin := range want {
		if origins[id] != wantOrigin {
			t.Errorf("origin[%q]=%q want %q", id, origins[id], wantOrigin)
		}
	}
}

func TestGetSupersetModels_NoMappingPropagatesMaxInputTokens(t *testing.T) {
	// No-mapping anthropic account: the upstream's real max_input_tokens must be carried
	// through to metas (not dropped, not re-guessed).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":"minimax-m2.7","max_input_tokens":196608}]}`))
	}))
	defer srv.Close()
	accounts := []Account{apikeyAccount(1, PlatformAnthropic, srv.URL, "ak", nil)}
	s := newSupersetService(t, newGroupAwareMockRepo(accounts), &realDoUpstream{client: &http.Client{}})

	ids, _, metas := s.GetSupersetModels(context.Background(), supersetGroup())
	if len(ids) != 1 || ids[0] != "minimax-m2.7" {
		t.Fatalf("ids=%v want [minimax-m2.7]", ids)
	}
	if metas["minimax-m2.7"].MaxInputTokens != 196608 {
		t.Errorf("metas[minimax-m2.7].MaxInputTokens=%d want 196608 (real upstream passthrough)", metas["minimax-m2.7"].MaxInputTokens)
	}
}

func TestGetSupersetModels_OpenAIMaxModelLen(t *testing.T) {
	// OpenAI/SGLang-style catalog reports max_model_len; it normalizes into MaxInputTokens.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"object":"list","data":[{"id":"local-model","max_model_len":131072}]}`))
	}))
	defer srv.Close()
	accounts := []Account{apikeyAccount(1, PlatformOpenAI, srv.URL, "ak", nil)}
	s := newSupersetService(t, newGroupAwareMockRepo(accounts), &realDoUpstream{client: &http.Client{}})

	_, _, metas := s.GetSupersetModels(context.Background(), supersetGroup())
	if metas["local-model"].MaxInputTokens != 131072 {
		t.Errorf("max_model_len not normalized: got %d want 131072", metas["local-model"].MaxInputTokens)
	}
}

func TestGetSupersetModels_MappedAccountFetchedForMetadata(t *testing.T) {
	// An account WITH a model_mapping still EXPOSES only its keys (raw upstream ids never
	// reach the client), but it IS now fetched so we can attach the real upstream
	// max_input_tokens of the mapping VALUE to the mapping KEY.
	var anthropicHits atomic.Int64
	anthropicSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		anthropicHits.Add(1)
		w.Write([]byte(`{"data":[{"id":"upstream-x","max_input_tokens":196608}]}`))
	}))
	defer anthropicSrv.Close()
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"object":"list","data":[{"id":"gpt-5"}]}`))
	}))
	defer okSrv.Close()

	accounts := []Account{
		apikeyAccount(1, PlatformAnthropic, anthropicSrv.URL, "ak-ant", map[string]any{"claude-mapped": "upstream-x"}),
		apikeyAccount(2, PlatformOpenAI, okSrv.URL, "ak-oai", nil),
	}
	s := newSupersetService(t, newGroupAwareMockRepo(accounts), &realDoUpstream{client: &http.Client{}})

	ids, origins, metas := s.GetSupersetModels(context.Background(), supersetGroup())
	got := make(map[string]bool)
	for _, id := range ids {
		got[id] = true
	}
	if !got["claude-mapped"] {
		t.Error("expected mapped account's key 'claude-mapped'")
	}
	if !got["gpt-5"] {
		t.Error("expected openai 'gpt-5' from successful fetch")
	}
	if got["upstream-x"] {
		t.Error("mapped account's raw upstream id must NOT appear")
	}
	if origins["claude-mapped"] != PlatformAnthropic {
		t.Errorf("origin=%q want anthropic", origins["claude-mapped"])
	}
	// mapped account IS fetched now (so we can harvest metadata).
	if hits := anthropicHits.Load(); hits < 1 {
		t.Errorf("mapped account fetched %d times; want >=1 (needed for metadata)", hits)
	}
	// The mapping VALUE's real window (196608) is attached to the KEY.
	if metas["claude-mapped"].MaxInputTokens != 196608 {
		t.Errorf("metas[claude-mapped].MaxInputTokens=%d want 196608 (reverse-looked-up from value)", metas["claude-mapped"].MaxInputTokens)
	}
}

func TestGetSupersetModels_NoMappingFetchFailureContributesNothing(t *testing.T) {
	// A no-mapping account whose upstream fails contributes nothing, but a sibling
	// no-mapping account that succeeds is still listed (failure isolation).
	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failSrv.Close()
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"object":"list","data":[{"id":"gpt-5"}]}`))
	}))
	defer okSrv.Close()

	accounts := []Account{
		apikeyAccount(1, PlatformAnthropic, failSrv.URL, "ak-ant", nil),
		apikeyAccount(2, PlatformOpenAI, okSrv.URL, "ak-oai", nil),
	}
	s := newSupersetService(t, newGroupAwareMockRepo(accounts), &realDoUpstream{client: &http.Client{}})

	ids, _, _ := s.GetSupersetModels(context.Background(), supersetGroup())
	if len(ids) != 1 || ids[0] != "gpt-5" {
		t.Errorf("want only [gpt-5] (failed account drops out), got %v", ids)
	}
}

func TestGetSupersetModels_MappingReconciliationExposesKeys(t *testing.T) {
	// Account HAS a model_mapping → listing must expose mapping KEYS, not raw upstream ids.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":"raw-upstream-id"}]}`))
	}))
	defer srv.Close()

	accounts := []Account{
		apikeyAccount(1, PlatformAnthropic, srv.URL, "ak", map[string]any{"client-facing-name": "raw-upstream-id"}),
	}
	s := newSupersetService(t, newGroupAwareMockRepo(accounts), &realDoUpstream{client: &http.Client{}})

	ids, _, _ := s.GetSupersetModels(context.Background(), supersetGroup())
	got := make(map[string]bool)
	for _, id := range ids {
		got[id] = true
	}
	if !got["client-facing-name"] {
		t.Error("expected mapping key 'client-facing-name'")
	}
	if got["raw-upstream-id"] {
		t.Error("must NOT expose raw upstream id when mapping exists")
	}
}

func TestGetSupersetModels_FollowsPagination(t *testing.T) {
	// Anthropic /v1/models is cursor-paginated. A catalog larger than one page must be
	// fully listed — a missed page would make /v1/models disagree with the upstream
	// catalog. Page 1 returns has_more=true/last_id=model-b; page 2 has_more=false. We
	// assert all ids appear AND that page 2 was requested with after_id=model-b & limit=1000.
	var page2AfterID, page2Limit string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("after_id") {
		case "":
			w.Write([]byte(`{"data":[{"id":"model-a"},{"id":"model-b"}],"has_more":true,"last_id":"model-b"}`))
		case "model-b":
			page2AfterID = r.URL.Query().Get("after_id")
			page2Limit = r.URL.Query().Get("limit")
			w.Write([]byte(`{"data":[{"id":"model-c"}],"has_more":false,"last_id":"model-c"}`))
		default:
			t.Errorf("unexpected after_id=%q", r.URL.Query().Get("after_id"))
			w.Write([]byte(`{"data":[]}`))
		}
	}))
	defer srv.Close()

	accounts := []Account{apikeyAccount(1, PlatformAnthropic, srv.URL, "ak", nil)}
	s := newSupersetService(t, newGroupAwareMockRepo(accounts), &realDoUpstream{client: &http.Client{}})

	ids, _, _ := s.GetSupersetModels(context.Background(), supersetGroup())
	got := make(map[string]bool)
	for _, id := range ids {
		got[id] = true
	}
	for _, want := range []string{"model-a", "model-b", "model-c"} {
		if !got[want] {
			t.Errorf("paginated id %q missing from %v", want, ids)
		}
	}
	if page2AfterID != "model-b" {
		t.Errorf("page 2 after_id=%q want model-b", page2AfterID)
	}
	if page2Limit != "1000" {
		t.Errorf("page 2 limit=%q want 1000", page2Limit)
	}
}

func TestGetSupersetModels_AllEmptyReturnsNil(t *testing.T) {
	// No mapping + upstream fails → account contributes nothing → (nil, nil).
	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer failSrv.Close()

	accounts := []Account{apikeyAccount(1, PlatformAnthropic, failSrv.URL, "ak", nil)}
	s := newSupersetService(t, newGroupAwareMockRepo(accounts), &realDoUpstream{client: &http.Client{}})

	ids, origins, metas := s.GetSupersetModels(context.Background(), supersetGroup())
	if ids != nil || origins != nil || metas != nil {
		t.Errorf("want nil,nil,nil got ids=%v origins=%v metas=%v", ids, origins, metas)
	}
}

func TestGetSupersetModels_CachedSecondCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":"claude-opus-4-8"}]}`))
	}))
	defer srv.Close()

	accounts := []Account{apikeyAccount(1, PlatformAnthropic, srv.URL, "ak", nil)}
	up := &realDoUpstream{client: &http.Client{}}
	s := newSupersetService(t, newGroupAwareMockRepo(accounts), up)

	_, _, _ = s.GetSupersetModels(context.Background(), supersetGroup())
	callsAfterFirst := up.doCalls.Load()
	if callsAfterFirst == 0 {
		t.Fatal("expected at least one upstream Do on first call")
	}
	_, _, _ = s.GetSupersetModels(context.Background(), supersetGroup())
	if up.doCalls.Load() != callsAfterFirst {
		t.Errorf("second call should hit cache, doCalls went %d→%d", callsAfterFirst, up.doCalls.Load())
	}
}

// TestGetSupersetModels_ConcurrentMissesCollapse is the stampede guard: when N requests
// miss the cache simultaneously, singleflight must collapse them so the upstream is hit
// exactly once per account, not N times. Without singleflight this is a self-DDoS on
// every cache expiry.
func TestGetSupersetModels_ConcurrentMissesCollapse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":"claude-opus-4-8"}]}`))
	}))
	defer srv.Close()

	accounts := []Account{apikeyAccount(1, PlatformAnthropic, srv.URL, "ak", nil)}
	up := &realDoUpstream{client: &http.Client{}}
	s := newSupersetService(t, newGroupAwareMockRepo(accounts), up)

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			ids, _, _ := s.GetSupersetModels(context.Background(), supersetGroup())
			if len(ids) != 1 || ids[0] != "claude-opus-4-8" {
				t.Errorf("got ids=%v", ids)
			}
		}()
	}
	wg.Wait()

	// 50 concurrent misses on one account must produce exactly ONE upstream call (one
	// flight wins; the rest wait on it and read the result).
	if got := up.doCalls.Load(); got != 1 {
		t.Errorf("upstream hit %d times for %d concurrent misses; want 1 (singleflight collapse)", got, n)
	}
}
