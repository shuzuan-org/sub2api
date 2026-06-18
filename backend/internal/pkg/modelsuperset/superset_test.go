package modelsuperset

import (
	"encoding/json"
	"testing"
)

func TestModelMatchesBase_BoundaryAware(t *testing.T) {
	cases := []struct {
		normalized, base string
		want             bool
	}{
		{"claude-opus-4-8", "opus-4-8", true},
		{"claude-opus-4-8-thinking", "opus-4-8", true},
		{"claude-opus-4-80", "opus-4-8", false}, // not a delimited token
		{"claude-sonnet-4-6", "opus", false},
		{"claude-opus-4-8", "opus", true},
		{"gpt-5", "opus", false},
	}
	for _, c := range cases {
		if got := modelMatchesBase(c.normalized, c.base); got != c.want {
			t.Errorf("modelMatchesBase(%q,%q)=%v want %v", c.normalized, c.base, got, c.want)
		}
	}
}

func TestNormalizeModelName(t *testing.T) {
	cases := map[string]string{
		"claude.sonnet.4.6":     "claude-sonnet-4-6",
		"claude-sonnet-4-6[1m]": "claude-sonnet-4-6",
		"claude-opus-4-8":       "claude-opus-4-8",
		"gpt-5":                 "gpt-5",
	}
	for in, want := range cases {
		if got := NormalizeModelName(in); got != want {
			t.Errorf("NormalizeModelName(%q)=%q want %q", in, got, want)
		}
	}
}

func TestModelContextWindow(t *testing.T) {
	cases := map[string]int{
		"claude-opus-4-8":   1000000,
		"claude-sonnet-4-6": 1000000, // sonnet-4 base
		"claude-opus-4-7":   1000000,
		"claude-haiku-4-5":  200000,
		"claude-opus-4-5":   200000, // not in oneMillionBases
	}
	for in, want := range cases {
		if got := modelContextWindow(in); got != want {
			t.Errorf("modelContextWindow(%q)=%d want %d", in, got, want)
		}
	}
}

func TestBuildModel_AnthropicClaude_FullTree(t *testing.T) {
	m := BuildModel("claude-opus-4-8", OriginAnthropic, ModelMeta{})
	if m.Capabilities == nil {
		t.Fatal("expected capabilities for anthropic claude model")
	}
	if m.MaxInputTokens != 1000000 {
		t.Errorf("max_input_tokens=%d want 1000000", m.MaxInputTokens)
	}
	effort := m.Capabilities["effort"].(map[string]any)
	if effort["max"].(map[string]any)["supported"] != true {
		t.Error("opus should have effort.max supported=true")
	}
	if m.Object != "model" || m.OwnedBy != "sub2api" || m.Type != "model" {
		t.Errorf("unexpected protocol-neutral fields: %+v", m)
	}
}

func TestBuildModel_Haiku_EffortMaxFalse(t *testing.T) {
	m := BuildModel("claude-haiku-4-5", OriginAnthropic, ModelMeta{})
	if m.MaxInputTokens != 200000 {
		t.Errorf("haiku max_input_tokens=%d want 200000", m.MaxInputTokens)
	}
	effort := m.Capabilities["effort"].(map[string]any)
	if effort["max"].(map[string]any)["supported"] != false {
		t.Error("haiku should have effort.max supported=false")
	}
}

func TestBuildModel_OpenAIOrigin_NoCapabilities(t *testing.T) {
	m := BuildModel("gpt-5", OriginOpenAI, ModelMeta{})
	if m.Capabilities != nil {
		t.Error("openai-origin model must NOT carry a Claude capabilities tree")
	}
	if m.MaxInputTokens != 0 {
		t.Errorf("openai-origin max_input_tokens=%d want 0 (unknown)", m.MaxInputTokens)
	}
	// Protocol-neutral keys still present so Codex clients work.
	if m.ID != "gpt-5" || m.Object != "model" || m.OwnedBy != "sub2api" {
		t.Errorf("openai-origin missing neutral keys: %+v", m)
	}
	// And capabilities is omitted from JSON (omitempty).
	b, _ := json.Marshal(m)
	var raw map[string]any
	_ = json.Unmarshal(b, &raw)
	if _, ok := raw["capabilities"]; ok {
		t.Error("capabilities should be omitted from JSON for openai-origin")
	}
}

func TestBuildModel_AnthropicOrigin_NonClaude_HasCapabilities(t *testing.T) {
	// sub2api adapts every anthropic-platform upstream to the Claude protocol, so an
	// anthropic-origin model emits the capability tree regardless of its display name.
	m := BuildModel("minimax-m2.7", OriginAnthropic, ModelMeta{})
	if m.Capabilities == nil {
		t.Error("anthropic-origin model must carry the Claude capability tree (protocol adaptation)")
	}
	// max_input_tokens stays 0 here: no real upstream meta, and the family guess is only
	// for Claude-named ids (a minimax name has no honest guess).
	if m.MaxInputTokens != 0 {
		t.Errorf("non-claude no-meta max_input_tokens=%d want 0", m.MaxInputTokens)
	}
}

func TestBuildList_Envelope(t *testing.T) {
	origins := map[string]Origin{
		"claude-opus-4-8": OriginAnthropic,
		"gpt-5":           OriginOpenAI,
	}
	list := BuildList([]string{"gpt-5", "claude-opus-4-8"}, origins, nil)
	if list.Object != "list" {
		t.Errorf("object=%q want list", list.Object)
	}
	if len(list.Data) != 2 {
		t.Fatalf("data len=%d want 2", len(list.Data))
	}
	// Sorted: claude-opus-4-8 < gpt-5. first_id/last_id are *string now.
	if list.FirstID == nil || list.LastID == nil {
		t.Fatalf("first_id/last_id should be non-nil for a non-empty list")
	}
	if *list.FirstID != "claude-opus-4-8" || *list.LastID != "gpt-5" {
		t.Errorf("first=%q last=%q want claude-opus-4-8/gpt-5", *list.FirstID, *list.LastID)
	}
	if list.HasMore {
		t.Error("has_more should be false")
	}
}

func TestBuildList_EmptyIsNull(t *testing.T) {
	// An empty listing must serialize first_id/last_id as JSON null (Anthropic's
	// convention), not "". *string + the len(data)>0 guard gives us that.
	list := BuildList(nil, nil, nil)
	if list.FirstID != nil || list.LastID != nil {
		t.Fatalf("empty list first_id/last_id should be nil, got %v/%v", list.FirstID, list.LastID)
	}
	b, err := json.Marshal(list)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(raw["first_id"]) != "null" || string(raw["last_id"]) != "null" {
		t.Errorf("first_id=%s last_id=%s want null/null", raw["first_id"], raw["last_id"])
	}
}

func TestMatchModelID(t *testing.T) {
	ids := []string{"claude-opus-4-8", "gpt-5"}
	origins := map[string]Origin{"claude-opus-4-8": OriginAnthropic, "gpt-5": OriginOpenAI}

	// exact
	if key, o, ok := MatchModelID("claude-opus-4-8", ids, origins); !ok || key != "claude-opus-4-8" || o != OriginAnthropic {
		t.Errorf("exact match failed: %q %v %v", key, o, ok)
	}
	// [1m] variant normalizes
	if key, _, ok := MatchModelID("claude-opus-4-8[1m]", ids, origins); !ok || key != "claude-opus-4-8" {
		t.Errorf("[1m] variant match failed: %q %v", key, ok)
	}
	// date suffix strips
	if key, _, ok := MatchModelID("claude-opus-4-8-20250101", ids, origins); !ok || key != "claude-opus-4-8" {
		t.Errorf("date-suffix match failed: %q %v", key, ok)
	}
	// unknown
	if _, _, ok := MatchModelID("gpt-4", ids, origins); ok {
		t.Error("gpt-4 should not match")
	}
	// empty
	if _, _, ok := MatchModelID("", ids, origins); ok {
		t.Error("empty id should not match")
	}
}

func TestBuildModel_UpstreamOverridesGuess(t *testing.T) {
	// Claude-family id backed by a smaller real window (e.g. minimax-m2.7=196608):
	// the real upstream value must win over the 1M family guess.
	m := BuildModel("claude-opus-4-8", OriginAnthropic, ModelMeta{MaxInputTokens: 196608})
	if m.MaxInputTokens != 196608 {
		t.Errorf("max_input_tokens=%d want 196608 (real upstream over guess)", m.MaxInputTokens)
	}
	if m.Capabilities == nil {
		t.Error("claude id should still emit capabilities (decoupled from window)")
	}
}

func TestBuildModel_NonClaudeWithUpstreamMeta(t *testing.T) {
	// Non-claude id with a real upstream window: surface the number, but NEVER a Claude
	// capability tree (decoupling).
	m := BuildModel("minimax-m2.7", OriginOpenAI, ModelMeta{MaxInputTokens: 131072})
	if m.MaxInputTokens != 131072 {
		t.Errorf("max_input_tokens=%d want 131072", m.MaxInputTokens)
	}
	if m.Capabilities != nil {
		t.Error("non-claude id must not carry capabilities even with real meta")
	}
}

func TestBuildModel_ClaudeNoMetaFallback(t *testing.T) {
	// No upstream meta → Claude family falls back to the family guess (no regression).
	if m := BuildModel("claude-opus-4-8", OriginAnthropic, ModelMeta{}); m.MaxInputTokens != 1000000 {
		t.Errorf("opus fallback=%d want 1000000", m.MaxInputTokens)
	}
	// Non-claude with no meta stays 0 (honest unknown).
	if m := BuildModel("minimax-m2.7", OriginOpenAI, ModelMeta{}); m.MaxInputTokens != 0 {
		t.Errorf("non-claude no-meta=%d want 0", m.MaxInputTokens)
	}
}

func TestBuildModel_OutputCapPassthrough(t *testing.T) {
	m := BuildModel("minimax-m2.7", OriginOpenAI, ModelMeta{MaxInputTokens: 131072, MaxOutputTokens: 8192})
	if m.MaxTokens != 8192 {
		t.Errorf("max_tokens=%d want 8192 (real output cap)", m.MaxTokens)
	}
}

func TestFilterForClaudeCode(t *testing.T) {
	origins := map[string]Origin{
		"claude-opus-4-8":   OriginAnthropic, // mapping key — kept
		"claude-sonnet-4-6": OriginAnthropic, // kept
		"minimax-m2.7":      OriginAnthropic, // raw anthropic upstream name — dropped
		"glm-5.2":           OriginAnthropic, // raw — dropped
		"gpt-5":             OriginOpenAI,    // openai — dropped
		"claude-haiku-4-5":  OriginOpenAI,    // claude token but openai origin — dropped (defensive)
	}
	ids := []string{"claude-opus-4-8", "minimax-m2.7", "gpt-5", "claude-sonnet-4-6", "glm-5.2", "claude-haiku-4-5"}
	got := FilterForClaudeCode(ids, origins)

	want := map[string]bool{"claude-opus-4-8": true, "claude-sonnet-4-6": true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want only the two anthropic claude-family ids", got)
	}
	for _, id := range got {
		if !want[id] {
			t.Errorf("unexpected id kept: %q", id)
		}
	}
	// Order preservation: claude-opus-4-8 comes before claude-sonnet-4-6 in input.
	if got[0] != "claude-opus-4-8" || got[1] != "claude-sonnet-4-6" {
		t.Errorf("order not preserved: %v", got)
	}
}

func TestFilterForClaudeCode_AllRawIsEmpty(t *testing.T) {
	// A group of only no-mapping minimax accounts → nothing survives → empty, so the
	// handler falls back to the default claude model set (never a raw-name list).
	origins := map[string]Origin{"minimax-m2.7": OriginAnthropic, "minimax-m2.7-highspeed": OriginAnthropic}
	got := FilterForClaudeCode([]string{"minimax-m2.7", "minimax-m2.7-highspeed"}, origins)
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}
