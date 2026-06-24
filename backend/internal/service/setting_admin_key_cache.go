package service

import (
	"context"
	"sync"
	"time"
)

// Admin API Key 鉴权读路径缓存。
//
// 每个使用 Admin API Key 的管理接口请求都会调用 GetAdminAPIKey 查 settings 表，
// 这里加一个短 TTL 的单值缓存消除该查询。admin key 的写入（生成/删除）会即时
// 清除本实例缓存；多副本部署下，其它实例最多在一个 TTL 后失效，由 TTL 兜底，
// 故 TTL 取较短值。
const adminAPIKeyCacheTTL = 30 * time.Second

type adminAPIKeyCache struct {
	mu        sync.RWMutex
	value     string
	expiresAt time.Time
	// hasValue 区分“缓存了空值（未配置）”与“尚未缓存”，避免未配置时每请求回源。
	hasValue bool
}

func (c *adminAPIKeyCache) get(now time.Time) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.hasValue || now.After(c.expiresAt) {
		return "", false
	}
	return c.value, true
}

func (c *adminAPIKeyCache) set(value string, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value = value
	c.expiresAt = now.Add(adminAPIKeyCacheTTL)
	c.hasValue = true
}

func (c *adminAPIKeyCache) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hasValue = false
	c.value = ""
	c.expiresAt = time.Time{}
}

// GetAdminAPIKeyCached 是 GetAdminAPIKey 的缓存版本，专供鉴权使用。
func (s *SettingService) GetAdminAPIKeyCached(ctx context.Context) (string, error) {
	now := time.Now()
	if v, ok := s.adminKeyCache.get(now); ok {
		return v, nil
	}
	key, err := s.GetAdminAPIKey(ctx)
	if err != nil {
		return "", err
	}
	s.adminKeyCache.set(key, now)
	return key, nil
}
