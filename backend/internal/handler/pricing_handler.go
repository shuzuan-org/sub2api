package handler

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"golang.org/x/sync/singleflight"
)

// PublicPricingResponse is the cached response for the public pricing endpoint.
type PublicPricingResponse struct {
	Groups    []PublicGroupPricing `json:"groups"`
	UpdatedAt time.Time            `json:"updated_at"`
}

// PublicGroupPricing holds pricing data for a single public group.
type PublicGroupPricing struct {
	GroupName      string               `json:"group_name"`
	Platform       string               `json:"platform"`
	RateMultiplier float64              `json:"rate_multiplier"`
	Models         []PublicModelPricing `json:"models"`
}

// PublicModelPricing holds pricing data for a single model within a group.
type PublicModelPricing struct {
	Model                  string  `json:"model"`
	InputPerMTokU          float64 `json:"input_per_mtok_u"`
	OutputPerMTokU         float64 `json:"output_per_mtok_u"`
	OriginalInputPerMTokU  float64 `json:"original_input_per_mtok_u"`
	OriginalOutputPerMTokU float64 `json:"original_output_per_mtok_u"`
	DiscountPercent        float64 `json:"discount_percent"`
}

// PricingHandler serves the public model pricing endpoint.
type PricingHandler struct {
	pricingService *service.PricingService
	gatewayService *service.GatewayService
	billingService *service.BillingService
	groupRepo      service.GroupRepository
	accountRepo    service.AccountRepository

	mu        sync.RWMutex
	cached    *PublicPricingResponse
	cacheTime time.Time
	cacheTTL  time.Duration
	sf        singleflight.Group
}

// NewPricingHandler creates a new PricingHandler.
func NewPricingHandler(
	pricingService *service.PricingService,
	gatewayService *service.GatewayService,
	billingService *service.BillingService,
	groupRepo service.GroupRepository,
	accountRepo service.AccountRepository,
) *PricingHandler {
	return &PricingHandler{
		pricingService: pricingService,
		gatewayService: gatewayService,
		billingService: billingService,
		groupRepo:      groupRepo,
		accountRepo:    accountRepo,
		cacheTTL:       24 * time.Hour,
	}
}

// GetPublicModelPricing returns aggregated model pricing for all public groups.
// GET /api/v1/pricing/models (public, no auth required)
func (h *PricingHandler) GetPublicModelPricing(c *gin.Context) {
	// Check cache
	h.mu.RLock()
	if h.cached != nil && time.Since(h.cacheTime) < h.cacheTTL {
		resp := h.cached
		h.mu.RUnlock()
		response.Success(c, resp)
		return
	}
	h.mu.RUnlock()

	// Rebuild via singleflight (use independent context to avoid cancellation from first caller)
	val, err, _ := h.sf.Do("pricing", func() (any, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return h.buildPricingData(ctx)
	})
	if err != nil {
		response.InternalError(c, "failed to load pricing data")
		return
	}

	resp, ok := val.(*PublicPricingResponse)
	if !ok {
		response.InternalError(c, "failed to load pricing data")
		return
	}
	response.Success(c, resp)
}

// buildPricingData aggregates pricing from all public groups.
func (h *PricingHandler) buildPricingData(ctx context.Context) (*PublicPricingResponse, error) {
	groups, err := h.groupRepo.ListActive(ctx)
	if err != nil {
		return nil, err
	}

	// Platforms that don't support per-model pricing
	skipPlatforms := map[string]bool{
		"antigravity": true,
		"sora":        true,
	}

	var result []PublicGroupPricing

	for _, g := range groups {
		// 公开定价页只展示 public 分组：subscriber/private 对无订阅公众无意义。
		if g.Visibility != service.VisibilityPublic {
			continue
		}
		if skipPlatforms[strings.ToLower(g.Platform)] {
			continue
		}

		// 只展示这个分组真正能服务的模型：没有可调度账号就没有可展示的价格。
		// 绝不回退到"整个 provider 目录"——那会把分组根本路由不到的模型标上价格。
		accounts, err := h.accountRepo.ListByGroup(ctx, g.ID)
		if err != nil || len(accounts) == 0 {
			continue
		}

		models := h.collectGroupModels(ctx, g, accounts)
		if len(models) == 0 {
			continue
		}

		sort.Slice(models, func(i, j int) bool {
			return models[i].Model < models[j].Model
		})

		discountPercent := 0.0
		if g.RateMultiplier < 1.0 {
			discountPercent = (1.0 - g.RateMultiplier) * 100
		}

		publicModels := make([]PublicModelPricing, 0, len(models))
		for _, m := range models {
			publicModels = append(publicModels, PublicModelPricing{
				Model:                  m.Model,
				InputPerMTokU:          m.InputPerMTok * g.RateMultiplier,
				OutputPerMTokU:         m.OutputPerMTok * g.RateMultiplier,
				OriginalInputPerMTokU:  m.InputPerMTok,
				OriginalOutputPerMTokU: m.OutputPerMTok,
				DiscountPercent:        discountPercent,
			})
		}

		result = append(result, PublicGroupPricing{
			GroupName:      g.Name,
			Platform:       g.Platform,
			RateMultiplier: g.RateMultiplier,
			Models:         publicModels,
		})
	}

	resp := &PublicPricingResponse{
		Groups:    result,
		UpdatedAt: time.Now(),
	}

	// Update cache
	h.mu.Lock()
	h.cached = resp
	h.cacheTime = time.Now()
	h.mu.Unlock()

	return resp, nil
}

// collectGroupModels builds the public price rows for one group.
//
// The model NAMES come from the same place GET /v1/models serves them: the group's
// real upstream catalog fused with each account's model_mapping. That is what a client
// can actually call, so the pricing page and the gateway can no longer disagree — and
// it collapses the "same model listed once per internal billing key" duplication that
// reading billing_model names directly produced.
//
// The PRICE comes from the same resolver the billing path uses (account override →
// litellm → hardcoded fallback), so a displayed price is a charged price. When accounts
// in one group disagree on price, the highest is shown: a public page may never quote
// less than what a request can actually cost.
func (h *PricingHandler) collectGroupModels(ctx context.Context, g service.Group, accounts []service.Account) []service.ModelPricingSummary {
	names := h.exposedModelNames(ctx, g, accounts)
	if len(names) == 0 {
		return nil
	}

	models := make([]service.ModelPricingSummary, 0, len(names))
	for _, name := range names {
		in, out, ok := h.maxPriceAcrossAccounts(accounts, name)
		if !ok {
			continue
		}
		models = append(models, service.ModelPricingSummary{
			Model:         name,
			InputPerMTok:  in * 1e6,
			OutputPerMTok: out * 1e6,
		})
	}
	return models
}

// exposedModelNames returns the model ids clients can call against this group, deduped.
// Primary source is the /v1/models superset (anthropic+openai). Platforms that superset
// does not cover (deepseek, gemini, …) fall back to the accounts' configured billing
// model names so those groups keep a price listing instead of silently vanishing.
func (h *PricingHandler) exposedModelNames(ctx context.Context, g service.Group, accounts []service.Account) []string {
	groupID := g.ID
	if h.gatewayService != nil {
		if ids, _, _, _ := h.gatewayService.GetSupersetModels(ctx, &groupID); len(ids) > 0 {
			return dedupePreservingOrder(ids)
		}
	}
	return dedupePreservingOrder(collectBillingModelNames(accounts))
}

// maxPriceAcrossAccounts resolves what `model` costs on each account that can serve it
// and returns the highest input/output pair. ok=false when no account prices the model.
func (h *PricingHandler) maxPriceAcrossAccounts(accounts []service.Account, model string) (inputPerToken, outputPerToken float64, ok bool) {
	for i := range accounts {
		acc := &accounts[i]

		billingModel := acc.GetMappedModel(model)
		if override := acc.GetBillingModelOverride(billingModel); override != "" {
			billingModel = override
		}

		pricing := service.ResolveModelPricing(acc, billingModel, h.billingService)
		if pricing == nil {
			continue
		}
		if !ok || pricing.InputPricePerToken > inputPerToken {
			inputPerToken = pricing.InputPricePerToken
		}
		if !ok || pricing.OutputPricePerToken > outputPerToken {
			outputPerToken = pricing.OutputPricePerToken
		}
		ok = true
	}
	return inputPerToken, outputPerToken, ok
}

func dedupePreservingOrder(names []string) []string {
	seen := make(map[string]bool, len(names))
	result := make([]string, 0, len(names))
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, trimmed)
	}
	return result
}

// collectBillingModelNames collects billing model names configured on the accounts.
// This is the same logic as APIKeyHandler.collectBillingModels.
func collectBillingModelNames(accounts []service.Account) []string {
	var result []string
	for _, acc := range accounts {
		if acc.Extra == nil {
			continue
		}
		if v, ok := acc.Extra["billing_model"]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				result = append(result, strings.TrimSpace(s))
			}
		}
		if raw, ok := acc.Extra["billing_model_mapping"]; ok {
			if mapping, ok := raw.(map[string]any); ok {
				for _, v := range mapping {
					if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
						result = append(result, strings.TrimSpace(s))
					}
				}
			}
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
