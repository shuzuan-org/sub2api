//go:build unit

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// anthropicGroupCtx wires an API key bound to an anthropic group into the gin context,
// the way apiKeyAuth middleware would.
func anthropicGroupCtx(c *gin.Context) {
	groupID := int64(7)
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{
		ID:      100,
		GroupID: &groupID,
		Group: &service.Group{
			ID:       groupID,
			Name:     "Anthropic Group",
			Platform: service.PlatformAnthropic,
			Status:   service.StatusActive,
		},
	})
}

// mappedAnthropicAccount has a model_mapping so the superset builder exposes its keys
// WITHOUT touching httpUpstream (which is nil in the minimal gateway service).
func mappedAnthropicAccount() service.Account {
	return service.Account{
		ID:          1,
		Platform:    service.PlatformAnthropic,
		Type:        service.AccountTypeAPIKey,
		Status:      service.StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"api_key":       "ak",
			"model_mapping": map[string]any{"claude-opus-4-8": "gpt-5.5"},
		},
	}
}

func TestModelsHandler_DualProtocolSuperset(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gw := newMinimalGatewayService(&stubAccountRepoForHandler{accounts: []service.Account{mappedAnthropicAccount()}})
	h := &GatewayHandler{gatewayService: gw}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.0") // claude branch → mapping keys
	anthropicGroupCtx(c)

	h.Models(c)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "list", resp["object"])

	data, ok := resp["data"].([]any)
	require.True(t, ok, "data must be a list")
	require.Len(t, data, 1)
	obj := data[0].(map[string]any)
	// OpenAI keys AND Anthropic keys coexist on the same object.
	require.Equal(t, "claude-opus-4-8", obj["id"])
	require.Equal(t, "model", obj["object"]) // OpenAI
	require.Equal(t, "model", obj["type"])   // Anthropic
	require.Equal(t, "claude-opus-4-8", obj["display_name"])
	require.Contains(t, obj, "owned_by")
	// Anthropic-origin claude model carries the capabilities tree.
	require.Contains(t, obj, "capabilities")
}

func TestModelsHandler_SingleModelLookupAndRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gw := newMinimalGatewayService(&stubAccountRepoForHandler{accounts: []service.Account{mappedAnthropicAccount()}})
	h := &GatewayHandler{gatewayService: gw}

	// Known model, requested with a [1m] variant spelling → id round-trips, derivation
	// uses the canonical key.
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models/claude-opus-4-8[1m]", nil)
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.0") // claude branch → mapping key visible
	c.Params = gin.Params{{Key: "id", Value: "claude-opus-4-8[1m]"}}
	anthropicGroupCtx(c)

	h.Model(c)

	require.Equal(t, http.StatusOK, w.Code)
	var obj map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &obj))
	require.Equal(t, "claude-opus-4-8[1m]", obj["id"])       // client spelling round-trips
	require.Equal(t, "claude-opus-4-8", obj["display_name"]) // canonical
	require.Contains(t, obj, "capabilities")
}

func TestModelsHandler_SingleModelNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gw := newMinimalGatewayService(&stubAccountRepoForHandler{accounts: []service.Account{mappedAnthropicAccount()}})
	h := &GatewayHandler{gatewayService: gw}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models/gpt-4", nil)
	c.Params = gin.Params{{Key: "id", Value: "gpt-4"}}
	anthropicGroupCtx(c)

	h.Model(c)

	require.Equal(t, http.StatusNotFound, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "error", resp["type"])
	errObj := resp["error"].(map[string]any)
	require.Equal(t, "not_found_error", errObj["type"])
}

// deepSeekGroupCtx wires an API key bound to a DeepSeek group into the gin context.
func deepSeekGroupCtx(c *gin.Context) {
	groupID := int64(9)
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{
		ID:      100,
		GroupID: &groupID,
		Group: &service.Group{
			ID:       groupID,
			Name:     "DeepSeek Group",
			Platform: service.PlatformDeepSeek,
			Status:   service.StatusActive,
		},
	})
}

// TestModelsHandler_DeepSeekSingleLookupMatchesListing pins the list/single-lookup
// existence invariant for a legacy (non-superset) platform: a DeepSeek group lists
// deepseek-* models from a default catalog, so the single-lookup must resolve the same
// ids (regression guard for the bug where GET /v1/models/{id} 404'd a model the listing
// just advertised, because the superset set is empty for DeepSeek groups).
func TestModelsHandler_DeepSeekSingleLookupMatchesListing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gw := newMinimalGatewayService(&stubAccountRepoForHandler{})
	h := &GatewayHandler{gatewayService: gw}

	// A model the DeepSeek listing advertises → single-lookup must be 200.
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models/deepseek-chat", nil)
	c.Params = gin.Params{{Key: "id", Value: "deepseek-chat"}}
	deepSeekGroupCtx(c)

	h.Model(c)

	require.Equal(t, http.StatusOK, w.Code)
	var obj map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &obj))
	require.Equal(t, "deepseek-chat", obj["id"])
	require.Equal(t, "model", obj["type"])

	// An unknown id under the same platform → 404 (not a panic, not a superset fallthrough).
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "/v1/models/no-such-model", nil)
	c2.Params = gin.Params{{Key: "id", Value: "no-such-model"}}
	deepSeekGroupCtx(c2)

	h.Model(c2)

	require.Equal(t, http.StatusNotFound, w2.Code)
}

func TestModelsHandler_DeepSeekLegacyShapePreserved(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// DeepSeek group must keep its legacy single-protocol shape (no superset, no
	// capabilities) — regression guard for the reordering.
	gw := newMinimalGatewayService(&stubAccountRepoForHandler{})
	h := &GatewayHandler{gatewayService: gw}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	groupID := int64(9)
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{
		ID:      101,
		GroupID: &groupID,
		Group:   &service.Group{ID: groupID, Platform: service.PlatformDeepSeek, Status: service.StatusActive},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "list", resp["object"])
	data := resp["data"].([]any)
	require.NotEmpty(t, data)
	// Legacy claude.Model shape: no OpenAI "object" key, no capabilities.
	first := data[0].(map[string]any)
	require.NotContains(t, first, "capabilities")
	require.NotContains(t, first, "owned_by")
}

func ptrF(v float64) *float64 { return &v }

// TestModelsHandler_CapacitySubscription: a caller with an active subscription gets the
// bottleneck-remaining USD in the envelope.
func TestModelsHandler_CapacitySubscription(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gw := newMinimalGatewayService(&stubAccountRepoForHandler{accounts: []service.Account{mappedAnthropicAccount()}})
	h := &GatewayHandler{gatewayService: gw}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	anthropicGroupCtx(c)
	// Monthly limit $100, used $30 → remaining $70 (only window configured → bottleneck).
	c.Set(string(middleware.ContextKeyMergedSubscription), &service.MergedSubscriptionState{
		FIFOQueue: []service.UserSubscription{{
			ID:              1,
			ExpiresAt:       time.Now().Add(24 * time.Hour),
			MonthlyUsageUSD: 30,
			Plan:            &service.SubscriptionPlan{MonthlyLimitUSD: ptrF(100)},
		}},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "USD", resp["unit"])
	require.InDelta(t, 70.0, resp["remaining"].(float64), 1e-9)
}

// TestModelsHandler_CapacityWalletBalance: no subscription but an auth subject with a
// wallet balance → remaining = balance.
func TestModelsHandler_CapacityWalletBalance(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gw := newMinimalGatewayService(&stubAccountRepoForHandler{accounts: []service.Account{mappedAnthropicAccount()}})
	userRepo := newStubUserRepoForHandler()
	userRepo.users[42] = &service.User{ID: 42, Balance: 12.5}
	h := &GatewayHandler{gatewayService: gw, userService: service.NewUserService(userRepo, nil, nil)}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	anthropicGroupCtx(c)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})

	h.Models(c)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "USD", resp["unit"])
	require.InDelta(t, 12.5, resp["remaining"].(float64), 1e-9)
}

// TestModelsHandler_CapacityAbsentOmitsFields: no subscription and no resolvable user →
// envelope omits remaining/unit entirely (omitempty), not zero values.
func TestModelsHandler_CapacityAbsentOmitsFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gw := newMinimalGatewayService(&stubAccountRepoForHandler{accounts: []service.Account{mappedAnthropicAccount()}})
	h := &GatewayHandler{gatewayService: gw}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	anthropicGroupCtx(c) // no subscription, no ContextKeyUser

	h.Models(c)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotContains(t, resp, "remaining")
	require.NotContains(t, resp, "unit")
}

// openaiGroupCtx wires an API key bound to an OpenAI-platform group.
func openaiGroupCtx(c *gin.Context) {
	groupID := int64(8)
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{
		ID:      100,
		GroupID: &groupID,
		Group: &service.Group{
			ID:       groupID,
			Name:     "OpenAI Group",
			Platform: service.PlatformOpenAI,
			Status:   service.StatusActive,
		},
	})
}

// mappedOpenAIAccount exposes a gpt-* key via model_mapping (no httpUpstream needed).
func mappedOpenAIAccount() service.Account {
	return service.Account{
		ID:          2,
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeAPIKey,
		Status:      service.StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"api_key":       "ak",
			"model_mapping": map[string]any{"gpt-5.5": "gpt-5.5"},
		},
	}
}

// TestModelsHandler_ClaudeCodeFilterAnthropicOnly pins the platform gate on the
// client-source filter: a claude-cli client hitting an ANTHROPIC group sees the
// claude-* mapping key, but the SAME client hitting an OPENAI group must still see the
// gpt-* name — the filter must not strip it and fall back to the default gpt list.
// group38Account mirrors production group 38: one anthropic upstream (MiniMax-M3) fronted
// by five mapping aliases (3 claude + 1 gpt + the real name). Used to verify all three
// User-Agent branches off a single backing model.
func group38Account() service.Account {
	return service.Account{
		ID:          3,
		Platform:    service.PlatformAnthropic,
		Type:        service.AccountTypeAPIKey,
		Status:      service.StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"api_key": "ak",
			"model_mapping": map[string]any{
				"claude-opus-4-8":   "MiniMax-M3",
				"claude-sonnet-4-6": "MiniMax-M3",
				"claude-haiku-4-5":  "MiniMax-M3",
				"gpt-5.5":           "MiniMax-M3",
				"MiniMax-M3":        "MiniMax-M3",
			},
		},
	}
}

func modelsForUA(t *testing.T, ua string) []string {
	t.Helper()
	gw := newMinimalGatewayService(&stubAccountRepoForHandler{accounts: []service.Account{group38Account()}})
	h := &GatewayHandler{gatewayService: gw}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	if ua != "" {
		c.Request.Header.Set("User-Agent", ua)
	}
	anthropicGroupCtx(c)
	h.Models(c)
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return modelIDs(resp)
}

// TestModelsHandler_ThreeWayClientBranching pins the by-User-Agent name adaptation off a
// single real model (MiniMax-M3) with five aliases: Claude Code sees only the claude-*
// keys, Codex sees only the gpt-* key, and any other client sees the deduped real name.
func TestModelsHandler_ThreeWayClientBranching(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Claude Code → only the three claude-* aliases.
	cc := modelsForUA(t, "claude-cli/2.1.0")
	require.ElementsMatch(t, []string{"claude-opus-4-8", "claude-sonnet-4-6", "claude-haiku-4-5"}, cc)

	// Codex → only the gpt-* alias.
	codex := modelsForUA(t, "codex_cli_rs/0.80.0 (Mac OS; arm64)")
	require.Equal(t, []string{"gpt-5.5"}, codex)

	// Codex Desktop UA form also matches.
	codexDesktop := modelsForUA(t, "Codex Desktop/0.140.0-alpha.19 (Mac OS 26.5.1; arm64)")
	require.Equal(t, []string{"gpt-5.5"}, codexDesktop)

	// Anything else → the single deduped real upstream name, no aliases.
	other := modelsForUA(t, "curl/8.0")
	require.Equal(t, []string{"MiniMax-M3"}, other)

	// Empty UA also falls to the "other" branch.
	noUA := modelsForUA(t, "")
	require.Equal(t, []string{"MiniMax-M3"}, noUA)
}

// TestModelsHandler_ClaudeCodeNoMatchEmpty: a claude-cli client hitting a group with no
// claude alias gets an empty list (no fabricated defaults), per the "no match → empty" rule.
func TestModelsHandler_ClaudeCodeNoMatchEmpty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gw := newMinimalGatewayService(&stubAccountRepoForHandler{accounts: []service.Account{mappedOpenAIAccount()}})
	h := &GatewayHandler{gatewayService: gw}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.0")
	openaiGroupCtx(c)
	h.Models(c)
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Empty(t, modelIDs(resp), "no claude alias → empty list, not default models")
}

func modelIDs(resp map[string]any) []string {
	data, _ := resp["data"].([]any)
	out := make([]string, 0, len(data))
	for _, d := range data {
		if m, ok := d.(map[string]any); ok {
			if id, ok := m["id"].(string); ok {
				out = append(out, id)
			}
		}
	}
	return out
}
