package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// 迁移到 metask-ai：用户登录后点「迁移」，本站在一个事务里记下余额 / 订阅 / Key 的快照、
// 清零余额、停用 Key、终止订阅（冻结），再服务端对服务端调 metask-ai 入账；入账成功后停用账号。
// 先冻结再入账：中途失败时用户暂时两边都没有额度，但不会两边都有；入账接口按用户 id 幂等，失败自动重试。

const (
	MetaskMigrationStatusFrozen    = "frozen"
	MetaskMigrationStatusCompleted = "completed"
)

var (
	ErrMetaskMigrationDisabled = infraerrors.Forbidden("MIGRATION_DISABLED", "迁移尚未开放")
	ErrMetaskMigrationAdmin    = infraerrors.Forbidden("MIGRATION_ADMIN_NOT_ALLOWED", "管理员账号不能迁移")
	ErrMetaskMigrationPending  = infraerrors.ServiceUnavailable("MIGRATION_PENDING", "余额已冻结，新站入账暂未完成，系统会自动重试，请稍后刷新")
	// ErrMetaskMigrationClosed 迁移开放期间，本站不再接受充值、兑换与订阅购买。
	ErrMetaskMigrationClosed = infraerrors.Forbidden("MIGRATION_TOPUP_CLOSED", "本站已停止充值、兑换与订阅购买，请迁移到新站后在新站操作")
)

// MetaskMigrationSubscription 生效订阅的快照。MonthlyLimitMU 为 nil 表示套餐没有月上限。
type MetaskMigrationSubscription struct {
	PlanName       string    `json:"plan_name,omitempty"`
	MonthlyLimitMU *int64    `json:"monthly_limit_mu"`
	ExpiresAt      time.Time `json:"expires_at"`
}

// MetaskMigrationKey 只带 sha256，不带明文（metask-ai 网关按哈希鉴权）。
type MetaskMigrationKey struct {
	KeyHash    string     `json:"key_hash"`
	KeyPrefix  string     `json:"key_prefix"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

// MetaskMigrationPayload 发给 metask-ai 的请求体（POST /api/internal/migrations/sub2api）。
// 金额单位 milli-U（1 U = 1000）。
type MetaskMigrationPayload struct {
	Sub2APIID          int64                         `json:"sub2api_id"`
	Email              string                        `json:"email"`
	PasswordHash       string                        `json:"password_hash,omitempty"`
	Username           string                        `json:"username,omitempty"`
	Phone              string                        `json:"phone,omitempty"`
	CreatedAt          time.Time                     `json:"created_at"`
	BalanceMU          int64                         `json:"balance_mu"`
	SubscriptionUsedMU int64                         `json:"subscription_used_mu"`
	Subscriptions      []MetaskMigrationSubscription `json:"subscriptions"`
	Keys               []MetaskMigrationKey          `json:"keys"`
}

// MetaskMigrationRecord 一条迁移记录。
type MetaskMigrationRecord struct {
	UserID      int64
	Status      string
	Payload     MetaskMigrationPayload
	Result      json.RawMessage
	Attempts    int
	LastError   string
	CreatedAt   time.Time
	CompletedAt *time.Time
}

// MetaskMigrationFreezeResult 冻结事务的结果。Created=false 表示之前已冻结过（重复点击）。
type MetaskMigrationFreezeResult struct {
	Record  *MetaskMigrationRecord
	Created bool
	// RawKeys 本次停用的 Key 明文，只用于失效鉴权缓存，不落库、不外发。
	RawKeys []string
	// PlanIDs 本次终止的订阅所属套餐，用于失效订阅缓存。
	PlanIDs []int64
}

// MetaskMigrationPreview 点击前给用户看的数字（不冻结）。
type MetaskMigrationPreview struct {
	Role          string
	Status        string
	BalanceMU     int64
	Subscriptions []MetaskMigrationSubscription
	KeyCount      int
}

type MetaskMigrationRepository interface {
	// Preview 读用户当前的余额、生效订阅与可用 Key 数。
	Preview(ctx context.Context, userID int64) (*MetaskMigrationPreview, error)
	// Get 没有记录时返回 (nil, nil)。
	Get(ctx context.Context, userID int64) (*MetaskMigrationRecord, error)
	// Freeze 在一个事务里锁用户行、记快照、清零余额、停用 Key、终止订阅并写迁移记录；已有记录时原样返回。
	Freeze(ctx context.Context, userID int64, usageSince time.Time) (*MetaskMigrationFreezeResult, error)
	// MarkCompleted 记入账结果并停用账号。
	MarkCompleted(ctx context.Context, userID int64, result json.RawMessage) error
	MarkFailed(ctx context.Context, userID int64, errMsg string) error
	// ListPending 列出还没入账成功的记录（最近一次尝试早于 olderThan）。
	ListPending(ctx context.Context, olderThan time.Duration, limit int) ([]int64, error)
}

// MetaskMigrationClient 调 metask-ai 的入账接口；同一 sub2api_id 重复调用返回第一次的结果。
type MetaskMigrationClient interface {
	Apply(ctx context.Context, baseURL, token string, payload *MetaskMigrationPayload) (json.RawMessage, error)
}

// MetaskMigrationStatus 给前端的状态。
type MetaskMigrationStatus struct {
	Enabled        bool                          `json:"enabled"`
	Eligible       bool                          `json:"eligible"`
	Reason         string                        `json:"reason,omitempty"`
	Status         string                        `json:"status"` // none | frozen | completed
	SiteURL        string                        `json:"site_url"`
	GatewayBaseURL string                        `json:"gateway_base_url"`
	Deadline       string                        `json:"deadline,omitempty"`
	BalanceMU      int64                         `json:"balance_mu"`
	Subscriptions  []MetaskMigrationSubscription `json:"subscriptions"`
	KeyCount       int                           `json:"key_count"`
	Result         json.RawMessage               `json:"result,omitempty"`
}

type MetaskMigrationService struct {
	repo           MetaskMigrationRepository
	client         MetaskMigrationClient
	cfg            *config.Config
	userService    *UserService
	authCache      APIKeyAuthCacheInvalidator
	billingCache   *BillingCacheService
	subService     *SubscriptionService
	retryStartOnce sync.Once
}

func NewMetaskMigrationService(
	repo MetaskMigrationRepository,
	client MetaskMigrationClient,
	cfg *config.Config,
	userService *UserService,
	authCache APIKeyAuthCacheInvalidator,
	billingCache *BillingCacheService,
	subService *SubscriptionService,
) *MetaskMigrationService {
	return &MetaskMigrationService{
		repo: repo, client: client, cfg: cfg, userService: userService,
		authCache: authCache, billingCache: billingCache, subService: subService,
	}
}

func (s *MetaskMigrationService) conf() config.MetaskMigrationConfig {
	if s == nil || s.cfg == nil {
		return config.MetaskMigrationConfig{}
	}
	return s.cfg.MetaskMigration
}

// Enabled 迁移是否开放（配置了开关、地址与令牌）。
func (s *MetaskMigrationService) Enabled() bool {
	return s.conf().Active()
}

func (s *MetaskMigrationService) GetStatus(ctx context.Context, userID int64) (*MetaskMigrationStatus, error) {
	c := s.conf()
	siteURL := strings.TrimSpace(c.SiteURL)
	if siteURL == "" {
		siteURL = strings.TrimSpace(c.BaseURL)
	}
	out := &MetaskMigrationStatus{
		Enabled:        s.Enabled(),
		Status:         "none",
		SiteURL:        strings.TrimRight(siteURL, "/"),
		GatewayBaseURL: strings.TrimRight(c.GatewayBaseURL, "/"),
		Deadline:       c.Deadline,
		Subscriptions:  []MetaskMigrationSubscription{},
	}
	rec, err := s.repo.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	if rec != nil {
		out.Status = rec.Status
		out.BalanceMU = rec.Payload.BalanceMU
		if rec.Payload.Subscriptions != nil {
			out.Subscriptions = rec.Payload.Subscriptions
		}
		out.KeyCount = len(rec.Payload.Keys)
		out.Result = rec.Result
		return out, nil
	}
	if !out.Enabled {
		out.Reason = "disabled"
		return out, nil
	}
	pv, err := s.repo.Preview(ctx, userID)
	if err != nil {
		return nil, err
	}
	out.BalanceMU = pv.BalanceMU
	if pv.Subscriptions != nil {
		out.Subscriptions = pv.Subscriptions
	}
	out.KeyCount = pv.KeyCount
	if pv.Role == RoleAdmin {
		out.Reason = "admin"
		return out, nil
	}
	out.Eligible = true
	return out, nil
}

// Migrate 用户确认迁移。可重复调用：已冻结未入账的会再试一次入账，已完成的返回原结果。
func (s *MetaskMigrationService) Migrate(ctx context.Context, userID int64) (*MetaskMigrationStatus, error) {
	if !s.Enabled() {
		return nil, ErrMetaskMigrationDisabled
	}
	fr, err := s.repo.Freeze(ctx, userID, s.conf().UsageSinceTime())
	if err != nil {
		return nil, err
	}
	if fr.Created {
		s.invalidateAfterFreeze(ctx, userID, fr)
	}
	if fr.Record.Status != MetaskMigrationStatusCompleted {
		if err := s.apply(ctx, fr.Record); err != nil {
			logger.LegacyPrintf("service.metask_migration", "[Migrate] apply failed: user=%d err=%v", userID, err)
			return nil, ErrMetaskMigrationPending
		}
	}
	return s.GetStatus(ctx, userID)
}

func (s *MetaskMigrationService) apply(ctx context.Context, rec *MetaskMigrationRecord) error {
	c := s.conf()
	result, err := s.client.Apply(ctx, c.BaseURL, c.Token, &rec.Payload)
	if err != nil {
		if markErr := s.repo.MarkFailed(ctx, rec.UserID, err.Error()); markErr != nil {
			logger.LegacyPrintf("service.metask_migration", "[apply] mark failed error: user=%d err=%v", rec.UserID, markErr)
		}
		return err
	}
	if err := s.repo.MarkCompleted(ctx, rec.UserID, result); err != nil {
		return err
	}
	// 账号已停用：清掉 JWT 与 API Key 两条鉴权路径上的用户缓存
	if s.userService != nil {
		s.userService.invalidateAuthUserCache(rec.UserID)
	}
	if s.authCache != nil {
		s.authCache.InvalidateAuthCacheByUserID(ctx, rec.UserID)
	}
	logger.LegacyPrintf("service.metask_migration", "[apply] completed: user=%d balance_mu=%d", rec.UserID, rec.Payload.BalanceMU)
	return nil
}

func (s *MetaskMigrationService) invalidateAfterFreeze(ctx context.Context, userID int64, fr *MetaskMigrationFreezeResult) {
	if s.userService != nil {
		s.userService.invalidateAuthUserCache(userID)
	}
	if s.authCache != nil {
		for _, key := range fr.RawKeys {
			s.authCache.InvalidateAuthCacheByKey(ctx, key)
		}
		s.authCache.InvalidateAuthCacheByUserID(ctx, userID)
	}
	if s.billingCache != nil {
		if err := s.billingCache.InvalidateUserBalance(ctx, userID); err != nil {
			logger.LegacyPrintf("service.metask_migration", "[freeze] invalidate balance cache: user=%d err=%v", userID, err)
		}
		if err := s.billingCache.InvalidateUserSubscriptions(ctx, userID); err != nil {
			logger.LegacyPrintf("service.metask_migration", "[freeze] invalidate subscription cache: user=%d err=%v", userID, err)
		}
	}
	if s.subService != nil {
		for _, planID := range fr.PlanIDs {
			s.subService.InvalidateSubCache(userID, planID)
		}
		s.subService.InvalidateMergedSubCache(userID)
	}
}

// StartRetryLoop 每分钟补调一次还没入账成功的迁移（用户冻结后关掉页面、或 metask-ai 临时不可用）。
func (s *MetaskMigrationService) StartRetryLoop() {
	if s == nil || s.repo == nil || s.client == nil {
		return
	}
	s.retryStartOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for range ticker.C {
				s.retryPending()
			}
		}()
	})
}

func (s *MetaskMigrationService) retryPending() {
	if !s.Enabled() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	ids, err := s.repo.ListPending(ctx, 30*time.Second, 20)
	if err != nil {
		logger.LegacyPrintf("service.metask_migration", "[retry] list pending: %v", err)
		return
	}
	for _, id := range ids {
		rec, err := s.repo.Get(ctx, id)
		if err != nil || rec == nil || rec.Status == MetaskMigrationStatusCompleted {
			continue
		}
		if err := s.apply(ctx, rec); err != nil && !errors.Is(err, context.Canceled) {
			logger.LegacyPrintf("service.metask_migration", "[retry] apply failed: user=%d attempts=%d err=%v", id, rec.Attempts+1, err)
		}
	}
}
