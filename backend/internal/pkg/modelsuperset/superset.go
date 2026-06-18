// Package modelsuperset builds dual-protocol model objects for GET /v1/models and
// GET /v1/models/{id}.
//
// sub2api fronts both Claude Code (Anthropic Messages API) and Codex (OpenAI
// Responses API). A single /v1/models response can serve both at once by emitting a
// FIELD SUPERSET: every object carries OpenAI's standard keys (id/object/created/
// owned_by) AND Anthropic's standard keys (type/display_name/created_at/capabilities/
// max_*). The two key sets do not collide — even created (int, OpenAI) vs created_at
// (RFC3339 string, Anthropic) are distinct keys — so each client reads the fields it
// recognizes and ignores the rest. No content negotiation is needed.
//
// HONESTY: the capabilities tree and max_input_tokens are Claude-family-shaped. They
// are emitted ONLY for Anthropic-origin Claude models, where they reflect what the
// Anthropic family conventionally supports. For an OpenAI-origin model (e.g. gpt-5) a
// Claude capabilities tree would be a lie, so it is omitted entirely and
// max_input_tokens stays 0 (unknown). We never fabricate capabilities for a backend we
// did not derive them from.
//
// The derivation logic (boundary-aware family matching, normalize, context window) is
// ported from cc2codex's models.go/config.go.
package modelsuperset

import (
	"sort"
	"strings"
)

// Origin is the account platform a model id was sourced from. It gates the
// Claude-family capability derivation (see BuildModel).
type Origin int

const (
	OriginAnthropic Origin = iota
	OriginOpenAI
)

// Model is the per-model superset. OpenAI keys and Anthropic keys coexist without
// collision.
type Model struct {
	// OpenAI standard
	ID      string `json:"id"`
	Object  string `json:"object"`   // always "model"
	Created int64  `json:"created"`  // Unix seconds
	OwnedBy string `json:"owned_by"` // "sub2api"
	// Anthropic standard (coexists with the above — no key collision)
	Type           string         `json:"type"`             // always "model"
	DisplayName    string         `json:"display_name"`     //
	CreatedAt      string         `json:"created_at"`       // RFC3339
	MaxInputTokens int            `json:"max_input_tokens"` // 0 = unknown
	MaxTokens      int            `json:"max_tokens"`       // 0 = unknown (output cap)
	Capabilities   map[string]any `json:"capabilities,omitempty"`
}

// ModelMeta carries the REAL upstream-reported capability numbers for one model id,
// harvested from the upstream /v1/models catalog. A zero value means "upstream did
// not report it" (unknown) — which BuildModel treats differently from a real cap.
type ModelMeta struct {
	MaxInputTokens  int // upstream max_input_tokens / max_model_len / context_length; 0 = unknown
	MaxOutputTokens int // upstream max_tokens (output cap); 0 = unknown
}

// List is the listing envelope. OpenAI's {object:"list", data} and Anthropic's
// pagination triple (first_id/last_id/has_more) coexist; both protocols read `data`.
//
// Remaining/Unit are sub2api extensions carrying the caller's remaining subscription
// quota at the envelope level (a per-caller scalar, orthogonal to any single model).
type List struct {
	// OpenAI
	Object string  `json:"object"` // "list"
	Data   []Model `json:"data"`
	// Anthropic pagination — we return all models in one page. first_id/last_id are
	// *string so an empty list serializes them as null (Anthropic's convention),
	// distinct from the empty-string a non-pointer would emit.
	FirstID *string `json:"first_id"`
	LastID  *string `json:"last_id"`
	HasMore bool    `json:"has_more"`
	// sub2api extensions (omitted when unknown).
	Remaining *float64 `json:"remaining,omitempty"`
	Unit      string   `json:"unit,omitempty"`
}

const (
	ownedBy           = "sub2api"
	modelEpoch        = 1735689600 // 2025-01-01T00:00:00Z
	modelEpochRFC3339 = "2025-01-01T00:00:00Z"
)

var (
	// claudeFamilyBases: the whole current Claude line. Capabilities every 4.x model
	// supports key off this.
	claudeFamilyBases = []string{"opus", "sonnet", "haiku"}
	// effortMaxBases: tiers whose `effort` capability includes the "max" level.
	effortMaxBases = []string{"opus", "sonnet"}
	// oneMillionBases: Anthropic families Claude Code treats as 1M-context. Matched as
	// delimited tokens (no string soup).
	oneMillionBases = []string{"sonnet-4", "opus-4-6", "opus-4-7", "opus-4-8"}
)

// NormalizeModelName canonicalizes: dot→dash, then strips Claude Code's `[...]` variant
// suffix (e.g. the `[1m]` context-protocol marker).
func NormalizeModelName(model string) string {
	normalized := strings.ReplaceAll(model, ".", "-")
	if idx := strings.IndexByte(normalized, '['); idx > 0 {
		normalized = normalized[:idx]
	}
	return normalized
}

// modelMatchesBase reports whether a normalized model name contains base as a delimited
// token: base must be followed by end-of-string or '-', so "opus-4-8" matches
// "claude-opus-4-8" and "claude-opus-4-8-thinking" but NOT "opus-4-80". Never use a bare
// strings.Contains for model identity.
func modelMatchesBase(normalized, base string) bool {
	for idx := strings.Index(normalized, base); idx >= 0; {
		end := idx + len(base)
		if end == len(normalized) || normalized[end] == '-' {
			return true
		}
		next := strings.Index(normalized[idx+1:], base)
		if next < 0 {
			return false
		}
		idx += 1 + next
	}
	return false
}

func modelMatchesAnyBase(normalized string, bases ...string) bool {
	for _, base := range bases {
		if modelMatchesBase(normalized, base) {
			return true
		}
	}
	return false
}

func modelSupports1M(normalized string) bool {
	return modelMatchesAnyBase(normalized, oneMillionBases...)
}

func isClaudeFamily(normalized string) bool {
	return modelMatchesAnyBase(normalized, claudeFamilyBases...)
}

// modelContextWindow returns the CC-visible context window for a Claude display model
// (NOT the upstream input cap): 1M for the 1M families, 200k otherwise.
func modelContextWindow(normalized string) int {
	if modelSupports1M(normalized) {
		return 1000000
	}
	return 200000
}

func capObj(supported bool) map[string]any { return map[string]any{"supported": supported} }

// BuildModel derives a superset object for a model id. capabilities + max_input_tokens
// are emitted ONLY for an Anthropic-origin Claude-family model (the honest case); for any
// other origin/family the capabilities tree is omitted and max_input_tokens stays 0.
func BuildModel(id string, origin Origin, meta ModelMeta) Model {
	m := Model{
		ID:          id,
		Object:      "model",
		Created:     modelEpoch,
		OwnedBy:     ownedBy,
		Type:        "model",
		DisplayName: id,
		CreatedAt:   modelEpochRFC3339,
		MaxTokens:   0, // output cap genuinely unknown — don't fabricate
	}

	normalized := NormalizeModelName(id)
	claudeFamily := origin == OriginAnthropic && isClaudeFamily(normalized)
	anthropicOrigin := origin == OriginAnthropic

	// max_input_tokens is a neutral number: prefer the REAL upstream value (the true
	// provider's window, even when the client-facing id is a Claude name backed by
	// minimax etc.). When upstream didn't report it, only a Claude-family model falls
	// back to the family guess; a non-Claude model stays 0 (honest "unknown"). This is
	// purely additive — with no upstream meta, behavior is unchanged.
	switch {
	case meta.MaxInputTokens > 0:
		m.MaxInputTokens = meta.MaxInputTokens
	case claudeFamily:
		m.MaxInputTokens = modelContextWindow(normalized)
	}
	// Output cap: only the real upstream value, never fabricated.
	if meta.MaxOutputTokens > 0 {
		m.MaxTokens = meta.MaxOutputTokens
	}

	// Capabilities: emit the Claude capability tree for ALL anthropic-origin models, not
	// just Claude-named ones. sub2api adapts every anthropic-platform upstream (minimax,
	// glm, deepseek, …) to the Claude Messages protocol, so a client (Claude Code) needs
	// the capabilities tree to drive thinking/effort/context_management regardless of the
	// model's display name. OpenAI-origin models (gpt-*) stay capability-less — they
	// speak the OpenAI protocol and a Claude tree there would be wrong.
	if !anthropicOrigin {
		return m
	}

	effortMax := modelMatchesAnyBase(normalized, effortMaxBases...)
	m.Capabilities = map[string]any{
		"batch":              capObj(true),
		"citations":          capObj(true),
		"code_execution":     capObj(true),
		"image_input":        capObj(true),
		"pdf_input":          capObj(true),
		"structured_outputs": capObj(true),
		"context_management": map[string]any{
			// Per Anthropic's schema, the group's own `supported` is a bare bool while each
			// sub-strategy is a {"supported": …} object — the shapes differ by design.
			"supported":                true,
			"clear_thinking_20251015":  capObj(true),
			"clear_tool_uses_20250919": capObj(true),
			"compact_20260112":         capObj(true),
		},
		"effort": map[string]any{
			"supported": true,
			"low":       capObj(true),
			"medium":    capObj(true),
			"high":      capObj(true),
			"max":       capObj(effortMax),
		},
		"thinking": map[string]any{
			"supported": true,
			"types": map[string]any{
				"adaptive": capObj(true),
				"enabled":  capObj(false), // 4.x family: adaptive only
			},
		},
	}
	return m
}

// BuildList builds the listing envelope from pre-deduplicated ids. ids are sorted here
// for a stable response; origins maps each id to its source platform; metas carries the
// real upstream caps per id (nil/zero entry = unknown).
func BuildList(ids []string, origins map[string]Origin, metas map[string]ModelMeta) List {
	sorted := make([]string, len(ids))
	copy(sorted, ids)
	sort.Strings(sorted)

	data := make([]Model, 0, len(sorted))
	for _, id := range sorted {
		data = append(data, BuildModel(id, origins[id], metas[id]))
	}

	list := List{Object: "list", Data: data, HasMore: false}
	if len(data) > 0 {
		// Local copies, not &data[i].ID: an append after this point would realloc the
		// backing array and leave these pointers dangling. No such append today, but the
		// local-var form removes the landmine entirely.
		first, last := data[0].ID, data[len(data)-1].ID
		list.FirstID = &first
		list.LastID = &last
	}
	return list
}

// FilterForClaudeCode keeps only Claude-family ids (claude-opus/sonnet/haiku and their
// variants) from the listing set, dropping any raw upstream name (minimax-m2.7, glm-5.2,
// gpt-*, …). It exists because a pure Claude Code client (User-Agent claude-cli/x.y.z)
// only recognizes claude-* model names: shown a real upstream name it silently refuses
// and never sends a request. sub2api is the only layer allowed to "lie" this way —
// it adapts every anthropic-platform upstream behind a Claude name. A group that wants a
// real provider visible to Claude Code must configure a model_mapping (claude-name →
// upstream); the mapping KEY is a claude-* name and survives this filter, while a
// no-mapping account's raw name is correctly dropped.
//
// Returned ids preserve the input order. origins is read-only. Callers pass the result
// straight to BuildList; an empty result lets the handler fall back to a default Claude
// model set (still all claude-* names), never an empty or raw-name list.
func FilterForClaudeCode(ids []string, origins map[string]Origin) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		// Only anthropic-origin claude-family names. An OpenAI-origin id that happens to
		// contain a claude token is not a thing we serve to Claude Code, so gate on origin
		// too (defensive; the listing set is already platform-fused).
		if origins[id] == OriginAnthropic && isClaudeFamily(NormalizeModelName(id)) {
			out = append(out, id)
		}
	}
	return out
}

// MatchModelID resolves a client-supplied id against the listing set, sharing the
// three-tier fallback (normalized → raw → strip 8-digit date suffix) so /v1/models/{id}
// and the list never disagree on existence. Returns the canonical matched id (for
// derivation) and its origin.
func MatchModelID(id string, ids []string, origins map[string]Origin) (key string, origin Origin, ok bool) {
	if id == "" {
		return "", 0, false
	}
	set := make(map[string]struct{}, len(ids))
	for _, m := range ids {
		set[m] = struct{}{}
	}

	normalized := NormalizeModelName(id)
	if _, found := set[normalized]; found {
		return normalized, origins[normalized], true
	}
	if _, found := set[id]; found {
		return id, origins[id], true
	}
	// Strip date suffix (e.g. claude-haiku-4-5-20251001 → claude-haiku-4-5).
	if idx := strings.LastIndex(normalized, "-"); idx > 0 && len(normalized)-idx-1 == 8 {
		base := normalized[:idx]
		if _, found := set[base]; found {
			return base, origins[base], true
		}
	}
	return "", 0, false
}
