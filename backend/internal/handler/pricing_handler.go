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
	UpdatedAt time.Time           `json:"updated_at"`
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
	groupRepo service.GroupRepository,
	accountRepo service.AccountRepository,
) *PricingHandler {
	return &PricingHandler{
		pricingService: pricingService,
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

	resp := val.(*PublicPricingResponse)
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

		billingModels := h.collectBillingModels(ctx, g.ID)

		var models []service.ModelPricingSummary
		if len(billingModels) > 0 {
			for model := range billingModels {
				pricing := h.pricingService.GetModelPricing(model)
				if pricing == nil {
					continue
				}
				models = append(models, service.ModelPricingSummary{
					Model:         model,
					InputPerMTok:  pricing.InputCostPerToken * 1e6,
					OutputPerMTok: pricing.OutputCostPerToken * 1e6,
				})
			}
		} else {
			models = h.pricingService.ListModelsByProvider(g.Platform)
		}

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

// collectBillingModels collects billing models from all accounts in a group.
// This is the same logic as APIKeyHandler.collectBillingModels.
func (h *PricingHandler) collectBillingModels(ctx context.Context, groupID int64) map[string]bool {
	accounts, err := h.accountRepo.ListByGroup(ctx, groupID)
	if err != nil || len(accounts) == 0 {
		return nil
	}

	result := make(map[string]bool)
	for _, acc := range accounts {
		if acc.Extra == nil {
			continue
		}
		if v, ok := acc.Extra["billing_model"]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				result[strings.ToLower(strings.TrimSpace(s))] = true
			}
		}
		if raw, ok := acc.Extra["billing_model_mapping"]; ok {
			if mapping, ok := raw.(map[string]any); ok {
				for _, v := range mapping {
					if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
						result[strings.ToLower(strings.TrimSpace(s))] = true
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
