// Package metrics 模型名归一化工具，控制 Prometheus 标签基数。
//
// 设计原则：
//   - 模型名作为 Prometheus 标签值时必须受控，避免用户随意拼写导致 series 爆炸。
//   - 通过 allowlist 机制：白名单内的模型名原样输出，不在白名单中的统一归入 "__other__"。
//   - allowlist 为空（开发环境/未配置）时降级为直接返回原始值，不做归一化。
package metrics

import (
	"strings"
	"sync"
)

// NormalizeModel 将原始模型名映射为归一化后的 Prometheus 标签值。
//
// 规则：
//  1. 空字符串返回空字符串（不记录 model 标签）
//  2. allowlist 未初始化（len == 0）时返回原始值（降级，避免开发环境阻滞）
//  3. 在 allowlist 中的模型名原样返回
//  4. 不在 allowlist 中的归入 "__other__"
//
// 线程安全：允许在请求热路径并发调用。
func NormalizeModel(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	allowedModelsMu.RLock()
	defer allowedModelsMu.RUnlock()

	if len(allowedModels) == 0 {
		// allowlist 未初始化（开发环境/未配置），降级返回原始值
		return raw
	}

	if allowedModels[raw] {
		return raw
	}
	// 大小写不敏感兜底：客户端传 "GLM-5.2" 时归一到白名单里的 "glm-5.2"，
	// 避免同一模型因大小写差异被拆成两条时间序列（或落进 __other__）。
	if canonical, ok := allowedModelsFold[strings.ToLower(raw)]; ok {
		return canonical
	}
	return "__other__"
}

var (
	allowedModels = make(map[string]bool)
	// allowedModelsFold: 小写模型名 → 白名单中的规范写法
	allowedModelsFold = make(map[string]string)
	allowedModelsMu   sync.RWMutex
)

// SetAllowedModels 设置模型归一化白名单（线程安全）。
//
// 调用时机：配置加载后、服务启动前调用一次；如需从 DB 动态刷新，可定时调用。
// 传入 nil 或空切片将清空白名单（后续 NormalizeModel 进入降级模式）。
func SetAllowedModels(models []string) {
	m := make(map[string]bool, len(models))
	fold := make(map[string]string, len(models))
	for _, name := range models {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		m[name] = true
		// 白名单内出现仅大小写不同的重名时按字典序取最小，保证映射稳定
		lower := strings.ToLower(name)
		if existing, ok := fold[lower]; !ok || name < existing {
			fold[lower] = name
		}
	}
	allowedModelsMu.Lock()
	allowedModels = m
	allowedModelsFold = fold
	allowedModelsMu.Unlock()
}

// AllowedModelCount 返回当前白名单中的模型数量（供测试和调试用）。
func AllowedModelCount() int {
	allowedModelsMu.RLock()
	defer allowedModelsMu.RUnlock()
	return len(allowedModels)
}
