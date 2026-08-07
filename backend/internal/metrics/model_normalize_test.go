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
