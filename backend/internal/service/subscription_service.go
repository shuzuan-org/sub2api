package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/dgraph-io/ristretto"
	"golang.org/x/sync/singleflight"
)

// MaxExpiresAt is the maximum allowed expiration date (year 2099)
// This prevents time.Time JSON serialization errors (RFC 3339 requires year <= 9999)
var MaxExpiresAt = time.Date(2099, 12, 31, 23, 59, 59, 0, time.UTC)

// MaxValidityDays is the maximum allowed validity days for subscriptions (100 years)
const MaxValidityDays = 36500

var (
	ErrSubscriptionNotFound       = infraerrors.NotFound("SUBSCRIPTION_NOT_FOUND", "subscription not found")
	ErrSubscriptionExpired        = infraerrors.Forbidden("SUBSCRIPTION_EXPIRED", "subscription has expired")
	ErrSubscriptionSuspended      = infraerrors.Forbidden("SUBSCRIPTION_SUSPENDED", "subscription is suspended")
	ErrSubscriptionAlreadyExists  = infraerrors.Conflict("SUBSCRIPTION_ALREADY_EXISTS", "subscription already exists for this user and group")
	ErrSubscriptionAssignConflict = infraerrors.Conflict("SUBSCRIPTION_ASSIGN_CONFLICT", "subscription exists but request conflicts with existing assignment semantics")
	ErrPlanNotFound = infraerrors.NotFound("PLAN_NOT_FOUND", "subscription plan not found")
	ErrInvalidInput               = infraerrors.BadRequest("INVALID_INPUT", "at least one of resetDaily, resetWeekly, or resetMonthly must be true")
	ErrDailyLimitExceeded         = infraerrors.TooManyRequests("DAILY_LIMIT_EXCEEDED", "daily usage limit exceeded")
	ErrWeeklyLimitExceeded        = infraerrors.TooManyRequests("WEEKLY_LIMIT_EXCEEDED", "weekly usage limit exceeded")
	ErrMonthlyLimitExceeded       = infraerrors.TooManyRequests("MONTHLY_LIMIT_EXCEEDED", "monthly usage limit exceeded")
	ErrSubscriptionNilInput       = infraerrors.BadRequest("SUBSCRIPTION_NIL_INPUT", "subscription input cannot be nil")
	ErrAdjustWouldExpire          = infraerrors.BadRequest("ADJUST_WOULD_EXPIRE", "adjustment would result in expired subscription (remaining days must be > 0)")
	ErrPlanNotPurchasable         = infraerrors.BadRequest("PLAN_NOT_PURCHASABLE", "this plan is not available for purchase")
	ErrPurchaseTooFrequent        = infraerrors.Conflict("PURCHASE_TOO_FREQUENT", "please wait before purchasing again")
	ErrPlanHasActiveSubscriptions = infraerrors.Conflict("PLAN_HAS_ACTIVE_SUBSCRIPTIONS", "cannot delete plan with active subscriptions")
)

// SubscriptionService 订阅服务
type SubscriptionService struct {
	planRepo            SubscriptionPlanRepository
	userSubRepo         UserSubscriptionRepository
	userRepo            UserRepository
	billingCacheService *BillingCacheService
	entClient           *dbent.Client

	// L1 缓存：加速中间件热路径的订阅查询
	subCacheL1     *ristretto.Cache
	subCacheGroup  singleflight.Group
	subCacheTTL    time.Duration
	subCacheJitter int // 抖动百分比

	maintenanceQueue *SubscriptionMaintenanceQueue
}

// NewSubscriptionService 创建订阅服务
func NewSubscriptionService(planRepo SubscriptionPlanRepository, userSubRepo UserSubscriptionRepository, userRepo UserRepository, billingCacheService *BillingCacheService, entClient *dbent.Client, cfg *config.Config) *SubscriptionService {
	svc := &SubscriptionService{
		planRepo:            planRepo,
		userSubRepo:         userSubRepo,
		userRepo:            userRepo,
		billingCacheService: billingCacheService,
		entClient:           entClient,
	}
	svc.initSubCache(cfg)
	svc.initMaintenanceQueue(cfg)
	return svc
}

func (s *SubscriptionService) initMaintenanceQueue(cfg *config.Config) {
	if cfg == nil {
		slog.Info("[SubscriptionMaintenance] no config, window maintenance runs synchronously")
		return
	}
	mc := cfg.SubscriptionMaintenance
	if mc.WorkerCount <= 0 || mc.QueueSize <= 0 {
		slog.Info("[SubscriptionMaintenance] window maintenance runs synchronously",
			"worker_count", mc.WorkerCount, "queue_size", mc.QueueSize)
		return
	}
	s.maintenanceQueue = NewSubscriptionMaintenanceQueue(mc.WorkerCount, mc.QueueSize)
	slog.Info("[SubscriptionMaintenance] async mode", "workers", mc.WorkerCount, "queue", mc.QueueSize)
}

// Stop stops the maintenance worker pool.
func (s *SubscriptionService) Stop() {
	if s == nil {
		return
	}
	if s.maintenanceQueue != nil {
		s.maintenanceQueue.Stop()
	}
}

// initSubCache 初始化订阅 L1 缓存
func (s *SubscriptionService) initSubCache(cfg *config.Config) {
	if cfg == nil {
		return
	}
	sc := cfg.SubscriptionCache
	if sc.L1Size <= 0 || sc.L1TTLSeconds <= 0 {
		return
	}
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: int64(sc.L1Size) * 10,
		MaxCost:     int64(sc.L1Size),
		BufferItems: 64,
	})
	if err != nil {
		slog.Warn("failed to init subscription L1 cache", "error", err)
		return
	}
	s.subCacheL1 = cache
	s.subCacheTTL = time.Duration(sc.L1TTLSeconds) * time.Second
	s.subCacheJitter = sc.JitterPercent
}

// subCacheKey 生成订阅缓存 key（热路径，避免 fmt.Sprintf 开销）
func subCacheKey(userID, planID int64) string {
	return "sub:" + strconv.FormatInt(userID, 10) + ":" + strconv.FormatInt(planID, 10)
}

// mergedSubCacheKey 生成合并订阅缓存 key
func mergedSubCacheKey(userID int64) string {
	return "msub:" + strconv.FormatInt(userID, 10)
}

// jitteredTTL 为 TTL 添加抖动，避免集中过期
func (s *SubscriptionService) jitteredTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 || s.subCacheJitter <= 0 {
		return ttl
	}
	pct := s.subCacheJitter
	if pct > 100 {
		pct = 100
	}
	delta := float64(pct) / 100
	factor := 1 - delta + rand.Float64()*(2*delta)
	if factor <= 0 {
		return ttl
	}
	return time.Duration(float64(ttl) * factor)
}

// InvalidateSubCache 失效指定用户+计划的订阅 L1 缓存
// 同时失效合并缓存，因为单个订阅变化会影响合并结果
func (s *SubscriptionService) InvalidateSubCache(userID, planID int64) {
	if s.subCacheL1 == nil {
		return
	}
	s.subCacheL1.Del(subCacheKey(userID, planID))
	s.subCacheL1.Del(mergedSubCacheKey(userID))
}

// InvalidateMergedSubCache 失效指定用户的合并订阅 L1 缓存
func (s *SubscriptionService) InvalidateMergedSubCache(userID int64) {
	if s.subCacheL1 == nil {
		return
	}
	s.subCacheL1.Del(mergedSubCacheKey(userID))
}

// PurchaseSubscriptionInput 用户自助购买订阅输入
type PurchaseSubscriptionInput struct {
	UserID int64
	PlanID int64
}

// PurchaseSubscription 用户使用余额购买订阅
// 事务内完成：校验 plan → 校验余额 → 扣余额 → 创建/续期订阅
func (s *SubscriptionService) PurchaseSubscription(ctx context.Context, input *PurchaseSubscriptionInput) (*UserSubscription, error) {
	// 1. 校验 plan
	plan, err := s.planRepo.GetByID(ctx, input.PlanID)
	if err != nil {
		return nil, ErrPlanNotFound
	}
	if !plan.IsActive() {
		return nil, ErrPlanNotPurchasable
	}
	if !plan.IsPublic() {
		return nil, ErrPlanNotPurchasable
	}
	if plan.Price == nil || *plan.Price <= 0 {
		return nil, ErrPlanNotPurchasable
	}

	price := *plan.Price
	validityDays := plan.DefaultValidityDays
	if validityDays <= 0 {
		validityDays = 30
	}

	// 2. 校验余额（先读取，事务内再次校验防并发）
	user, err := s.userRepo.GetByID(ctx, input.UserID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if user.Balance < price {
		return nil, ErrInsufficientBalance
	}

	// 3. 事务：扣余额 + 创建/续期订阅
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	txCtx := dbent.NewTxContext(ctx, tx)

	// 3a. 扣余额（DeductBalance 在事务上下文中使用行锁防并发）
	if err := s.userRepo.DeductBalance(txCtx, input.UserID, price); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("deduct balance: %w", err)
	}

	// 3b. 事务内重新读取余额确认未透支
	updatedUser, err := s.userRepo.GetByID(txCtx, input.UserID)
	if err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("verify balance: %w", err)
	}
	if updatedUser.Balance < 0 {
		_ = tx.Rollback()
		return nil, ErrInsufficientBalance
	}

	// 3c. 新建订阅（每次购买都独立创建，支持叠加）
	notes := fmt.Sprintf("购买订阅，扣费 $%.4f", price)
	now := time.Now()

	// 防重：10 秒内同 plan 刚购买过，拒绝重复提交
	existingSub, err := s.userSubRepo.GetLatestByUserIDAndPlanID(txCtx, input.UserID, input.PlanID)
	if err != nil && !errors.Is(err, ErrSubscriptionNotFound) {
		_ = tx.Rollback()
		return nil, fmt.Errorf("check existing subscription: %w", err)
	}
	if existingSub != nil && time.Since(existingSub.UpdatedAt) < 10*time.Second {
		_ = tx.Rollback()
		return nil, ErrPurchaseTooFrequent
	}

	newSub := &UserSubscription{
		UserID:     input.UserID,
		PlanID:     input.PlanID,
		StartsAt:   now,
		ExpiresAt:  now.AddDate(0, 0, validityDays),
		Status:     SubscriptionStatusActive,
		AssignedAt: now,
		Notes:      notes,
	}
	if newSub.ExpiresAt.After(MaxExpiresAt) {
		newSub.ExpiresAt = MaxExpiresAt
	}

	if err := s.userSubRepo.Create(txCtx, newSub); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("create subscription: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	s.InvalidateSubCache(input.UserID, input.PlanID)
	s.InvalidateMergedSubCache(input.UserID)
	if s.billingCacheService != nil {
		s.billingCacheService.QueueDeductBalance(input.UserID, price)
	}

	return newSub, nil
}

// AssignSubscriptionInput 分配订阅输入
type AssignSubscriptionInput struct {
	UserID       int64
	PlanID       int64
	ValidityDays int
	AssignedBy   int64
	Notes        string
}

// AssignSubscription 分配订阅给用户（不允许重复分配）
func (s *SubscriptionService) AssignSubscription(ctx context.Context, input *AssignSubscriptionInput) (*UserSubscription, error) {
	sub, _, err := s.assignSubscriptionWithReuse(ctx, input)
	if err != nil {
		return nil, err
	}
	return sub, nil
}

// AssignOrExtendSubscription 分配或续期订阅（用于兑换码等场景）
// 如果用户已有同分组的订阅：
//   - 未过期：从当前过期时间累加天数
//   - 已过期：从当前时间开始计算新的过期时间，并激活订阅
//
// 如果没有订阅：创建新订阅
func (s *SubscriptionService) AssignOrExtendSubscription(ctx context.Context, input *AssignSubscriptionInput) (*UserSubscription, bool, error) {
	// 检查订阅计划是否存在
	plan, err := s.planRepo.GetByID(ctx, input.PlanID)
	if err != nil {
		return nil, false, fmt.Errorf("plan not found: %w", err)
	}
	_ = plan // plan 存在即可

	// 查询是否已有订阅
	existingSub, err := s.userSubRepo.GetByUserIDAndPlanID(ctx, input.UserID, input.PlanID)
	if err != nil {
		// 不存在记录是正常情况，其他错误需要返回
		existingSub = nil
	}

	validityDays := input.ValidityDays
	if validityDays <= 0 {
		validityDays = 30
	}
	if validityDays > MaxValidityDays {
		validityDays = MaxValidityDays
	}

	// 已有订阅，执行续期（在事务中完成所有更新）
	if existingSub != nil {
		now := time.Now()
		var newExpiresAt time.Time

		if existingSub.ExpiresAt.After(now) {
			// 未过期：从当前过期时间累加
			newExpiresAt = existingSub.ExpiresAt.AddDate(0, 0, validityDays)
		} else {
			// 已过期：从当前时间开始计算
			newExpiresAt = now.AddDate(0, 0, validityDays)
		}

		// 确保不超过最大过期时间
		if newExpiresAt.After(MaxExpiresAt) {
			newExpiresAt = MaxExpiresAt
		}

		// 开启事务：ExtendExpiry + UpdateStatus + UpdateNotes 在同一事务中完成
		tx, err := s.entClient.Tx(ctx)
		if err != nil {
			return nil, false, fmt.Errorf("begin transaction: %w", err)
		}
		txCtx := dbent.NewTxContext(ctx, tx)

		// 更新过期时间
		if err := s.userSubRepo.ExtendExpiry(txCtx, existingSub.ID, newExpiresAt); err != nil {
			_ = tx.Rollback()
			return nil, false, fmt.Errorf("extend subscription: %w", err)
		}

		// 如果订阅已过期或被暂停，恢复为active状态
		if existingSub.Status != SubscriptionStatusActive {
			if err := s.userSubRepo.UpdateStatus(txCtx, existingSub.ID, SubscriptionStatusActive); err != nil {
				_ = tx.Rollback()
				return nil, false, fmt.Errorf("update subscription status: %w", err)
			}
		}

		// 追加备注
		if input.Notes != "" {
			newNotes := existingSub.Notes
			if newNotes != "" {
				newNotes += "\n"
			}
			newNotes += input.Notes
			if err := s.userSubRepo.UpdateNotes(txCtx, existingSub.ID, newNotes); err != nil {
				_ = tx.Rollback()
				return nil, false, fmt.Errorf("update subscription notes: %w", err)
			}
		}

		// 提交事务
		if err := tx.Commit(); err != nil {
			return nil, false, fmt.Errorf("commit transaction: %w", err)
		}

		// 失效订阅缓存
		s.InvalidateSubCache(input.UserID, input.PlanID)
		if s.billingCacheService != nil {
			userID, planID := input.UserID, input.PlanID
			go func() {
				cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = s.billingCacheService.InvalidateSubscription(cacheCtx, userID, planID)
			}()
		}

		// 返回更新后的订阅
		sub, err := s.userSubRepo.GetByID(ctx, existingSub.ID)
		return sub, true, err // true 表示是续期
	}

	// 没有订阅，创建新订阅
	sub, err := s.createSubscription(ctx, input)
	if err != nil {
		return nil, false, err
	}

	// 失效订阅缓存
	s.InvalidateSubCache(input.UserID, input.PlanID)
	if s.billingCacheService != nil {
		userID, planID := input.UserID, input.PlanID
		go func() {
			cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.billingCacheService.InvalidateSubscription(cacheCtx, userID, planID)
		}()
	}

	return sub, false, nil // false 表示是新建
}

// createSubscription 创建新订阅（内部方法）
func (s *SubscriptionService) createSubscription(ctx context.Context, input *AssignSubscriptionInput) (*UserSubscription, error) {
	validityDays := input.ValidityDays
	if validityDays <= 0 {
		validityDays = 30
	}
	if validityDays > MaxValidityDays {
		validityDays = MaxValidityDays
	}

	now := time.Now()
	expiresAt := now.AddDate(0, 0, validityDays)
	if expiresAt.After(MaxExpiresAt) {
		expiresAt = MaxExpiresAt
	}

	sub := &UserSubscription{
		UserID:     input.UserID,
		PlanID:     input.PlanID,
		StartsAt:   now,
		ExpiresAt:  expiresAt,
		Status:     SubscriptionStatusActive,
		AssignedAt: now,
		Notes:      input.Notes,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	// 只有当 AssignedBy > 0 时才设置（0 表示系统分配，如兑换码）
	if input.AssignedBy > 0 {
		sub.AssignedBy = &input.AssignedBy
	}

	if err := s.userSubRepo.Create(ctx, sub); err != nil {
		return nil, err
	}

	// 重新获取完整订阅信息（包含关联）
	return s.userSubRepo.GetByID(ctx, sub.ID)
}

// BulkAssignSubscriptionInput 批量分配订阅输入
type BulkAssignSubscriptionInput struct {
	UserIDs      []int64
	PlanID       int64
	ValidityDays int
	AssignedBy   int64
	Notes        string
}

// BulkAssignResult 批量分配结果
type BulkAssignResult struct {
	SuccessCount  int
	CreatedCount  int
	ReusedCount   int
	FailedCount   int
	Subscriptions []UserSubscription
	Errors        []string
	Statuses      map[int64]string
}

// BulkAssignSubscription 批量分配订阅
func (s *SubscriptionService) BulkAssignSubscription(ctx context.Context, input *BulkAssignSubscriptionInput) (*BulkAssignResult, error) {
	result := &BulkAssignResult{
		Subscriptions: make([]UserSubscription, 0),
		Errors:        make([]string, 0),
		Statuses:      make(map[int64]string),
	}

	for _, userID := range input.UserIDs {
		sub, reused, err := s.assignSubscriptionWithReuse(ctx, &AssignSubscriptionInput{
			UserID:       userID,
			PlanID:       input.PlanID,
			ValidityDays: input.ValidityDays,
			AssignedBy:   input.AssignedBy,
			Notes:        input.Notes,
		})
		if err != nil {
			result.FailedCount++
			result.Errors = append(result.Errors, fmt.Sprintf("user %d: %v", userID, err))
			result.Statuses[userID] = "failed"
		} else {
			result.SuccessCount++
			result.Subscriptions = append(result.Subscriptions, *sub)
			if reused {
				result.ReusedCount++
				result.Statuses[userID] = "reused"
			} else {
				result.CreatedCount++
				result.Statuses[userID] = "created"
			}
		}
	}

	return result, nil
}

func (s *SubscriptionService) assignSubscriptionWithReuse(ctx context.Context, input *AssignSubscriptionInput) (*UserSubscription, bool, error) {
	// 检查订阅计划是否存在
	_, err := s.planRepo.GetByID(ctx, input.PlanID)
	if err != nil {
		return nil, false, fmt.Errorf("plan not found: %w", err)
	}

	// 检查是否已存在订阅；若已存在，则按幂等成功返回现有订阅
	exists, err := s.userSubRepo.ExistsByUserIDAndPlanID(ctx, input.UserID, input.PlanID)
	if err != nil {
		return nil, false, err
	}
	if exists {
		sub, getErr := s.userSubRepo.GetByUserIDAndPlanID(ctx, input.UserID, input.PlanID)
		if getErr != nil {
			return nil, false, getErr
		}
		if conflictReason, conflict := detectAssignSemanticConflict(sub, input); conflict {
			return nil, false, ErrSubscriptionAssignConflict.WithMetadata(map[string]string{
				"conflict_reason": conflictReason,
			})
		}
		return sub, true, nil
	}

	sub, err := s.createSubscription(ctx, input)
	if err != nil {
		return nil, false, err
	}

	// 失效订阅缓存
	s.InvalidateSubCache(input.UserID, input.PlanID)
	if s.billingCacheService != nil {
		userID, planID := input.UserID, input.PlanID
		go func() {
			cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.billingCacheService.InvalidateSubscription(cacheCtx, userID, planID)
		}()
	}

	return sub, false, nil
}

func detectAssignSemanticConflict(existing *UserSubscription, input *AssignSubscriptionInput) (string, bool) {
	if existing == nil || input == nil {
		return "", false
	}

	normalizedDays := normalizeAssignValidityDays(input.ValidityDays)
	if !existing.StartsAt.IsZero() {
		expectedExpiresAt := existing.StartsAt.AddDate(0, 0, normalizedDays)
		if expectedExpiresAt.After(MaxExpiresAt) {
			expectedExpiresAt = MaxExpiresAt
		}
		if !existing.ExpiresAt.Equal(expectedExpiresAt) {
			return "validity_days_mismatch", true
		}
	}

	existingNotes := strings.TrimSpace(existing.Notes)
	inputNotes := strings.TrimSpace(input.Notes)
	if existingNotes != inputNotes {
		return "notes_mismatch", true
	}

	return "", false
}

func normalizeAssignValidityDays(days int) int {
	if days <= 0 {
		days = 30
	}
	if days > MaxValidityDays {
		days = MaxValidityDays
	}
	return days
}

// RevokeSubscription 撤销订阅
func (s *SubscriptionService) RevokeSubscription(ctx context.Context, subscriptionID int64) error {
	// 先获取订阅信息用于失效缓存
	sub, err := s.userSubRepo.GetByID(ctx, subscriptionID)
	if err != nil {
		return err
	}

	if err := s.userSubRepo.Delete(ctx, subscriptionID); err != nil {
		return err
	}

	// 失效订阅缓存
	s.InvalidateSubCache(sub.UserID, sub.PlanID)
	if s.billingCacheService != nil {
		userID, planID := sub.UserID, sub.PlanID
		go func() {
			cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.billingCacheService.InvalidateSubscription(cacheCtx, userID, planID)
		}()
	}

	return nil
}

// ExtendSubscription 调整订阅时长（正数延长，负数缩短）
func (s *SubscriptionService) ExtendSubscription(ctx context.Context, subscriptionID int64, days int) (*UserSubscription, error) {
	sub, err := s.userSubRepo.GetByID(ctx, subscriptionID)
	if err != nil {
		return nil, ErrSubscriptionNotFound
	}

	// 限制调整天数范围
	if days > MaxValidityDays {
		days = MaxValidityDays
	}
	if days < -MaxValidityDays {
		days = -MaxValidityDays
	}

	now := time.Now()
	isExpired := !sub.ExpiresAt.After(now)

	// 如果订阅已过期，不允许负向调整
	if isExpired && days < 0 {
		return nil, infraerrors.BadRequest("CANNOT_SHORTEN_EXPIRED", "cannot shorten an expired subscription")
	}

	// 计算新的过期时间
	var newExpiresAt time.Time
	if isExpired {
		// 已过期：从当前时间开始增加天数
		newExpiresAt = now.AddDate(0, 0, days)
	} else {
		// 未过期：从原过期时间增加/减少天数
		newExpiresAt = sub.ExpiresAt.AddDate(0, 0, days)
	}

	if newExpiresAt.After(MaxExpiresAt) {
		newExpiresAt = MaxExpiresAt
	}

	// 检查新的过期时间必须大于当前时间
	if !newExpiresAt.After(now) {
		return nil, ErrAdjustWouldExpire
	}

	if err := s.userSubRepo.ExtendExpiry(ctx, subscriptionID, newExpiresAt); err != nil {
		return nil, err
	}

	// 如果订阅已过期，恢复为active状态
	if sub.Status == SubscriptionStatusExpired {
		if err := s.userSubRepo.UpdateStatus(ctx, subscriptionID, SubscriptionStatusActive); err != nil {
			return nil, err
		}
	}

	// 失效订阅缓存
	s.InvalidateSubCache(sub.UserID, sub.PlanID)
	if s.billingCacheService != nil {
		userID, planID := sub.UserID, sub.PlanID
		go func() {
			cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.billingCacheService.InvalidateSubscription(cacheCtx, userID, planID)
		}()
	}

	return s.userSubRepo.GetByID(ctx, subscriptionID)
}

// GetByID 根据ID获取订阅
func (s *SubscriptionService) GetByID(ctx context.Context, id int64) (*UserSubscription, error) {
	return s.userSubRepo.GetByID(ctx, id)
}

// GetActiveSubscription 获取用户对特定计划的有效订阅
// 使用 L1 缓存 + singleflight 加速中间件热路径。
// 返回缓存对象的浅拷贝，调用方可安全修改字段而不会污染缓存或触发 data race。
func (s *SubscriptionService) GetActiveSubscription(ctx context.Context, userID, planID int64) (*UserSubscription, error) {
	key := subCacheKey(userID, planID)

	// L1 缓存命中：返回浅拷贝
	if s.subCacheL1 != nil {
		if v, ok := s.subCacheL1.Get(key); ok {
			if sub, ok := v.(*UserSubscription); ok {
				cp := *sub
				return &cp, nil
			}
		}
	}

	// singleflight 防止并发击穿
	value, err, _ := s.subCacheGroup.Do(key, func() (any, error) {
		sub, err := s.userSubRepo.GetActiveByUserIDAndPlanID(ctx, userID, planID)
		if err != nil {
			return nil, err // 直接透传 repo 已翻译的错误（NotFound → ErrSubscriptionNotFound，其他错误原样返回）
		}
		// 写入 L1 缓存
		if s.subCacheL1 != nil {
			_ = s.subCacheL1.SetWithTTL(key, sub, 1, s.jitteredTTL(s.subCacheTTL))
		}
		return sub, nil
	})
	if err != nil {
		return nil, err
	}
	// singleflight 返回的也是缓存指针，需要浅拷贝
	sub, ok := value.(*UserSubscription)
	if !ok || sub == nil {
		return nil, ErrSubscriptionNotFound
	}
	cp := *sub
	return &cp, nil
}

// ListUserSubscriptions 获取用户的所有订阅
func (s *SubscriptionService) ListUserSubscriptions(ctx context.Context, userID int64) ([]UserSubscription, error) {
	subs, err := s.userSubRepo.ListByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	normalizeExpiredWindows(subs)
	normalizeSubscriptionStatus(subs)
	return subs, nil
}

// ListActiveUserSubscriptions 获取用户的所有有效订阅
func (s *SubscriptionService) ListActiveUserSubscriptions(ctx context.Context, userID int64) ([]UserSubscription, error) {
	subs, err := s.userSubRepo.ListActiveByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	normalizeExpiredWindows(subs)
	return subs, nil
}

// ListPlanSubscriptions 获取计划的所有订阅
func (s *SubscriptionService) ListPlanSubscriptions(ctx context.Context, planID int64, page, pageSize int) ([]UserSubscription, *pagination.PaginationResult, error) {
	params := pagination.PaginationParams{Page: page, PageSize: pageSize}
	subs, pag, err := s.userSubRepo.ListByPlanID(ctx, planID, params)
	if err != nil {
		return nil, nil, err
	}
	normalizeExpiredWindows(subs)
	normalizeSubscriptionStatus(subs)
	return subs, pag, nil
}

// List 获取所有订阅（分页，支持筛选和排序）
func (s *SubscriptionService) List(ctx context.Context, page, pageSize int, userID, planID *int64, status, sortBy, sortOrder string) ([]UserSubscription, *pagination.PaginationResult, error) {
	params := pagination.PaginationParams{Page: page, PageSize: pageSize}
	subs, pag, err := s.userSubRepo.List(ctx, params, userID, planID, status, sortBy, sortOrder)
	if err != nil {
		return nil, nil, err
	}
	normalizeExpiredWindows(subs)
	normalizeSubscriptionStatus(subs)
	return subs, pag, nil
}

// normalizeExpiredWindows 将已过期窗口的用量清零，用于展示层。
// 只修改 usage 数字，不修改 window start，以保持维护逻辑的判断依据完整。
func normalizeExpiredWindows(subs []UserSubscription) {
	for i := range subs {
		sub := &subs[i]
		if sub.NeedsDailyReset() {
			sub.DailyUsageUSD = 0
		}
		if sub.NeedsWeeklyReset() {
			sub.WeeklyUsageUSD = 0
		}
		if sub.NeedsMonthlyReset() {
			sub.MonthlyUsageUSD = 0
		}
	}
}

// normalizeSubscriptionStatus 根据实际过期时间修正状态（仅影响返回数据，不影响数据库）
// 这确保前端显示正确的状态，即使定时任务尚未更新数据库
func normalizeSubscriptionStatus(subs []UserSubscription) {
	now := time.Now()
	for i := range subs {
		sub := &subs[i]
		if sub.Status == SubscriptionStatusActive && !sub.ExpiresAt.After(now) {
			sub.Status = SubscriptionStatusExpired
		}
	}
}

// startOfDay 返回给定时间所在日期的零点（保持原时区）
func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// CheckAndActivateWindow 激活尚未初始化的窗口（任意 window start 为 nil 时触发）。
// 只更新 nil 的字段，不覆写已激活字段，避免破坏现有窗口起始时间。
// 使用 ResetXxxUsage（usage=0, window_start=now）激活：对于从未激活的字段 usage 本就为 0，语义等价。
func (s *SubscriptionService) CheckAndActivateWindow(ctx context.Context, sub *UserSubscription) error {
	if !sub.NeedsWindowActivation() {
		return nil
	}

	windowStart := startOfDay(time.Now())
	if sub.DailyWindowStart == nil {
		if err := s.userSubRepo.ResetDailyUsage(ctx, sub.ID, windowStart); err != nil {
			return fmt.Errorf("activate daily window: %w", err)
		}
	}
	if sub.WeeklyWindowStart == nil {
		if err := s.userSubRepo.ResetWeeklyUsage(ctx, sub.ID, windowStart); err != nil {
			return fmt.Errorf("activate weekly window: %w", err)
		}
	}
	if sub.MonthlyWindowStart == nil {
		if err := s.userSubRepo.ResetMonthlyUsage(ctx, sub.ID, windowStart); err != nil {
			return fmt.Errorf("activate monthly window: %w", err)
		}
	}
	return nil
}

// AdminResetQuota manually resets the daily, weekly, and/or monthly usage windows.
// Uses startOfDay(now) as the new window start, matching automatic resets.
func (s *SubscriptionService) AdminResetQuota(ctx context.Context, subscriptionID int64, resetDaily, resetWeekly, resetMonthly bool) (*UserSubscription, error) {
	if !resetDaily && !resetWeekly && !resetMonthly {
		return nil, ErrInvalidInput
	}
	sub, err := s.userSubRepo.GetByID(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	windowStart := startOfDay(time.Now())
	if resetDaily {
		if err := s.userSubRepo.ResetDailyUsage(ctx, sub.ID, windowStart); err != nil {
			return nil, err
		}
	}
	if resetWeekly {
		if err := s.userSubRepo.ResetWeeklyUsage(ctx, sub.ID, windowStart); err != nil {
			return nil, err
		}
	}
	if resetMonthly {
		if err := s.userSubRepo.ResetMonthlyUsage(ctx, sub.ID, windowStart); err != nil {
			return nil, err
		}
	}
	// Invalidate L1 ristretto cache. Ristretto's Del() is asynchronous by design,
	// so call Wait() immediately after to flush pending operations and guarantee
	// the deleted key is not returned on the very next Get() call.
	s.InvalidateSubCache(sub.UserID, sub.PlanID)
	if s.subCacheL1 != nil {
		s.subCacheL1.Wait()
	}
	if s.billingCacheService != nil {
		_ = s.billingCacheService.InvalidateSubscription(ctx, sub.UserID, sub.PlanID)
	}
	// Return the refreshed subscription from DB
	return s.userSubRepo.GetByID(ctx, subscriptionID)
}

// CheckAndResetWindows 检查并重置过期的窗口
func (s *SubscriptionService) CheckAndResetWindows(ctx context.Context, sub *UserSubscription) error {
	// 使用当天零点作为新窗口起始时间
	windowStart := startOfDay(time.Now())
	needsInvalidateCache := false

	// 日窗口重置（24小时）
	if sub.NeedsDailyReset() {
		if err := s.userSubRepo.ResetDailyUsage(ctx, sub.ID, windowStart); err != nil {
			return err
		}
		sub.DailyWindowStart = &windowStart
		sub.DailyUsageUSD = 0
		needsInvalidateCache = true
	}

	// 周窗口重置（7天）
	if sub.NeedsWeeklyReset() {
		if err := s.userSubRepo.ResetWeeklyUsage(ctx, sub.ID, windowStart); err != nil {
			return err
		}
		sub.WeeklyWindowStart = &windowStart
		sub.WeeklyUsageUSD = 0
		needsInvalidateCache = true
	}

	// 月窗口重置（30天）
	if sub.NeedsMonthlyReset() {
		if err := s.userSubRepo.ResetMonthlyUsage(ctx, sub.ID, windowStart); err != nil {
			return err
		}
		sub.MonthlyWindowStart = &windowStart
		sub.MonthlyUsageUSD = 0
		needsInvalidateCache = true
	}

	// 如果有窗口被重置，失效缓存以保持一致性
	if needsInvalidateCache {
		s.InvalidateSubCache(sub.UserID, sub.PlanID)
		if s.billingCacheService != nil {
			_ = s.billingCacheService.InvalidateSubscription(ctx, sub.UserID, sub.PlanID)
		}
	}

	return nil
}

// CheckUsageLimits 检查使用限额（返回错误如果超限）
// 用于中间件的快速预检查，additionalCost 通常为 0
func (s *SubscriptionService) CheckUsageLimits(ctx context.Context, sub *UserSubscription, plan *SubscriptionPlan, additionalCost float64) error {
	if plan == nil {
		return ErrPlanNotFound
	}
	if !sub.CheckDailyLimit(plan, additionalCost) {
		return ErrDailyLimitExceeded
	}
	if !sub.CheckWeeklyLimit(plan, additionalCost) {
		return ErrWeeklyLimitExceeded
	}
	if !sub.CheckMonthlyLimit(plan, additionalCost) {
		return ErrMonthlyLimitExceeded
	}
	return nil
}

// ValidateAndCheckLimits 合并验证+限额检查（中间件热路径专用）
// 仅做内存检查，不触发 DB 写入。窗口重置的 DB 写入由 DoWindowMaintenance 异步完成。
// 返回 needsMaintenance 表示是否需要异步执行窗口维护。
func (s *SubscriptionService) ValidateAndCheckLimits(sub *UserSubscription, plan *SubscriptionPlan) (needsMaintenance bool, err error) {
	if plan == nil {
		return false, ErrPlanNotFound
	}

	// 1. 验证订阅状态
	if sub.Status == SubscriptionStatusExpired {
		return false, ErrSubscriptionExpired
	}
	if sub.Status == SubscriptionStatusSuspended {
		return false, ErrSubscriptionSuspended
	}
	if sub.IsExpired() {
		return false, ErrSubscriptionExpired
	}

	// 2. 内存中修正过期窗口的用量，确保 CheckUsageLimits 不会误拒绝用户
	//    实际的 DB 窗口重置由 DoWindowMaintenance 异步完成
	if sub.NeedsDailyReset() {
		sub.DailyUsageUSD = 0
		needsMaintenance = true
	}
	if sub.NeedsWeeklyReset() {
		sub.WeeklyUsageUSD = 0
		needsMaintenance = true
	}
	if sub.NeedsMonthlyReset() {
		sub.MonthlyUsageUSD = 0
		needsMaintenance = true
	}
	if sub.NeedsWindowActivation() {
		needsMaintenance = true
	}

	// 3. 检查用量限额
	if !sub.CheckDailyLimit(plan, 0) {
		return needsMaintenance, ErrDailyLimitExceeded
	}
	if !sub.CheckWeeklyLimit(plan, 0) {
		return needsMaintenance, ErrWeeklyLimitExceeded
	}
	if !sub.CheckMonthlyLimit(plan, 0) {
		return needsMaintenance, ErrMonthlyLimitExceeded
	}

	return needsMaintenance, nil
}

// DoWindowMaintenance 异步执行窗口维护（激活+重置）
// 使用独立 context，不受请求取消影响。
// 注意：此方法仅在 ValidateAndCheckLimits 返回 needsMaintenance=true 时调用，
// 而 IsExpired()=true 的订阅在 ValidateAndCheckLimits 中已被拦截返回错误，
// 因此进入此方法的订阅一定未过期，无需处理过期状态同步。
func (s *SubscriptionService) DoWindowMaintenance(sub *UserSubscription) {
	if s == nil {
		return
	}
	if s.maintenanceQueue != nil {
		// 异步模式：值拷贝传入 closure，worker goroutine 拥有独立数据副本，不依赖调用方生命周期
		subCopy := *sub
		if err := s.maintenanceQueue.TryEnqueue(func() {
			s.doWindowMaintenance(&subCopy)
		}); err != nil {
			slog.Warn("subscription maintenance enqueue failed", "subscription_id", sub.ID, "error", err)
		}
		return
	}

	s.doWindowMaintenance(sub)
}

func (s *SubscriptionService) doWindowMaintenance(sub *UserSubscription) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 激活和重置独立运行，不互斥：
	// 一个订阅可能同时需要激活某些 window（nil）和重置另一些 window（过期）。
	// 例如：管理员给已有订阅的计划新增了 weekly limit，则 weekly_window_start=nil
	// 而 daily_window_start 已激活且可能需要重置。
	if sub.NeedsWindowActivation() {
		if err := s.CheckAndActivateWindow(ctx, sub); err != nil {
			slog.Error("failed to activate subscription windows", "subscription_id", sub.ID, "error", err)
		}
	}
	if err := s.CheckAndResetWindows(ctx, sub); err != nil {
		slog.Error("failed to reset subscription windows", "subscription_id", sub.ID, "error", err)
	}

	// 失效 L1 缓存，确保后续请求拿到更新后的数据
	s.InvalidateSubCache(sub.UserID, sub.PlanID)
}

// RecordUsage 记录使用量到订阅
func (s *SubscriptionService) RecordUsage(ctx context.Context, subscriptionID int64, costUSD float64) error {
	return s.userSubRepo.IncrementUsage(ctx, subscriptionID, costUSD)
}

// SubscriptionProgress 订阅进度
type SubscriptionProgress struct {
	ID            int64                `json:"id"`
	GroupName     string               `json:"group_name"`
	ExpiresAt     time.Time            `json:"expires_at"`
	ExpiresInDays int                  `json:"expires_in_days"`
	Daily         *UsageWindowProgress `json:"daily,omitempty"`
	Weekly        *UsageWindowProgress `json:"weekly,omitempty"`
	Monthly       *UsageWindowProgress `json:"monthly,omitempty"`
}

// UsageWindowProgress 使用窗口进度
type UsageWindowProgress struct {
	LimitUSD        float64   `json:"limit_usd"`
	UsedUSD         float64   `json:"used_usd"`
	RemainingUSD    float64   `json:"remaining_usd"`
	Percentage      float64   `json:"percentage"`
	WindowStart     time.Time `json:"window_start"`
	ResetsAt        time.Time `json:"resets_at"`
	ResetsInSeconds int64     `json:"resets_in_seconds"`
}

// GetSubscriptionProgress 获取订阅使用进度
func (s *SubscriptionService) GetSubscriptionProgress(ctx context.Context, subscriptionID int64) (*SubscriptionProgress, error) {
	sub, err := s.userSubRepo.GetByID(ctx, subscriptionID)
	if err != nil {
		return nil, ErrSubscriptionNotFound
	}

	plan := sub.Plan
	if plan == nil {
		plan, err = s.planRepo.GetByID(ctx, sub.PlanID)
		if err != nil {
			return nil, err
		}
	}

	return s.calculateProgress(sub, plan), nil
}

// calculateProgress 根据已加载的订阅和计划数据计算使用进度（纯内存计算，无 DB 查询）
func (s *SubscriptionService) calculateProgress(sub *UserSubscription, plan *SubscriptionPlan) *SubscriptionProgress {
	progress := &SubscriptionProgress{
		ID:            sub.ID,
		GroupName:     plan.Name,
		ExpiresAt:     sub.ExpiresAt,
		ExpiresInDays: sub.DaysRemaining(),
	}

	// 日进度
	if plan.HasDailyLimit() && sub.DailyWindowStart != nil {
		limit := *plan.DailyLimitUSD
		resetsAt := sub.DailyWindowStart.Add(24 * time.Hour)
		progress.Daily = &UsageWindowProgress{
			LimitUSD:        limit,
			UsedUSD:         sub.DailyUsageUSD,
			RemainingUSD:    limit - sub.DailyUsageUSD,
			Percentage:      (sub.DailyUsageUSD / limit) * 100,
			WindowStart:     *sub.DailyWindowStart,
			ResetsAt:        resetsAt,
			ResetsInSeconds: int64(time.Until(resetsAt).Seconds()),
		}
		if progress.Daily.RemainingUSD < 0 {
			progress.Daily.RemainingUSD = 0
		}
		if progress.Daily.Percentage > 100 {
			progress.Daily.Percentage = 100
		}
		if progress.Daily.ResetsInSeconds < 0 {
			progress.Daily.ResetsInSeconds = 0
		}
	}

	// 周进度
	if plan.HasWeeklyLimit() && sub.WeeklyWindowStart != nil {
		limit := *plan.WeeklyLimitUSD
		resetsAt := sub.WeeklyWindowStart.Add(7 * 24 * time.Hour)
		progress.Weekly = &UsageWindowProgress{
			LimitUSD:        limit,
			UsedUSD:         sub.WeeklyUsageUSD,
			RemainingUSD:    limit - sub.WeeklyUsageUSD,
			Percentage:      (sub.WeeklyUsageUSD / limit) * 100,
			WindowStart:     *sub.WeeklyWindowStart,
			ResetsAt:        resetsAt,
			ResetsInSeconds: int64(time.Until(resetsAt).Seconds()),
		}
		if progress.Weekly.RemainingUSD < 0 {
			progress.Weekly.RemainingUSD = 0
		}
		if progress.Weekly.Percentage > 100 {
			progress.Weekly.Percentage = 100
		}
		if progress.Weekly.ResetsInSeconds < 0 {
			progress.Weekly.ResetsInSeconds = 0
		}
	}

	// 月进度
	if plan.HasMonthlyLimit() && sub.MonthlyWindowStart != nil {
		limit := *plan.MonthlyLimitUSD
		resetsAt := sub.MonthlyWindowStart.Add(30 * 24 * time.Hour)
		progress.Monthly = &UsageWindowProgress{
			LimitUSD:        limit,
			UsedUSD:         sub.MonthlyUsageUSD,
			RemainingUSD:    limit - sub.MonthlyUsageUSD,
			Percentage:      (sub.MonthlyUsageUSD / limit) * 100,
			WindowStart:     *sub.MonthlyWindowStart,
			ResetsAt:        resetsAt,
			ResetsInSeconds: int64(time.Until(resetsAt).Seconds()),
		}
		if progress.Monthly.RemainingUSD < 0 {
			progress.Monthly.RemainingUSD = 0
		}
		if progress.Monthly.Percentage > 100 {
			progress.Monthly.Percentage = 100
		}
		if progress.Monthly.ResetsInSeconds < 0 {
			progress.Monthly.ResetsInSeconds = 0
		}
	}

	return progress
}

// GetUserSubscriptionsWithProgress 获取用户所有订阅及进度
func (s *SubscriptionService) GetUserSubscriptionsWithProgress(ctx context.Context, userID int64) ([]SubscriptionProgress, error) {
	// ListActiveByUserID 已使用 .WithPlan() eager-load Plan 关联，1 次查询获取所有数据
	subs, err := s.userSubRepo.ListActiveByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	progresses := make([]SubscriptionProgress, 0, len(subs))
	for i := range subs {
		sub := &subs[i]
		plan := sub.Plan
		if plan == nil {
			continue
		}
		progresses = append(progresses, *s.calculateProgress(sub, plan))
	}

	return progresses, nil
}

// ValidateSubscription 验证订阅是否有效
func (s *SubscriptionService) ValidateSubscription(ctx context.Context, sub *UserSubscription) error {
	if sub.Status == SubscriptionStatusExpired {
		return ErrSubscriptionExpired
	}
	if sub.Status == SubscriptionStatusSuspended {
		return ErrSubscriptionSuspended
	}
	if sub.IsExpired() {
		// 更新状态
		_ = s.userSubRepo.UpdateStatus(ctx, sub.ID, SubscriptionStatusExpired)
		return ErrSubscriptionExpired
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────
// 多订阅合并状态
// ─────────────────────────────────────────────────────────────────

// MergedSubscriptionState 多订阅合并后的状态
type MergedSubscriptionState struct {
	// 合并后的有效限额（所有活跃订阅的 plan 限额之和）
	EffectiveDailyLimit   *float64
	EffectiveWeeklyLimit  *float64
	EffectiveMonthlyLimit *float64

	// 合并后的总用量（所有活跃订阅用量之和）
	TotalDailyUsage   float64
	TotalWeeklyUsage  float64
	TotalMonthlyUsage float64

	// FIFOQueue：按过期时间升序排列的活跃订阅列表。
	// 扣费时从队首开始依次填满，溢出到下一个。
	// 同时用于窗口维护遍历（替代原 ActiveSubscriptions）。
	// FIFOQueue[0] 是最早过期的订阅，等价于原 FIFOTarget。
	FIFOQueue []UserSubscription

	// 是否需要窗口维护
	NeedsMaintenance bool
}

// FIFOTarget 返回第一个未过期的活跃订阅，如果队列为空或全部过期则返回 nil。
// 用于需要单个"代表性订阅"的场景（状态检查、展示）。
func (s *MergedSubscriptionState) FIFOTarget() *UserSubscription {
	now := time.Now()
	for i := range s.FIFOQueue {
		if s.FIFOQueue[i].ExpiresAt.After(now) {
			return &s.FIFOQueue[i]
		}
	}
	return nil
}

// ActivePlanIDs 返回队列中未过期订阅对应的 plan ID 集合（去重）。
// 用于分组 subscriber 可见性判断（与计费解耦：只关心订阅是否有效，不关心是否超限）。
func (s *MergedSubscriptionState) ActivePlanIDs() []int64 {
	if s == nil {
		return nil
	}
	now := time.Now()
	seen := make(map[int64]struct{}, len(s.FIFOQueue))
	planIDs := make([]int64, 0, len(s.FIFOQueue))
	for i := range s.FIFOQueue {
		if !s.FIFOQueue[i].ExpiresAt.After(now) {
			continue
		}
		pid := s.FIFOQueue[i].PlanID
		if _, ok := seen[pid]; ok {
			continue
		}
		seen[pid] = struct{}{}
		planIDs = append(planIDs, pid)
	}
	return planIDs
}

// recalcUsage 重新计算合并用量（窗口重置后调用）
func (s *MergedSubscriptionState) recalcUsage() {
	s.TotalDailyUsage = 0
	s.TotalWeeklyUsage = 0
	s.TotalMonthlyUsage = 0
	for i := range s.FIFOQueue {
		sub := &s.FIFOQueue[i]
		s.TotalDailyUsage += sub.DailyUsageUSD
		s.TotalWeeklyUsage += sub.WeeklyUsageUSD
		s.TotalMonthlyUsage += sub.MonthlyUsageUSD
	}
}

// mergeSubscriptions 合并多个活跃订阅为统一状态
func mergeSubscriptions(subs []UserSubscription) *MergedSubscriptionState {
	state := &MergedSubscriptionState{}

	// 用于跟踪是否任意 plan 的某个窗口没有限制（nil → 合并后也无限制）
	dailyUnlimited := false
	weeklyUnlimited := false
	monthlyUnlimited := false

	// 按过期时间升序排列，构建 FIFO 扣费队列
	sorted := make([]UserSubscription, 0, len(subs))
	for i := range subs {
		if subs[i].Plan != nil && subs[i].IsActive() {
			sorted = append(sorted, subs[i])
		}
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ExpiresAt.Before(sorted[j].ExpiresAt)
	})
	state.FIFOQueue = sorted

	// 聚合限额和用量（顺序无关，遍历 FIFOQueue）
	for i := range state.FIFOQueue {
		sub := &state.FIFOQueue[i]
		plan := sub.Plan

		if !dailyUnlimited {
			if plan.HasDailyLimit() {
				if state.EffectiveDailyLimit == nil {
					zero := 0.0
					state.EffectiveDailyLimit = &zero
				}
				*state.EffectiveDailyLimit += *plan.DailyLimitUSD
			} else {
				dailyUnlimited = true
				state.EffectiveDailyLimit = nil
			}
		}
		if !weeklyUnlimited {
			if plan.HasWeeklyLimit() {
				if state.EffectiveWeeklyLimit == nil {
					zero := 0.0
					state.EffectiveWeeklyLimit = &zero
				}
				*state.EffectiveWeeklyLimit += *plan.WeeklyLimitUSD
			} else {
				weeklyUnlimited = true
				state.EffectiveWeeklyLimit = nil
			}
		}
		if !monthlyUnlimited {
			if plan.HasMonthlyLimit() {
				if state.EffectiveMonthlyLimit == nil {
					zero := 0.0
					state.EffectiveMonthlyLimit = &zero
				}
				*state.EffectiveMonthlyLimit += *plan.MonthlyLimitUSD
			} else {
				monthlyUnlimited = true
				state.EffectiveMonthlyLimit = nil
			}
		}

		state.TotalDailyUsage += sub.DailyUsageUSD
		state.TotalWeeklyUsage += sub.WeeklyUsageUSD
		state.TotalMonthlyUsage += sub.MonthlyUsageUSD
	}

	return state
}

// GetMergedSubscriptionState 获取用户的合并订阅状态
// 使用 L1 缓存 + singleflight 加速中间件热路径。
func (s *SubscriptionService) GetMergedSubscriptionState(ctx context.Context, userID int64) (*MergedSubscriptionState, error) {
	key := mergedSubCacheKey(userID)

	// L1 缓存命中：返回深拷贝
	if s.subCacheL1 != nil {
		if v, ok := s.subCacheL1.Get(key); ok {
			if state, ok := v.(*MergedSubscriptionState); ok {
				return s.copyMergedState(state), nil
			}
		}
	}

	// singleflight 防止并发击穿
	value, err, _ := s.subCacheGroup.Do(key, func() (any, error) {
		subs, err := s.userSubRepo.ListActiveByUserID(ctx, userID)
		if err != nil {
			return nil, err
		}
		if len(subs) == 0 {
			return nil, ErrSubscriptionNotFound
		}

		// 规范化过期窗口
		normalizeExpiredWindows(subs)

		state := mergeSubscriptions(subs)

		// 写入 L1 缓存
		if s.subCacheL1 != nil {
			_ = s.subCacheL1.SetWithTTL(key, state, 1, s.jitteredTTL(s.subCacheTTL))
		}
		return state, nil
	})
	if err != nil {
		return nil, err
	}

	state, ok := value.(*MergedSubscriptionState)
	if !ok || state == nil {
		return nil, ErrSubscriptionNotFound
	}
	return s.copyMergedState(state), nil
}

// copyMergedState 深拷贝合并状态，避免缓存污染
func (s *SubscriptionService) copyMergedState(src *MergedSubscriptionState) *MergedSubscriptionState {
	dst := &MergedSubscriptionState{
		TotalDailyUsage:   src.TotalDailyUsage,
		TotalWeeklyUsage:  src.TotalWeeklyUsage,
		TotalMonthlyUsage: src.TotalMonthlyUsage,
		NeedsMaintenance:  src.NeedsMaintenance,
	}
	if src.EffectiveDailyLimit != nil {
		v := *src.EffectiveDailyLimit
		dst.EffectiveDailyLimit = &v
	}
	if src.EffectiveWeeklyLimit != nil {
		v := *src.EffectiveWeeklyLimit
		dst.EffectiveWeeklyLimit = &v
	}
	if src.EffectiveMonthlyLimit != nil {
		v := *src.EffectiveMonthlyLimit
		dst.EffectiveMonthlyLimit = &v
	}
	if len(src.FIFOQueue) > 0 {
		now := time.Now()
		dst.FIFOQueue = make([]UserSubscription, 0, len(src.FIFOQueue))
		for _, sub := range src.FIFOQueue {
			if sub.ExpiresAt.After(now) {
				dst.FIFOQueue = append(dst.FIFOQueue, sub)
			}
		}
	}
	return dst
}

// ValidateMergedState 合并状态验证+限额检查（中间件热路径专用）
// 仅做内存检查，不触发 DB 写入。窗口重置的 DB 写入由 DoWindowMaintenance 异步完成。
// 返回 needsMaintenance 表示是否需要异步执行窗口维护。
func (s *SubscriptionService) ValidateMergedState(state *MergedSubscriptionState) (needsMaintenance bool, err error) {
	if state == nil {
		return false, ErrSubscriptionNotFound
	}
	target := state.FIFOTarget()
	if target == nil {
		return false, ErrSubscriptionNotFound
	}

	// 检查队首订阅状态（最早过期的）
	if target.Status == SubscriptionStatusExpired || target.IsExpired() {
		return false, ErrSubscriptionExpired
	}
	if target.Status == SubscriptionStatusSuspended {
		return false, ErrSubscriptionSuspended
	}

	// 窗口维护判断（对每个活跃订阅）
	for i := range state.FIFOQueue {
		sub := &state.FIFOQueue[i]
		if sub.NeedsDailyReset() {
			sub.DailyUsageUSD = 0
			needsMaintenance = true
		}
		if sub.NeedsWeeklyReset() {
			sub.WeeklyUsageUSD = 0
			needsMaintenance = true
		}
		if sub.NeedsMonthlyReset() {
			sub.MonthlyUsageUSD = 0
			needsMaintenance = true
		}
		if sub.NeedsWindowActivation() {
			needsMaintenance = true
		}
	}

	// 重新计算合并用量（窗口重置后）
	state.recalcUsage()

	// 检查合并限额
	if state.EffectiveDailyLimit != nil && state.TotalDailyUsage >= *state.EffectiveDailyLimit {
		return needsMaintenance, ErrDailyLimitExceeded
	}
	if state.EffectiveWeeklyLimit != nil && state.TotalWeeklyUsage >= *state.EffectiveWeeklyLimit {
		return needsMaintenance, ErrWeeklyLimitExceeded
	}
	if state.EffectiveMonthlyLimit != nil && state.TotalMonthlyUsage >= *state.EffectiveMonthlyLimit {
		return needsMaintenance, ErrMonthlyLimitExceeded
	}

	return needsMaintenance, nil
}

// MergedStateFIFOQueue 安全地从合并状态中取 FIFOQueue，nil 时返回空切片。
func MergedStateFIFOQueue(state *MergedSubscriptionState) []UserSubscription {
	if state == nil {
		return nil
	}
	return state.FIFOQueue
}
