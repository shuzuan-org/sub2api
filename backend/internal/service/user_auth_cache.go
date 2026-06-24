package service

import (
	"context"
	"strconv"
	"time"

	gocache "github.com/patrickmn/go-cache"
)

// 鉴权读路径专用的进程内用户缓存。
//
// 背景：管理后台 / 普通接口的每个请求都会经过鉴权中间件，而 JWT 鉴权需要
// GetByID、Admin API Key 鉴权需要 GetFirstAdmin，二者原本每请求同步查库，
// 成为所有接口共担的“通行税”。这里加一层短 TTL 的本地缓存来消除该查询。
//
// 注意：此缓存只供鉴权使用，**不能**替换 GetByID/GetFirstAdmin 的实时语义——
// 业务路径（如读余额、扣费后回读）仍需实时数据，因此单独提供 *Cached 方法，
// 不改动原方法。鉴权侧本就用 TokenVersion 校验保证改密后旧 token 失效，
// 允许短暂（默认 30s）的陈旧。所有用户写操作已统一调用 InvalidateAuthCacheByUserID，
// 我们在其旁清掉本地缓存，保证变更及时可见。
const (
	defaultUserAuthCacheTTL     = 30 * time.Second
	defaultUserAuthCacheCleanup = 5 * time.Minute
	firstAdminCacheKey          = "first_admin"
)

// userAuthCache 封装本地用户缓存；TTL<=0 时整体禁用，方法退化为直查 DB。
type userAuthCache struct {
	cache *gocache.Cache
	ttl   time.Duration
}

func newUserAuthCache(ttl time.Duration) *userAuthCache {
	if ttl <= 0 {
		return &userAuthCache{}
	}
	return &userAuthCache{
		cache: gocache.New(ttl, defaultUserAuthCacheCleanup),
		ttl:   ttl,
	}
}

func (c *userAuthCache) enabled() bool {
	return c != nil && c.cache != nil
}

func userAuthCacheKey(id int64) string {
	return strconv.FormatInt(id, 10)
}

func (c *userAuthCache) getByID(id int64) (*User, bool) {
	if !c.enabled() {
		return nil, false
	}
	if v, ok := c.cache.Get(userAuthCacheKey(id)); ok {
		if u, ok := v.(*User); ok {
			return u, true
		}
	}
	return nil, false
}

func (c *userAuthCache) setByID(id int64, u *User) {
	if !c.enabled() || u == nil {
		return
	}
	c.cache.SetDefault(userAuthCacheKey(id), u)
}

func (c *userAuthCache) getFirstAdmin() (*User, bool) {
	if !c.enabled() {
		return nil, false
	}
	if v, ok := c.cache.Get(firstAdminCacheKey); ok {
		if u, ok := v.(*User); ok {
			return u, true
		}
	}
	return nil, false
}

func (c *userAuthCache) setFirstAdmin(u *User) {
	if !c.enabled() || u == nil {
		return
	}
	c.cache.SetDefault(firstAdminCacheKey, u)
}

// invalidate 清除指定用户的本地缓存。由于无法低成本判断该用户是否为 first admin，
// 任一用户失效都连带清除 first_admin 条目（其代价仅为下次 admin 鉴权回源一次）。
func (c *userAuthCache) invalidate(id int64) {
	if !c.enabled() {
		return
	}
	c.cache.Delete(userAuthCacheKey(id))
	c.cache.Delete(firstAdminCacheKey)
}

// GetByIDCached 供鉴权读路径使用：优先读本地缓存，未命中再回源并写缓存。
// 与 GetByID 的区别在于可能返回至多一个 TTL 内的陈旧用户（鉴权侧由 TokenVersion 兜底）。
func (s *UserService) GetByIDCached(ctx context.Context, id int64) (*User, error) {
	if u, ok := s.authUserCache.getByID(id); ok {
		return u, nil
	}
	user, err := s.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	s.authUserCache.setByID(id, user)
	return user, nil
}

// GetFirstAdminCached 供 Admin API Key 鉴权使用：缓存首个管理员用户。
func (s *UserService) GetFirstAdminCached(ctx context.Context) (*User, error) {
	if u, ok := s.authUserCache.getFirstAdmin(); ok {
		return u, nil
	}
	admin, err := s.GetFirstAdmin(ctx)
	if err != nil {
		return nil, err
	}
	s.authUserCache.setFirstAdmin(admin)
	if admin != nil {
		s.authUserCache.setByID(admin.ID, admin)
	}
	return admin, nil
}

// invalidateAuthUserCache 在用户写操作后清除本地鉴权缓存。
func (s *UserService) invalidateAuthUserCache(userID int64) {
	s.authUserCache.invalidate(userID)
}
