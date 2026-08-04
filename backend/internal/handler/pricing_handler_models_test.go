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

	in, out, ok := h.maxPriceAcrossAccounts(accounts, "GLM-5.2")
	if !ok {
		t.Fatal("expected a price for GLM-5.2")
	}
	if got := in * 1e6; got != 98 {
		t.Errorf("input price = %v U/MTok, want 98 (highest charged)", got)
	}
	if got := out * 1e6; got != 308 {
		t.Errorf("output price = %v U/MTok, want 308 (highest charged)", got)
	}
}

// A model no account can price must be dropped, not shown at zero.
func TestMaxPriceAcrossAccounts_NoPricingIsNotOK(t *testing.T) {
	h := &PricingHandler{billingService: service.NewBillingService(&config.Config{}, nil)}

	accounts := []service.Account{accountWithBilling("model-nobody-prices-xyz", 0, 0)}
	if _, _, ok := h.maxPriceAcrossAccounts(accounts, "model-nobody-prices-xyz"); ok {
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
