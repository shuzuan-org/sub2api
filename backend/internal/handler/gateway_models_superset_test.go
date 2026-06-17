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
