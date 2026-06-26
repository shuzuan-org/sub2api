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
	// Capabilities is the REAL upstream capability tree (image_input, pdf_input, effort,
	// thinking, …), harvested verbatim from the upstream /v1/models entry. nil means the
	// upstream did not report it — BuildModel then falls back to the hardcoded Claude
	// default tree. Non-nil values from sub2api's own anthropic upstreams are structurally
	// identical to that default tree, so passing the whole tree through is exact.
	Capabilities map[string]any
}

// MergeMeta folds incoming into cur field-by-field, first-non-zero wins per field: a field
// already set on cur is kept, an unset (zero/empty) field is filled from incoming. This is
// the single merge rule for accumulating one model's meta across several upstream probes
// (multiple accounts in a group, or several aliases collapsing onto one real name).
//
// Crucially it merges EVERY field independently — MaxInputTokens, MaxOutputTokens, and
// Capabilities each survive on their own. An earlier "aliases share identical meta"
// assumption let a wholesale overwrite silently drop a field one probe had and the next
// didn't; per-field first-non-zero removes that footgun.
//
// The returned Capabilities may alias incoming's map (no deep copy) — callers that cache
// the result must clone first (see cloneModelMetaMap). len()==0 counts as "unset" for
// Capabilities so an empty {} never shadows a real tree.
func MergeMeta(cur, incoming ModelMeta) ModelMeta {
	if cur.MaxInputTokens == 0 {
		cur.MaxInputTokens = incoming.MaxInputTokens
	}
	if cur.MaxOutputTokens == 0 {
		cur.MaxOutputTokens = incoming.MaxOutputTokens
	}
	if len(cur.Capabilities) == 0 && len(incoming.Capabilities) > 0 {
		cur.Capabilities = incoming.Capabilities
	}
	return cur
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
	// gptFamilyBases: the OpenAI GPT line. Used to surface only gpt-* names to a Codex
	// client. Matched as a delimited token (gpt-5, gpt-5.5-codex, …), never a bare substring.
	gptFamilyBases = []string{"gpt"}
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

func isGPTFamily(normalized string) bool {
	return modelMatchesAnyBase(normalized, gptFamilyBases...)
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

	// Real upstream capabilities win: sub2api's anthropic upstreams (glm, minimax, …) report
	// a per-model capability tree structurally identical to the default below, but with the
	// HONEST values (a text-only model reports image_input=false). When present, pass the
	// whole tree through verbatim — no per-field merge, no fabrication. An empty {} counts as
	// "not reported" (same as nil): never emit a hollow tree, fall back instead.
	//
	// The tree is shared by reference into the response (no copy). Callers must own meta —
	// GetSupersetModels feeds clones from cloneModelMetaMap, so the map is request-private and
	// serialized read-only. Don't feed BuildModel a cached meta directly.
	if len(meta.Capabilities) > 0 {
		m.Capabilities = meta.Capabilities
		return m
	}

	// Upstream didn't report capabilities (a real api.anthropic.com upstream omits the
	// field) → fall back to the hardcoded Claude tree, which is correct for genuine Claude
	// models. This is the historical behavior; with no upstream caps, output is unchanged.
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
// variants) from the listing set. A pure Claude Code client (User-Agent claude-cli/x.y.z)
// only recognizes claude-* names: shown a raw upstream name (minimax-m2.7, gpt-5.5, …) it
// silently refuses and never sends a request. So we surface only the claude-* mapping keys
// an operator configured. If the group has no claude-* mapping key, the result is empty —
// the handler must return an empty list, NOT fabricate default claude names that can't
// route here.
//
// Filtering is purely by NAME shape (mapping key), independent of the upstream platform:
// an anthropic group may expose a gpt-5.5 alias and an openai group is just as likely to
// carry claude aliases. origins is unused here (kept for signature symmetry with callers).
// Returned ids preserve input order.
func FilterForClaudeCode(ids []string, origins map[string]Origin) []string {
	return filterByFamily(ids, isClaudeFamily)
}

// FilterForCodex keeps only GPT-family ids (gpt-*) from the listing set. A Codex client
// recognizes gpt-* names; same contract as FilterForClaudeCode but for the OpenAI line.
// Empty result → empty list (no fabricated defaults).
func FilterForCodex(ids []string, origins map[string]Origin) []string {
	return filterByFamily(ids, isGPTFamily)
}

// filterByFamily keeps ids whose normalized name matches the predicate, preserving order.
func filterByFamily(ids []string, match func(string) bool) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if match(NormalizeModelName(id)) {
			out = append(out, id)
		}
	}
	return out
}

// RealUpstreamNames returns the distinct real upstream model names backing the listing,
// for clients that are neither Claude Code nor Codex, along with the metadata and origin
// re-keyed onto those real names. upstreams maps each client-facing id (a mapping key, or
// the raw name for no-mapping accounts) to the real upstream model it resolves to; metas
// and origins are keyed by the client-facing id. Multiple aliases collapsing onto one
// upstream (e.g. group 38's five keys all → MiniMax-M3) yield a single entry, and that
// entry inherits the real caps from whichever alias carried them (so the real name keeps
// its true max_input_tokens instead of going to 0). Order is sorted for stability.
func RealUpstreamNames(ids []string, upstreams map[string]string, metas map[string]ModelMeta, origins map[string]Origin) (outIDs []string, outMetas map[string]ModelMeta, outOrigins map[string]Origin) {
	seen := make(map[string]struct{}, len(ids))
	outMetas = make(map[string]ModelMeta, len(ids))
	outOrigins = make(map[string]Origin, len(ids))
	for _, id := range ids {
		up := upstreams[id]
		if up == "" {
			up = id // no mapping recorded → the id IS the real name
		}
		seen[up] = struct{}{}
		// Accumulate this alias's real meta onto the upstream name. Aliases of one upstream are
		// ASSUMED to come from one probe, but MergeMeta folds per-field first-non-zero, so even
		// if they diverge (misconfig, split across accounts) no field is silently dropped.
		outMetas[up] = MergeMeta(outMetas[up], metas[id])
		// Origin gates capability emission. Anthropic wins on collision (same rule as fusion).
		if cur, ok := outOrigins[up]; !ok || (cur != OriginAnthropic && origins[id] == OriginAnthropic) {
			outOrigins[up] = origins[id]
		}
	}
	outIDs = make([]string, 0, len(seen))
	for up := range seen {
		outIDs = append(outIDs, up)
	}
	sort.Strings(outIDs)
	return outIDs, outMetas, outOrigins
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
