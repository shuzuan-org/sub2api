package service

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbuser "github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

// referralCodeCharset 邀请码字符集：去掉易混淆字符（I/O/0/1）的大写字母+数字。
const referralCodeCharset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// referralCodeLen 邀请码长度（6 位）。
const referralCodeLen = 6

// referralCodeMaxRetry 懒创建时唯一冲突的最大重试次数。
const referralCodeMaxRetry = 5

// InviteeRecord 邀请明细记录（一条 = 一个被邀请用户）。
// 充值/佣金相关字段本期为占位（恒为 0），状态恒为 registered。
type InviteeRecord struct {
	Email        string
	Username     string
	RegisteredAt time.Time
	TotalRecharge float64 // 占位：本期恒 0
	Status       string  // 占位：恒 "registered"
}

// InviteService 邀请好友服务。
type InviteService struct {
	entClient            *dbent.Client
	userRepo             UserRepository
	settingService       *SettingService
	billingCacheService  *BillingCacheService
	authCacheInvalidator APIKeyAuthCacheInvalidator
}

// NewInviteService 创建邀请好友服务实例。
func NewInviteService(
	entClient *dbent.Client,
	userRepo UserRepository,
	settingService *SettingService,
	billingCacheService *BillingCacheService,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
) *InviteService {
	return &InviteService{
		entClient:            entClient,
		userRepo:             userRepo,
		settingService:       settingService,
		billingCacheService:  billingCacheService,
		authCacheInvalidator: authCacheInvalidator,
	}
}

// GenerateReferralCode 生成一个 6 位邀请码（大写字母+数字，去易混淆字符）。
func GenerateReferralCode() (string, error) {
	b := make([]byte, referralCodeLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, referralCodeLen)
	for i := range b {
		out[i] = referralCodeCharset[int(b[i])%len(referralCodeCharset)]
	}
	return string(out), nil
}

// GetOrCreateCode 返回用户的专属邀请码，无则懒创建（唯一冲突重试 ≤5 次）。
func (s *InviteService) GetOrCreateCode(ctx context.Context, userID int64) (string, error) {
	u, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return "", err
	}
	if u.ReferralCode != nil && *u.ReferralCode != "" {
		return *u.ReferralCode, nil
	}

	for i := 0; i < referralCodeMaxRetry; i++ {
		code, genErr := GenerateReferralCode()
		if genErr != nil {
			return "", genErr
		}
		setErr := s.userRepo.SetReferralCode(ctx, userID, code)
		if setErr == nil {
			return code, nil
		}
		if errors.Is(setErr, ErrReferralCodeConflict) {
			// 冲突可能是：码已被他人占用（重试换码），或本用户已有码（并发首访）→ 重读返回已有码。
			if cur, getErr := s.userRepo.GetByID(ctx, userID); getErr == nil &&
				cur.ReferralCode != nil && *cur.ReferralCode != "" {
				return *cur.ReferralCode, nil
			}
			continue
		}
		return "", setErr
	}
	return "", ErrReferralCodeConflict
}

// ListInvitees 分页查询「我邀请的人」（referred_by = userID），可按邮箱/用户名模糊搜索。
func (s *InviteService) ListInvitees(
	ctx context.Context, userID int64, page, pageSize int, search string,
) ([]InviteeRecord, int, error) {
	params := pagination.PaginationParams{Page: page, PageSize: pageSize}

	q := s.entClient.User.Query().Where(dbuser.ReferredByEQ(userID))
	if search != "" {
		q = q.Where(dbuser.Or(
			dbuser.EmailContainsFold(search),
			dbuser.UsernameContainsFold(search),
		))
	}

	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	rows, err := q.
		Order(dbent.Desc(dbuser.FieldCreatedAt)).
		Offset(params.Offset()).
		Limit(params.Limit()).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}

	out := make([]InviteeRecord, 0, len(rows))
	for _, r := range rows {
		out = append(out, InviteeRecord{
			Email:         r.Email,
			Username:      r.Username,
			RegisteredAt:  r.CreatedAt,
			TotalRecharge: 0,            // 占位：充值归因本期不做
			Status:        "registered", // 占位：恒已注册
		})
	}
	return out, total, nil
}

// AttributeAndReward 在「新用户已创建」之后调用：把 newUserID 归因到 referralCode 对应的邀请人，
// 并按 settings 配置给邀请人发放固定 U 奖励。
//
// 设计约定（重要）：
//   - referralCode 为空 / 无效 / 自邀 → 记录日志后返回 nil，绝不报错（不得阻断注册）。
//   - 调用方应传入事务上下文 txCtx；本方法内的写操作都在该事务内完成。
//   - 返回 (inviterID, credited)：credited 表示是否确实发放了余额，调用方据此在事务外失效缓存。
func (s *InviteService) AttributeAndReward(
	txCtx context.Context, newUserID int64, referralCode string,
) (inviterID int64, credited bool) {
	if referralCode == "" {
		return 0, false
	}
	inviter, err := s.userRepo.GetByReferralCode(txCtx, referralCode)
	if err != nil {
		logger.LegacyPrintf("service.invite",
			"[Invite] referral code not found, skip: code=%s err=%v", referralCode, err)
		return 0, false
	}
	if inviter.ID == newUserID {
		logger.LegacyPrintf("service.invite",
			"[Invite] self-invite ignored: user=%d", newUserID)
		return 0, false
	}

	if err := s.userRepo.SetReferredBy(txCtx, newUserID, inviter.ID); err != nil {
		logger.LegacyPrintf("service.invite",
			"[Invite] set referred_by failed: newUser=%d inviter=%d err=%v",
			newUserID, inviter.ID, err)
		return 0, false
	}

	amount := s.settingService.GetInviteRewardAmount(txCtx)
	if amount <= 0 {
		return inviter.ID, false
	}
	if err := s.userRepo.UpdateBalance(txCtx, inviter.ID, amount); err != nil {
		logger.LegacyPrintf("service.invite",
			"[Invite] credit inviter failed: inviter=%d amount=%.4f err=%v",
			inviter.ID, amount, err)
		return 0, false
	}
	logger.LegacyPrintf("service.invite",
		"[Invite] rewarded inviter: inviter=%d newUser=%d amount=%.4f",
		inviter.ID, newUserID, amount)
	return inviter.ID, true
}

// InvalidateInviterCache 在奖励发放成功且事务提交后调用，失效邀请人的鉴权/余额缓存。
func (s *InviteService) InvalidateInviterCache(ctx context.Context, inviterID int64) {
	if inviterID <= 0 {
		return
	}
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, inviterID)
	}
	if s.billingCacheService != nil {
		cacheCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.billingCacheService.InvalidateUserBalance(cacheCtx, inviterID)
	}
}
