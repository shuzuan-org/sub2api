package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

var (
	ErrChannelInviteBatchNotFound     = infraerrors.NotFound("CHANNEL_INVITE_BATCH_NOT_FOUND", "channel invite batch not found")
	ErrChannelInviteCodeNotFound      = infraerrors.NotFound("CHANNEL_INVITE_CODE_NOT_FOUND", "channel invite code not found")
	ErrChannelInviteCodeExpired       = infraerrors.BadRequest("CHANNEL_INVITE_CODE_EXPIRED", "channel invite code has expired")
	ErrChannelInviteCodeDisabled      = infraerrors.BadRequest("CHANNEL_INVITE_CODE_DISABLED", "channel invite code is disabled")
	ErrChannelInviteCodeMaxUsed       = infraerrors.BadRequest("CHANNEL_INVITE_CODE_MAX_USED", "channel invite code has reached maximum uses")
	ErrChannelInviteCodeAlreadyUsed   = infraerrors.Conflict("CHANNEL_INVITE_CODE_ALREADY_USED", "you have already claimed this invite code")
	ErrChannelInviteCodeBatchInactive = infraerrors.BadRequest("CHANNEL_INVITE_BATCH_INACTIVE", "channel invite batch is not active")
	ErrChannelInviteAlreadyGranted    = infraerrors.Conflict("CHANNEL_INVITE_ALREADY_GRANTED", "您已参加过渠道活动，每位用户只能参加一次")
)

// ChannelInviteService 渠道邀请码服务
type ChannelInviteService struct {
	repo                 ChannelInviteRepository
	userRepo             UserRepository
	entClient            *dbent.Client
	billingCache         BillingCache
	authCacheInvalidator APIKeyAuthCacheInvalidator
}

// NewChannelInviteService 创建渠道邀请码服务实例
func NewChannelInviteService(
	repo ChannelInviteRepository,
	userRepo UserRepository,
	entClient *dbent.Client,
	billingCache BillingCache,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
) *ChannelInviteService {
	return &ChannelInviteService{
		repo:                 repo,
		userRepo:             userRepo,
		entClient:            entClient,
		billingCache:         billingCache,
		authCacheInvalidator: authCacheInvalidator,
	}
}

// ======================== 批次管理 ========================

// CreateBatch 创建渠道邀请码批次
func (s *ChannelInviteService) CreateBatch(ctx context.Context, input *CreateChannelInviteBatchInput) (*ChannelInviteBatch, error) {
	batch := &ChannelInviteBatch{
		Name:           strings.TrimSpace(input.Name),
		BonusAmount:    input.BonusAmount,
		MaxUsesPerCode: input.MaxUsesPerCode,
		StartTime:      input.StartTime,
		EndTime:        input.EndTime,
		Status:         ChannelInviteBatchStatusActive,
		Notes:          input.Notes,
		CreatedBy:      input.CreatedBy,
	}

	if batch.MaxUsesPerCode <= 0 {
		batch.MaxUsesPerCode = 1
	}

	if err := s.repo.CreateBatch(ctx, batch, input.GroupIDs); err != nil {
		return nil, fmt.Errorf("create batch: %w", err)
	}

	// 自动为每个活动生成 1 个邀请码（一人一码一活动）
	_, err := s.GenerateCodes(ctx, batch.ID, 1)
	if err != nil {
		return nil, fmt.Errorf("auto-generate code: %w", err)
	}

	return batch, nil
}

// GetBatch 获取批次详情
func (s *ChannelInviteService) GetBatch(ctx context.Context, id int64) (*ChannelInviteBatch, error) {
	batch, err := s.repo.GetBatch(ctx, id)
	if err != nil {
		return nil, err
	}
	// 填充计数
	codeCount, usedCount, err := s.repo.GetBatchCodeStats(ctx, id)
	if err == nil {
		batch.CodeCount = codeCount
		batch.UsedCount = usedCount
	}
	return batch, nil
}

// UpdateBatch 更新批次
func (s *ChannelInviteService) UpdateBatch(ctx context.Context, id int64, input *UpdateChannelInviteBatchInput) (*ChannelInviteBatch, error) {
	if err := s.repo.UpdateBatch(ctx, id, input); err != nil {
		return nil, fmt.Errorf("update batch: %w", err)
	}
	return s.GetBatch(ctx, id)
}

// DeleteBatch 删除批次
func (s *ChannelInviteService) DeleteBatch(ctx context.Context, id int64) error {
	if err := s.repo.DeleteBatch(ctx, id); err != nil {
		return fmt.Errorf("delete batch: %w", err)
	}
	return nil
}

// ListBatches 获取批次列表
func (s *ChannelInviteService) ListBatches(ctx context.Context, params pagination.PaginationParams, status, search string) ([]ChannelInviteBatch, *pagination.PaginationResult, error) {
	return s.repo.ListBatches(ctx, params, status, search)
}

// ======================== 码管理 ========================

// GenerateCodes 批量生成邀请码
func (s *ChannelInviteService) GenerateCodes(ctx context.Context, batchID int64, count int) ([]ChannelInviteCode, error) {
	if count <= 0 || count > 500 {
		return nil, infraerrors.BadRequest("INVALID_COUNT", "count must be between 1 and 500")
	}

	batch, err := s.repo.GetBatch(ctx, batchID)
	if err != nil {
		return nil, err
	}

	codes := make([]ChannelInviteCode, 0, count)
	for i := 0; i < count; i++ {
		codeStr, err := s.generateRandomCode()
		if err != nil {
			return nil, fmt.Errorf("generate random code: %w", err)
		}
		codes = append(codes, ChannelInviteCode{
			BatchID: batchID,
			Code:    codeStr,
			Status:  ChannelInviteCodeStatusUnused,
			MaxUses: batch.MaxUsesPerCode,
		})
	}

	if err := s.repo.CreateCodes(ctx, codes); err != nil {
		return nil, fmt.Errorf("create codes: %w", err)
	}

	return codes, nil
}

// ListCodes 获取批次的码列表
func (s *ChannelInviteService) ListCodes(ctx context.Context, batchID int64, params pagination.PaginationParams, status, search string) ([]ChannelInviteCode, *pagination.PaginationResult, error) {
	return s.repo.ListCodes(ctx, batchID, params, status, search)
}

// ListUsages 获取批次的使用记录
func (s *ChannelInviteService) ListUsages(ctx context.Context, batchID int64, params pagination.PaginationParams) ([]ChannelInviteCodeUsage, *pagination.PaginationResult, error) {
	return s.repo.ListUsagesByBatch(ctx, batchID, params)
}

// ======================== 兑换流程 ========================

// ClaimCode 用户兑换渠道邀请码
// 1. 校验码和批次状态
// 2. 事务中：增加使用次数、创建使用记录、添加用户到目标分组
// 3. 如果用户已绑定手机号，立即发放奖励
func (s *ChannelInviteService) ClaimCode(ctx context.Context, userID int64, codeStr string) error {
	codeStr = strings.TrimSpace(codeStr)
	if codeStr == "" {
		return ErrChannelInviteCodeNotFound
	}

	// 获取用户信息
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}

	// 开启事务
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	txCtx := dbent.NewTxContext(ctx, tx)

	// 事务中获取并锁定码记录
	code, err := s.repo.GetCodeByCodeForUpdate(txCtx, codeStr)
	if err != nil {
		return err
	}

	// 验证批次状态
	if code.Batch == nil || !code.Batch.IsActive() {
		return ErrChannelInviteCodeBatchInactive
	}

	// 验证码状态
	if err := s.validateCodeForClaim(code); err != nil {
		return err
	}

	// 检查用户是否已获得过任何渠道活动奖励（一用户只能参加一次）
	hasPrior, err := s.repo.HasPriorBonusGrantedByUser(txCtx, userID)
	if err != nil {
		return fmt.Errorf("check prior bonus: %w", err)
	}
	if hasPrior {
		return ErrChannelInviteAlreadyGranted
	}

	// 检查用户是否已使用过此码
	existing, err := s.repo.GetUsageByCodeAndUser(txCtx, code.ID, userID)
	if err != nil {
		return fmt.Errorf("check existing usage: %w", err)
	}
	if existing != nil {
		return ErrChannelInviteCodeAlreadyUsed
	}

	// 增加使用次数
	if err := s.repo.IncrementUsedCount(txCtx, code.ID); err != nil {
		return fmt.Errorf("increment used count: %w", err)
	}

	// 创建使用记录（bonus_granted 先设为 false）
	usage := &ChannelInviteCodeUsage{
		CodeID:       code.ID,
		BatchID:      code.BatchID,
		UserID:       userID,
		BonusGranted: false,
		ClaimedAt:    time.Now(),
	}
	if err := s.repo.CreateUsage(txCtx, usage); err != nil {
		return fmt.Errorf("create usage record: %w", err)
	}

	// 添加用户到目标分组
	groupIDs, err := s.repo.GetBatchGroupIDs(txCtx, code.BatchID)
	if err != nil {
		return fmt.Errorf("get batch groups: %w", err)
	}
	for _, gid := range groupIDs {
		if err := s.userRepo.AddGroupToAllowedGroups(txCtx, userID, gid); err != nil {
			// 忽略唯一约束冲突（用户已在分组中）
			if !strings.Contains(err.Error(), "duplicate key") && !strings.Contains(err.Error(), "unique") {
				return fmt.Errorf("add user to group %d: %w", gid, err)
			}
		}
	}

	// 如果用户已经绑定了手机号，立即发放奖励
	bonusImmediate := user.PhoneNumber != nil && *user.PhoneNumber != ""
	if bonusImmediate {
		if err := s.grantBonusInTx(txCtx, usage.ID, userID, code.Batch.BonusAmount); err != nil {
			return fmt.Errorf("grant immediate bonus: %w", err)
		}
		usage.BonusGranted = true
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	// 失效缓存
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
	}
	if s.billingCache != nil {
		go func() {
			cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.billingCache.InvalidateUserBalance(cacheCtx, userID)
		}()
	}

	return nil
}

// ======================== 公开发验 ========================

// ValidateCodeResult 邀请码校验结果
type ValidateCodeResult struct {
	Valid         bool   `json:"valid"`
	Type          string `json:"type,omitempty"` // "channel" | "friend"
	RemainingUses int    `json:"remaining_uses,omitempty"`
	BatchStatus   string `json:"batch_status,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

// ValidateCode 公开发验邀请码：渠道码校验活动状态和剩余次数，朋友码校验是否存在
func (s *ChannelInviteService) ValidateCode(ctx context.Context, codeStr string) ValidateCodeResult {
	codeStr = strings.TrimSpace(codeStr)
	if codeStr == "" {
		return ValidateCodeResult{Valid: false, Reason: "code is empty"}
	}

	// 12位 hex → 渠道活动码
	if isChannelCodeFormat(codeStr) {
		code, err := s.repo.GetCodeByCode(ctx, codeStr)
		if err != nil {
			return ValidateCodeResult{Valid: false, Reason: "邀请码无效"}
		}
		if code.Batch == nil || !code.Batch.IsActive() {
			return ValidateCodeResult{Valid: false, Reason: "该活动已结束"}
		}
		if !code.CanClaim() {
			return ValidateCodeResult{Valid: false, Reason: "邀请码已被使用或已达上限"}
		}
		return ValidateCodeResult{
			Valid:         true,
			Type:          "channel",
			RemainingUses: code.MaxUses - code.UsedCount,
			BatchStatus:   code.Batch.Status,
		}
	}

	// 6位 → 朋友邀请码
	if len(codeStr) == 6 {
		_, err := s.userRepo.GetByReferralCode(ctx, codeStr)
		if err != nil {
			return ValidateCodeResult{Valid: false, Reason: "邀请码无效"}
		}
		return ValidateCodeResult{Valid: true, Type: "friend"}
	}

	return ValidateCodeResult{Valid: false, Reason: "邀请码格式不正确"}
}

func isChannelCodeFormat(code string) bool {
	if len(code) != 12 {
		return false
	}
	for _, c := range code {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// HasPendingBonuses 检查用户是否有待发放的渠道邀请奖励
func (s *ChannelInviteService) HasPendingBonuses(ctx context.Context, userID int64) (bool, error) {
	return s.repo.HasPendingBonusByUser(ctx, userID)
}

// GrantPendingBonuses 发放用户所有待发放的渠道邀请奖励（手机绑定后调用）
func (s *ChannelInviteService) GrantPendingBonuses(ctx context.Context, userID int64) error {
	usages, err := s.repo.ListPendingBonusByUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("list pending bonuses: %w", err)
	}

	if len(usages) == 0 {
		return nil
	}

	for _, usage := range usages {
		bonusAmount := float64(0)
		if usage.Batch != nil {
			bonusAmount = usage.Batch.BonusAmount
		}

		// 发放奖励（更新余额 + 标记 usage）
		if err := s.grantBonus(ctx, usage.ID, userID, bonusAmount); err != nil {
			logger.LegacyPrintf("service.channel_invite", "[GrantBonus] grant failed: usage=%d user=%d amount=%.2f err=%v",
				usage.ID, userID, bonusAmount, err)
			continue
		}

		logger.LegacyPrintf("service.channel_invite", "[GrantBonus] granted: usage=%d user=%d amount=%.2f",
			usage.ID, userID, bonusAmount)
	}

	return nil
}

// ======================== 内部方法 ========================

func (s *ChannelInviteService) validateCodeForClaim(code *ChannelInviteCode) error {
	if !code.CanClaim() {
		if code.Status == ChannelInviteCodeStatusExpired {
			return ErrChannelInviteCodeExpired
		}
		if code.MaxUses > 0 && code.UsedCount >= code.MaxUses {
			return ErrChannelInviteCodeMaxUsed
		}
		return ErrChannelInviteCodeNotFound
	}
	return nil
}

// grantBonusInTx 在事务中发放奖励
func (s *ChannelInviteService) grantBonusInTx(ctx context.Context, usageID int64, userID int64, amount float64) error {
	if amount <= 0 {
		return nil
	}

	// 更新用户余额
	if err := s.userRepo.UpdateBalance(ctx, userID, amount); err != nil {
		return fmt.Errorf("update balance: %w", err)
	}

	// 标记 usage 奖励已发放
	if err := s.repo.GrantBonus(ctx, usageID, amount); err != nil {
		return fmt.Errorf("mark bonus granted: %w", err)
	}

	return nil
}

// grantBonus 发放奖励（非事务）
func (s *ChannelInviteService) grantBonus(ctx context.Context, usageID int64, userID int64, amount float64) error {
	if amount <= 0 {
		return nil
	}

	// 开启事务
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	txCtx := dbent.NewTxContext(ctx, tx)

	if err := s.grantBonusInTx(txCtx, usageID, userID, amount); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

func (s *ChannelInviteService) generateRandomCode() (string, error) {
	bytes := make([]byte, 6)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	return strings.ToUpper(hex.EncodeToString(bytes)), nil
}
