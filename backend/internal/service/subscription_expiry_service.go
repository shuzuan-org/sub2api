package service

import (
	"context"
	"log"
	"sync"
	"time"
)

// SubscriptionExpiryService periodically updates expired subscription status.
type SubscriptionExpiryService struct {
	userSubRepo UserSubscriptionRepository
	// billingCache/subService 用于在订阅被标记 expired 后立即失效对应用户的
	// 计费缓存（Redis billing:sub:{userID}）与合并订阅 L1 缓存。
	// 不失效的话，按 userID 键存的旧订阅快照会让该用户后续请求在 TTL 内（最长 5 分钟）
	// 全部被误判为 SUBSCRIPTION_INVALID。两者均允许为 nil（测试或降级场景）。
	billingCache BillingCache
	subService   *SubscriptionService
	interval     time.Duration
	stopCh       chan struct{}
	stopOnce     sync.Once
	wg           sync.WaitGroup
}

func NewSubscriptionExpiryService(userSubRepo UserSubscriptionRepository, billingCache BillingCache, subService *SubscriptionService, interval time.Duration) *SubscriptionExpiryService {
	return &SubscriptionExpiryService{
		userSubRepo:  userSubRepo,
		billingCache: billingCache,
		subService:   subService,
		interval:     interval,
		stopCh:       make(chan struct{}),
	}
}

func (s *SubscriptionExpiryService) Start() {
	if s == nil || s.userSubRepo == nil || s.interval <= 0 {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		s.runOnce()
		for {
			select {
			case <-ticker.C:
				s.runOnce()
			case <-s.stopCh:
				return
			}
		}
	}()
}

func (s *SubscriptionExpiryService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

func (s *SubscriptionExpiryService) runOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	updated, userIDs, err := s.userSubRepo.BatchUpdateExpiredStatus(ctx)
	if err != nil {
		log.Printf("[SubscriptionExpiry] Update expired subscriptions failed: %v", err)
		return
	}
	if updated > 0 {
		log.Printf("[SubscriptionExpiry] Updated %d expired subscriptions", updated)
	}
	s.invalidateCaches(userIDs)
}

// invalidateCaches 使用独立超时：DB 更新即使耗尽 runOnce 的预算，失效步骤也要有
// 完整的时间窗（失效失败的兜底只有缓存 TTL，值得单独保障）。
func (s *SubscriptionExpiryService) invalidateCaches(userIDs []int64) {
	if len(userIDs) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, userID := range userIDs {
		if s.billingCache != nil {
			if err := s.billingCache.InvalidateSubscriptionCache(ctx, userID); err != nil {
				log.Printf("[SubscriptionExpiry] Invalidate billing cache failed for user %d: %v", userID, err)
			}
		}
		if s.subService != nil {
			s.subService.InvalidateMergedSubCache(userID)
		}
	}
}
