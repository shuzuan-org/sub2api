package metrics

import (
	"testing"
)

// 客户端可能传大写模型名（metacode -m "GLM-5.2"），归一化后应落到白名单里的
// 规范写法，而不是被拆成两条时间序列或掉进 __other__。
func TestNormalizeModelIsCaseInsensitive(t *testing.T) {
	SetAllowedModels([]string{"glm-5.2", "claude-opus-4-5"})
	t.Cleanup(func() { SetAllowedModels(nil) })

	for _, raw := range []string{"glm-5.2", "GLM-5.2", "Glm-5.2", " GLM-5.2 "} {
		if got := NormalizeModel(raw); got != "glm-5.2" {
			t.Errorf("NormalizeModel(%q) = %q, want %q", raw, got, "glm-5.2")
		}
	}

	if got := NormalizeModel("glm-4.6"); got != "__other__" {
		t.Errorf("NormalizeModel(%q) = %q, want __other__", "glm-4.6", got)
	}
}

func TestNormalizeModelCaseCollisionIsDeterministic(t *testing.T) {
	// 配置同时包含大小写别名时，精确拼写保持原样；混合大小写则稳定选择字典序最小项。
	SetAllowedModels([]string{"glm-5.2", "GLM-5.2"})
	t.Cleanup(func() { SetAllowedModels(nil) })

	if got := NormalizeModel("glm-5.2"); got != "glm-5.2" {
		t.Fatalf("exact lowercase = %q, want glm-5.2", got)
	}
	if got := NormalizeModel("GLM-5.2"); got != "GLM-5.2" {
		t.Fatalf("exact uppercase = %q, want GLM-5.2", got)
	}
	if got := NormalizeModel("GlM-5.2"); got != "GLM-5.2" {
		t.Fatalf("folded collision = %q, want GLM-5.2", got)
	}
}

func TestNormalizeModelEmptyAllowlistPreservesTrimmedInput(t *testing.T) {
	SetAllowedModels(nil)
	if got := NormalizeModel(" GLM-5.2 "); got != "GLM-5.2" {
		t.Fatalf("NormalizeModel with empty allowlist = %q, want GLM-5.2", got)
	}
}
