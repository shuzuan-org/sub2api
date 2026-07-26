package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// errCacheMiss 模拟 redis.Nil（service 层只判断 err != nil，不依赖具体错误类型）。
var errCacheMiss = errors.New("cache miss")

// planScopedCacheStub 按 (userID, planID) 存取订阅快照，记录失效调用。
// 所有测试同步调用，无需并发保护。
type planScopedCacheStub struct {
	billingCacheWorkerStub
	snapshots      map[[2]int64]*SubscriptionCacheData
	invalidatedIDs []int64
}

func (s *planScopedCacheStub) GetSubscriptionCache(ctx context.Context, userID, planID int64) (*SubscriptionCacheData, error) {
	if data, ok := s.snapshots[[2]int64{userID, planID}]; ok {
		return data, nil
	}
	return nil, errCacheMiss
}

func (s *planScopedCacheStub) InvalidateSubscriptionCache(ctx context.Context, userID int64) error {
	s.invalidatedIDs = append(s.invalidatedIDs, userID)
	return nil
}

// planScopedSubRepo 记录 DB 回源调用并返回预设订阅。
type planScopedSubRepo struct {
	userSubRepoNoop
	dbCalls int
	sub     *UserSubscription
}

func (r *planScopedSubRepo) GetActiveByUserIDAndPlanID(ctx context.Context, userID, planID int64) (*UserSubscription, error) {
	r.dbCalls++
	return r.sub, nil
}

func TestGetSubscriptionStatusPlanScopedCacheHit(t *testing.T) {
	cache := &planScopedCacheStub{
		snapshots: map[[2]int64]*SubscriptionCacheData{
			{1, 9}: {Status: SubscriptionStatusActive, ExpiresAt: time.Now().Add(time.Hour)},
		},
	}
	repo := &planScopedSubRepo{}
	svc := NewBillingCacheService(cache, nil, repo, nil, &config.Config{})
	t.Cleanup(svc.Stop)

	data, err := svc.GetSubscriptionStatus(context.Background(), 1, 9)
	require.NoError(t, err)
	require.Equal(t, SubscriptionStatusActive, data.Status)
	require.Zero(t, repo.dbCalls, "cache hit must not touch DB")
}

func TestGetSubscriptionStatusOtherPlanSnapshotIsMiss(t *testing.T) {
	// 场景：FIFO 队首轮换（旧订阅刚过期），缓存里只有旧 plan 12 的快照。
	// 按 (user,plan) 存取后，请求 plan 9 不会命中 plan 12 的旧数据，
	// 而是回源 DB 拿到新队首的真实状态 —— 不再出现"过期快照误杀新订阅"。
	staleExpiry := time.Now().Add(-time.Minute)
	freshExpiry := time.Now().Add(24 * time.Hour)
	cache := &planScopedCacheStub{
		snapshots: map[[2]int64]*SubscriptionCacheData{
			{1, 12}: {Status: SubscriptionStatusActive, ExpiresAt: staleExpiry},
		},
	}
	repo := &planScopedSubRepo{
		sub: &UserSubscription{
			UserID:    1,
			PlanID:    9,
			Status:    SubscriptionStatusActive,
			ExpiresAt: freshExpiry,
		},
	}
	svc := NewBillingCacheService(cache, nil, repo, nil, &config.Config{})
	t.Cleanup(svc.Stop)

	data, err := svc.GetSubscriptionStatus(context.Background(), 1, 9)
	require.NoError(t, err)
	require.Equal(t, 1, repo.dbCalls, "other plan's snapshot must not satisfy this read")
	require.WithinDuration(t, freshExpiry, data.ExpiresAt, time.Second, "must return fresh subscription, not stale snapshot")
}

// expiryRepoStub 模拟批量过期返回受影响用户。
type expiryRepoStub struct {
	userSubRepoNoop
	updated int64
	userIDs []int64
}

func (r *expiryRepoStub) BatchUpdateExpiredStatus(context.Context) (int64, []int64, error) {
	return r.updated, r.userIDs, nil
}

func TestSubscriptionExpiryInvalidatesBillingCache(t *testing.T) {
	cache := &planScopedCacheStub{}
	repo := &expiryRepoStub{updated: 2, userIDs: []int64{1, 36}}
	svc := NewSubscriptionExpiryService(repo, cache, nil, time.Minute)

	svc.runOnce()

	require.Equal(t, []int64{1, 36}, cache.invalidatedIDs)
}

func TestSubscriptionExpiryNoUpdatesNoInvalidation(t *testing.T) {
	cache := &planScopedCacheStub{}
	repo := &expiryRepoStub{}
	svc := NewSubscriptionExpiryService(repo, cache, nil, time.Minute)

	svc.runOnce()

	require.Empty(t, cache.invalidatedIDs)
}
