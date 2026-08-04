package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// accountWithBilling builds an account whose every request bills as billingModel,
// optionally with a manually confirmed price override (U per MTok).
func accountWithBilling(billingModel string, inPerMTok, outPerMTok float64) service.Account {
	extra := map[string]any{
		"billing_model_mapping": map[string]any{"*": billingModel},
	}
	if inPerMTok > 0 || outPerMTok > 0 {
		extra["model_pricing"] = map[string]any{
			billingModel: map[string]any{
				"input_cost_per_token":  inPerMTok / 1e6,
				"output_cost_per_token": outPerMTok / 1e6,
			},
		}
	}
	return service.Account{Extra: extra}
}

// One exposed model backed by accounts that bill under different internal names must
// produce ONE price, and that price must be the highest a request can actually cost —
// a public page may never quote less than what will be charged.
func TestMaxPriceAcrossAccounts_PicksHighestChargedPrice(t *testing.T) {
	h := &PricingHandler{billingService: service.NewBillingService(&config.Config{}, nil)}

	accounts := []service.Account{
		accountWithBilling("z-ai/glm-5.2", 24.5, 77),
		accountWithBilling("zai/glm-5.2", 98, 308),
		accountWithBilling("glm-5.2", 98, 308),
	}

	price, ok := h.maxPriceAcrossAccounts(accounts, "GLM-5.2")
	if !ok {
		t.Fatal("expected a price for GLM-5.2")
	}
	if got := price.InputPerMTok; got != 98 {
		t.Errorf("input price = %v U/MTok, want 98 (highest charged)", got)
	}
	if got := price.OutputPerMTok; got != 308 {
		t.Errorf("output price = %v U/MTok, want 308 (highest charged)", got)
	}
}

// Cache prices follow the same rule as input/output: highest charged wins, and they
// are reported per component so a model priced only for cache reads still shows one.
func TestMaxPriceAcrossAccounts_CachePrices(t *testing.T) {
	h := &PricingHandler{billingService: service.NewBillingService(&config.Config{}, nil)}

	cheap := accountWithBilling("cache-test-model", 3, 15)
	cheap.Extra["model_pricing"].(map[string]any)["cache-test-model"].(map[string]any)["cache_creation_input_token_cost"] = 3.75 / 1e6
	cheap.Extra["model_pricing"].(map[string]any)["cache-test-model"].(map[string]any)["cache_read_input_token_cost"] = 0.3 / 1e6

	pricey := accountWithBilling("cache-test-model-2", 3, 15)
	pricey.Extra["model_pricing"].(map[string]any)["cache-test-model-2"].(map[string]any)["cache_creation_input_token_cost"] = 7.5 / 1e6
	pricey.Extra["model_pricing"].(map[string]any)["cache-test-model-2"].(map[string]any)["cache_read_input_token_cost"] = 0.1 / 1e6

	price, ok := h.maxPriceAcrossAccounts([]service.Account{cheap, pricey}, "cache-test-model")
	if !ok {
		t.Fatal("expected a price")
	}
	if got := price.CacheCreatePerMTok; got != 7.5 {
		t.Errorf("cache create = %v U/MTok, want 7.5 (highest charged)", got)
	}
	if got := price.CacheReadPerMTok; got != 0.3 {
		t.Errorf("cache read = %v U/MTok, want 0.3 (highest charged)", got)
	}
}

// A model with no cache pricing must report zero, not inherit the input price —
// the page renders that as "no cache price", not as a charge.
func TestMaxPriceAcrossAccounts_NoCachePriceIsZero(t *testing.T) {
	h := &PricingHandler{billingService: service.NewBillingService(&config.Config{}, nil)}

	accounts := []service.Account{accountWithBilling("no-cache-model-xyz", 24.5, 77)}
	price, ok := h.maxPriceAcrossAccounts(accounts, "no-cache-model-xyz")
	if !ok {
		t.Fatal("expected a price")
	}
	if price.CacheCreatePerMTok != 0 || price.CacheReadPerMTok != 0 {
		t.Errorf("cache prices = %v/%v, want 0/0", price.CacheCreatePerMTok, price.CacheReadPerMTok)
	}
}

// The 1h cache-write rate is only surfaced when it really is a separate, higher rate;
// otherwise the base column already tells the whole story.
func TestCacheCreate1hPricePerToken(t *testing.T) {
	tests := []struct {
		name    string
		pricing service.ModelPricing
		want    float64
	}{
		{"no breakdown", service.ModelPricing{CacheCreationPricePerToken: 3.75, CacheCreation1hPrice: 6}, 0},
		{"breakdown, 1h higher", service.ModelPricing{SupportsCacheBreakdown: true, CacheCreation5mPrice: 3.75, CacheCreation1hPrice: 6}, 6},
		{"breakdown, 1h not higher", service.ModelPricing{SupportsCacheBreakdown: true, CacheCreation5mPrice: 3.75, CacheCreation1hPrice: 3.75}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cacheCreate1hPricePerToken(&tt.pricing); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// With a 5m/1h breakdown, an unqualified cache write bills at the 5m rate — that is
// what the base column must show.
func TestCacheCreatePricePerToken_UsesFiveMinuteRate(t *testing.T) {
	p := service.ModelPricing{
		SupportsCacheBreakdown:     true,
		CacheCreationPricePerToken: 3.75,
		CacheCreation5mPrice:       3.75,
		CacheCreation1hPrice:       6,
	}
	if got := cacheCreatePricePerToken(&p); got != 3.75 {
		t.Errorf("got %v, want 3.75 (5m rate)", got)
	}
}

// A model no account can price must be dropped, not shown at zero.
func TestMaxPriceAcrossAccounts_NoPricingIsNotOK(t *testing.T) {
	h := &PricingHandler{billingService: service.NewBillingService(&config.Config{}, nil)}

	accounts := []service.Account{accountWithBilling("model-nobody-prices-xyz", 0, 0)}
	if _, ok := h.maxPriceAcrossAccounts(accounts, "model-nobody-prices-xyz"); ok {
		t.Error("expected ok=false for a model with no resolvable pricing")
	}
}

func TestDedupePreservingOrder(t *testing.T) {
	got := dedupePreservingOrder([]string{"GLM-5.2", " ", "glm-5.2", "Claude-Opus", "GLM-5.2"})
	want := []string{"GLM-5.2", "Claude-Opus"}

	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// billing_model and billing_model_mapping are both read, across all accounts.
func TestCollectBillingModelNames(t *testing.T) {
	accounts := []service.Account{
		{Extra: map[string]any{"billing_model": "glm-5.2"}},
		accountWithBilling("z-ai/glm-5.2", 0, 0),
		{Extra: nil},
	}

	got := collectBillingModelNames(accounts)
	if len(got) != 2 {
		t.Fatalf("got %v, want 2 names", got)
	}
}

// Aliases that differ only by pricing-table namespace are the same model: one row.
func TestStripPricingNamespaces_CollapsesVendorAliases(t *testing.T) {
	got := dedupePreservingOrder(stripPricingNamespaces([]string{
		"z-ai/glm-5.2", "zai/glm-5.2", "glm-5.2",
	}))
	if len(got) != 1 || got[0] != "glm-5.2" {
		t.Fatalf("got %v, want [glm-5.2]", got)
	}
}

// A declared billing model wins over the upstream catalog: these upstreams are
// passthrough proxies advertising every model they can relay, which is not what the
// account is provisioned to serve.
func TestServedModelNames_PrefersDeclaredBillingModels(t *testing.T) {
	h := &PricingHandler{} // nil gatewayService: the declared path must not need it
	accounts := []service.Account{
		accountWithBilling("z-ai/glm-5.2", 0, 0),
		accountWithBilling("glm-5.2", 0, 0),
	}

	got := h.servedModelNames(t.Context(), service.Group{ID: 1}, accounts)
	if len(got) != 1 || got[0] != "glm-5.2" {
		t.Fatalf("got %v, want [glm-5.2]", got)
	}
}
