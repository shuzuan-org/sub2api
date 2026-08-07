//go:build unit

package service

import (
	"testing"
)

// 线上场景：账号 ks-anthropic-glm-5.2 的 model_mapping 配的是小写 glm-5.2，
// 客户端 metacode -m "GLM-5.2" 传大写模型名，导致账号被 model_mapping 过滤掉，
// 最终 503 no available accounts。
func TestAccountModelMatchingIsCaseInsensitive(t *testing.T) {
	account := &Account{
		Platform: PlatformAnthropic,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"glm-5.2": "glm-5.2",
			},
		},
	}

	for _, requested := range []string{"glm-5.2", "GLM-5.2", "Glm-5.2"} {
		if !account.IsModelSupported(requested) {
			t.Errorf("IsModelSupported(%q) = false, want true", requested)
		}
		if got := account.GetMappedModel(requested); got != "glm-5.2" {
			t.Errorf("GetMappedModel(%q) = %q, want %q", requested, got, "glm-5.2")
		}
	}

	if account.IsModelSupported("glm-4.6") {
		t.Error("IsModelSupported(\"glm-4.6\") = true, want false")
	}
}

func TestAccountWildcardMappingIsCaseInsensitive(t *testing.T) {
	account := &Account{
		Platform: PlatformAnthropic,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"glm-*": "glm-5.2",
			},
		},
	}

	if !account.IsModelSupported("GLM-5.2") {
		t.Error("IsModelSupported(\"GLM-5.2\") = false, want true")
	}
	if got := account.GetMappedModel("GLM-5.2"); got != "glm-5.2" {
		t.Errorf("GetMappedModel(\"GLM-5.2\") = %q, want %q", got, "glm-5.2")
	}
}

// 精确（区分大小写）匹配仍优先于大小写不敏感匹配，避免改变已有配置的行为。
func TestAccountExactMatchWinsOverFoldMatch(t *testing.T) {
	account := &Account{
		Platform: PlatformAnthropic,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"glm-5.2": "upstream-lower",
				"GLM-5.2": "upstream-upper",
			},
		},
	}

	if got := account.GetMappedModel("glm-5.2"); got != "upstream-lower" {
		t.Errorf("GetMappedModel(\"glm-5.2\") = %q, want %q", got, "upstream-lower")
	}
	if got := account.GetMappedModel("GLM-5.2"); got != "upstream-upper" {
		t.Errorf("GetMappedModel(\"GLM-5.2\") = %q, want %q", got, "upstream-upper")
	}
	// 两个 key 都只在大小写上不同时，按字典序取最小，保证结果确定。
	if got := account.GetMappedModel("Glm-5.2"); got != "upstream-upper" {
		t.Errorf("GetMappedModel(\"Glm-5.2\") = %q, want %q", got, "upstream-upper")
	}
}

func TestGroupRoutingIsCaseInsensitive(t *testing.T) {
	group := &Group{
		ModelRoutingEnabled: true,
		ModelRouting: map[string][]int64{
			"glm-5.2": {42},
		},
	}

	for _, requested := range []string{"glm-5.2", "GLM-5.2", "Glm-5.2"} {
		got := group.GetRoutingAccountIDs(requested)
		if len(got) != 1 || got[0] != 42 {
			t.Errorf("GetRoutingAccountIDs(%q) = %v, want [42]", requested, got)
		}
	}

	if got := group.GetRoutingAccountIDs("glm-4.6"); got != nil {
		t.Errorf("GetRoutingAccountIDs(\"glm-4.6\") = %v, want nil", got)
	}
}

// 多条规则同时命中时按 pattern 长度降序取最长，不受 map 迭代顺序影响。
func TestGroupRoutingPrefersLongestPattern(t *testing.T) {
	group := &Group{
		ModelRoutingEnabled: true,
		ModelRouting: map[string][]int64{
			"glm-*":     {1},
			"glm-5*":    {2},
			"glm-5.2-*": {3},
		},
	}

	for i := 0; i < 50; i++ {
		got := group.GetRoutingAccountIDs("GLM-5.2-AIR")
		if len(got) != 1 || got[0] != 3 {
			t.Fatalf("GetRoutingAccountIDs(\"GLM-5.2-AIR\") = %v, want [3]", got)
		}
	}
}
