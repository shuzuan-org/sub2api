//go:build unit

package service

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// countingUserRepo 嵌入 mockUserRepo 并对 GetByID / GetFirstAdmin 计数与返回可控数据，
// 用于验证鉴权缓存的命中与失效。
type countingUserRepo struct {
	mockUserRepo
	getByIDCalls       atomic.Int64
	getFirstAdminCalls atomic.Int64
	user               *User
	admin              *User
}

func (r *countingUserRepo) GetByID(context.Context, int64) (*User, error) {
	r.getByIDCalls.Add(1)
	return r.user, nil
}

func (r *countingUserRepo) GetFirstAdmin(context.Context) (*User, error) {
	r.getFirstAdminCalls.Add(1)
	return r.admin, nil
}

func newUserServiceForCacheTest(repo UserRepository) *UserService {
	return NewUserService(repo, nil, nil)
}

func TestGetByIDCached_CachesAcrossCalls(t *testing.T) {
	repo := &countingUserRepo{user: &User{ID: 7, TokenVersion: 1}}
	svc := newUserServiceForCacheTest(repo)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		u, err := svc.GetByIDCached(ctx, 7)
		require.NoError(t, err)
		require.Equal(t, int64(7), u.ID)
	}

	require.Equal(t, int64(1), repo.getByIDCalls.Load(), "应仅回源一次，其余命中缓存")
}

func TestGetByIDCached_InvalidationForcesReload(t *testing.T) {
	repo := &countingUserRepo{user: &User{ID: 7, TokenVersion: 1}}
	svc := newUserServiceForCacheTest(repo)
	ctx := context.Background()

	_, err := svc.GetByIDCached(ctx, 7)
	require.NoError(t, err)

	// 模拟改密：TokenVersion 自增 + 失效缓存（ChangePassword 内部路径）
	repo.user = &User{ID: 7, TokenVersion: 2}
	svc.invalidateAuthUserCache(7)

	u, err := svc.GetByIDCached(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, int64(2), u.TokenVersion, "失效后应读到自增的 TokenVersion")
	require.Equal(t, int64(2), repo.getByIDCalls.Load(), "失效后必须再次回源")
}

func TestGetFirstAdminCached_CachesAndInvalidates(t *testing.T) {
	repo := &countingUserRepo{admin: &User{ID: 1, Role: "admin"}}
	svc := newUserServiceForCacheTest(repo)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, err := svc.GetFirstAdminCached(ctx)
		require.NoError(t, err)
	}
	require.Equal(t, int64(1), repo.getFirstAdminCalls.Load(), "first admin 应仅回源一次")

	// 任一用户失效都连带清除 first_admin 条目
	svc.invalidateAuthUserCache(42)

	_, err := svc.GetFirstAdminCached(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(2), repo.getFirstAdminCalls.Load(), "失效后 first admin 应再次回源")
}

func TestGetByIDCached_DisabledWhenTTLZero(t *testing.T) {
	repo := &countingUserRepo{user: &User{ID: 7}}
	svc := newUserServiceForCacheTest(repo)
	svc.authUserCache = newUserAuthCache(0) // TTL<=0 关闭缓存

	for i := 0; i < 3; i++ {
		_, err := svc.GetByIDCached(context.Background(), 7)
		require.NoError(t, err)
	}
	require.Equal(t, int64(3), repo.getByIDCalls.Load(), "缓存关闭时每次都回源")
}
