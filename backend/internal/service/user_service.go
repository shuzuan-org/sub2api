package service

import (
	"context"
	"fmt"
	"log"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

var (
	ErrUserNotFound      = infraerrors.NotFound("USER_NOT_FOUND", "user not found")
	ErrPasswordIncorrect = infraerrors.BadRequest("PASSWORD_INCORRECT", "current password is incorrect")
	ErrInsufficientPerms = infraerrors.Forbidden("INSUFFICIENT_PERMISSIONS", "insufficient permissions")
	// ErrReferralCodeConflict 表示设置邀请码时发生唯一冲突或目标用户已有邀请码（需重试或视为已存在）。
	ErrReferralCodeConflict = infraerrors.Conflict("REFERRAL_CODE_CONFLICT", "referral code conflict")

	// 手机号绑定错误
	ErrPhoneAlreadyBound       = infraerrors.BadRequest("PHONE_ALREADY_BOUND", "该手机号已绑定当前账户")
	ErrPhoneNumberAlreadyBound = infraerrors.Conflict("PHONE_NUMBER_ALREADY_BOUND", "该手机号已绑定其他账户")
	ErrInvalidPhoneNumber      = infraerrors.BadRequest("INVALID_PHONE_NUMBER", "手机号格式不正确")
)

// UserListFilters contains all filter options for listing users
type UserListFilters struct {
	Status     string           // User status filter
	Role       string           // User role filter
	Search     string           // Search in email, username
	GroupName  string           // Filter by allowed group name (fuzzy match)
	Attributes map[int64]string // Custom attribute filters: attributeID -> value
	// IncludeSubscriptions controls whether ListWithFilters should load active subscriptions.
	// For large datasets this can be expensive; admin list pages should enable it on demand.
	// nil means not specified (default: load subscriptions for backward compatibility).
	IncludeSubscriptions *bool
}

type UserRepository interface {
	Create(ctx context.Context, user *User) error
	GetByID(ctx context.Context, id int64) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	GetFirstAdmin(ctx context.Context) (*User, error)
	Update(ctx context.Context, user *User) error
	Delete(ctx context.Context, id int64) error

	List(ctx context.Context, params pagination.PaginationParams) ([]User, *pagination.PaginationResult, error)
	ListWithFilters(ctx context.Context, params pagination.PaginationParams, filters UserListFilters) ([]User, *pagination.PaginationResult, error)

	UpdateBalance(ctx context.Context, id int64, amount float64) error
	DeductBalance(ctx context.Context, id int64, amount float64) error
	UpdateConcurrency(ctx context.Context, id int64, amount int) error
	ExistsByEmail(ctx context.Context, email string) (bool, error)
	// GetByPhone 按手机号查找用户（仅匹配非空 phone）
	GetByPhone(ctx context.Context, phone string) (*User, error)
	// ExistsByPhone 检查手机号是否已存在（仅匹配非空 phone）
	ExistsByPhone(ctx context.Context, phone string) (bool, error)
	RemoveGroupFromAllowedGroups(ctx context.Context, groupID int64) (int64, error)
	// AddGroupToAllowedGroups 将指定分组增量添加到用户的 allowed_groups（幂等，冲突忽略）
	AddGroupToAllowedGroups(ctx context.Context, userID int64, groupID int64) error
	// RemoveGroupFromUserAllowedGroups 移除单个用户的指定分组权限
	RemoveGroupFromUserAllowedGroups(ctx context.Context, userID int64, groupID int64) error
	// ListUsersByGroupAllowed 按分组查询所有已授权用户（通过 user_allowed_groups 联接表）
	ListUsersByGroupAllowed(ctx context.Context, groupID int64) ([]User, error)

	// TOTP 双因素认证
	UpdateTotpSecret(ctx context.Context, userID int64, encryptedSecret *string) error
	EnableTotp(ctx context.Context, userID int64) error
	DisableTotp(ctx context.Context, userID int64) error

	// 邀请好友
	// GetByReferralCode 按专属邀请码查用户；未找到返回 ErrUserNotFound。
	GetByReferralCode(ctx context.Context, code string) (*User, error)
	// SetReferralCode 仅当用户当前无邀请码时写入（WHERE referral_code IS NULL）。
	// 唯一冲突或用户已有码时返回 ErrReferralCodeConflict。
	SetReferralCode(ctx context.Context, id int64, code string) error
	// SetReferredBy 设置用户的邀请人。
	SetReferredBy(ctx context.Context, id int64, referrerID int64) error

	// 手机号绑定
	GetByPhoneNumber(ctx context.Context, phone string) (*User, error)
	ExistsByPhoneNumber(ctx context.Context, phone string) (bool, error)
	// BindPhoneAndGrantBonus 绑定手机号并赠送余额（事务内原子执行）。
	BindPhoneAndGrantBonus(ctx context.Context, userID int64, phone string, bonusAmount float64) (*User, error)
}

// UpdateProfileRequest 更新用户资料请求
type UpdateProfileRequest struct {
	Email       *string `json:"email"`
	Username    *string `json:"username"`
	Concurrency *int    `json:"concurrency"`
}

// ChangePasswordRequest 修改密码请求
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// UserService 用户服务
type UserService struct {
	userRepo             UserRepository
	authCacheInvalidator APIKeyAuthCacheInvalidator
	billingCache         BillingCache
	channelInviteSvc     *ChannelInviteService
	inviteSvc            *InviteService
	// authUserCache 是鉴权读路径专用的本地用户缓存，消除中间件每请求查库。
	// 详见 user_auth_cache.go。
	authUserCache *userAuthCache
}

// NewUserService 创建用户服务实例
func NewUserService(userRepo UserRepository, authCacheInvalidator APIKeyAuthCacheInvalidator, billingCache BillingCache) *UserService {
	return &UserService{
		userRepo:             userRepo,
		authCacheInvalidator: authCacheInvalidator,
		billingCache:         billingCache,
		authUserCache:        newUserAuthCache(defaultUserAuthCacheTTL),
	}
}

// SetChannelInviteService sets the channel invite service for deferred bonus granting.
func (s *UserService) SetChannelInviteService(svc *ChannelInviteService) {
	s.channelInviteSvc = svc
}

// SetInviteService 注入普通邀请码服务，用于被邀请人绑机成功后给邀请人发放奖励。
func (s *UserService) SetInviteService(svc *InviteService) {
	s.inviteSvc = svc
}

// GetFirstAdmin 获取首个管理员用户（用于 Admin API Key 认证）
func (s *UserService) GetFirstAdmin(ctx context.Context) (*User, error) {
	admin, err := s.userRepo.GetFirstAdmin(ctx)
	if err != nil {
		return nil, fmt.Errorf("get first admin: %w", err)
	}
	return admin, nil
}

// GetProfile 获取用户资料
func (s *UserService) GetProfile(ctx context.Context, userID int64) (*User, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	return user, nil
}

// UpdateProfile 更新用户资料
func (s *UserService) UpdateProfile(ctx context.Context, userID int64, req UpdateProfileRequest) (*User, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	oldConcurrency := user.Concurrency

	// 更新字段
	if req.Email != nil {
		// 检查新邮箱是否已被使用
		exists, err := s.userRepo.ExistsByEmail(ctx, *req.Email)
		if err != nil {
			return nil, fmt.Errorf("check email exists: %w", err)
		}
		if exists && *req.Email != user.Email {
			return nil, ErrEmailExists
		}
		user.Email = *req.Email
	}

	if req.Username != nil {
		user.Username = *req.Username
	}

	if req.Concurrency != nil {
		user.Concurrency = *req.Concurrency
	}

	if err := s.userRepo.Update(ctx, user); err != nil {
		return nil, fmt.Errorf("update user: %w", err)
	}
	s.invalidateAuthUserCache(userID)
	if s.authCacheInvalidator != nil && user.Concurrency != oldConcurrency {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
	}

	return user, nil
}

// ChangePassword 修改密码
// Security: Increments TokenVersion to invalidate all existing JWT tokens
func (s *UserService) ChangePassword(ctx context.Context, userID int64, req ChangePasswordRequest) error {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}

	// 验证当前密码
	if !user.CheckPassword(req.CurrentPassword) {
		return ErrPasswordIncorrect
	}

	if err := user.SetPassword(req.NewPassword); err != nil {
		return fmt.Errorf("set password: %w", err)
	}

	// Increment TokenVersion to invalidate all existing tokens
	// This ensures that any tokens issued before the password change become invalid
	user.TokenVersion++

	if err := s.userRepo.Update(ctx, user); err != nil {
		return fmt.Errorf("update user: %w", err)
	}

	// 改密后 TokenVersion 已自增，必须清除本地鉴权缓存，
	// 否则缓存中的旧 user 会让已撤销的 token 在 TTL 内仍通过 TokenVersion 校验。
	s.invalidateAuthUserCache(userID)

	return nil
}

// GetByID 根据ID获取用户（管理员功能）
func (s *UserService) GetByID(ctx context.Context, id int64) (*User, error) {
	user, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	return user, nil
}

// List 获取用户列表（管理员功能）
func (s *UserService) List(ctx context.Context, params pagination.PaginationParams) ([]User, *pagination.PaginationResult, error) {
	users, pagination, err := s.userRepo.List(ctx, params)
	if err != nil {
		return nil, nil, fmt.Errorf("list users: %w", err)
	}
	return users, pagination, nil
}

// UpdateBalance 更新用户余额（管理员功能）
func (s *UserService) UpdateBalance(ctx context.Context, userID int64, amount float64) error {
	if err := s.userRepo.UpdateBalance(ctx, userID, amount); err != nil {
		return fmt.Errorf("update balance: %w", err)
	}
	s.invalidateAuthUserCache(userID)
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
	}
	if s.billingCache != nil {
		go func() {
			cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.billingCache.InvalidateUserBalance(cacheCtx, userID); err != nil {
				log.Printf("invalidate user balance cache failed: user_id=%d err=%v", userID, err)
			}
		}()
	}
	return nil
}

// UpdateConcurrency 更新用户并发数（管理员功能）
func (s *UserService) UpdateConcurrency(ctx context.Context, userID int64, concurrency int) error {
	if err := s.userRepo.UpdateConcurrency(ctx, userID, concurrency); err != nil {
		return fmt.Errorf("update concurrency: %w", err)
	}
	s.invalidateAuthUserCache(userID)
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
	}
	return nil
}

// UpdateStatus 更新用户状态（管理员功能）
func (s *UserService) UpdateStatus(ctx context.Context, userID int64, status string) error {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}

	user.Status = status

	if err := s.userRepo.Update(ctx, user); err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	s.invalidateAuthUserCache(userID)
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
	}

	return nil
}

// Delete 删除用户（管理员功能）
func (s *UserService) Delete(ctx context.Context, userID int64) error {
	s.invalidateAuthUserCache(userID)
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
	}
	if err := s.userRepo.Delete(ctx, userID); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return nil
}

const phoneBindBonusAmount = 100.0 // 绑定手机号赠送 100U

// BindPhoneAndGrantBonus 绑定手机号并赠送余额。
// 使用 Redis 分布式锁 + 数据库事务保证并发安全。
// 若用户有待发放的渠道活动奖励，则跳过 100U 基础奖励（不叠加）。
// 返回实际赠送金额和更新后的用户。
func (s *UserService) BindPhoneAndGrantBonus(ctx context.Context, userID int64, phone string) (float64, *User, error) {
	// 检查是否有待发放的渠道邀请奖励，有则跳过 100U 基础奖励
	bonusAmount := phoneBindBonusAmount
	if s.channelInviteSvc != nil {
		hasPending, err := s.channelInviteSvc.HasPendingBonuses(ctx, userID)
		if err == nil && hasPending {
			bonusAmount = 0
		}
	}

	// 已通过验证码校验，此处直接绑定
	user, err := s.userRepo.BindPhoneAndGrantBonus(ctx, userID, phone, bonusAmount)
	if err != nil {
		return 0, nil, err
	}

	// 失效缓存
	s.invalidateAuthUserCache(userID)
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
	}
	if s.billingCache != nil {
		go func() {
			cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.billingCache.InvalidateUserBalance(cacheCtx, userID); err != nil {
				log.Printf("invalidate user balance cache failed: user_id=%d err=%v", userID, err)
			}
		}()
	}

	// 发放待处理的渠道邀请奖励，并把这次绑定实际触发的赠送金额返回给前端。
	if s.channelInviteSvc != nil {
		granted, err := s.channelInviteSvc.GrantPendingBonuses(ctx, userID)
		if err != nil {
			log.Printf("grant pending channel invite bonuses failed: user_id=%d err=%v", userID, err)
		} else if granted > 0 {
			bonusAmount += granted
			if refreshed, err := s.userRepo.GetByID(ctx, userID); err == nil {
				user = refreshed
			}
		}
	}

	// 普通邀请码：被邀请人绑机成功后，额外给邀请人发放 100U（被邀请人自己的 100U 由上面的基础逻辑发放，互不影响）。
	// referred_by 仅由普通邀请码归因写入（渠道码不写），故此判定即「普通邀请码的被邀请人」。
	// 绑机为一次性原子动作，本步天然只会执行一次，不会重复发放。
	if s.inviteSvc != nil && user != nil && user.ReferredBy != nil && *user.ReferredBy > 0 {
		if _, err := s.inviteSvc.RewardInviterOnInviteeBind(ctx, *user.ReferredBy); err != nil {
			log.Printf("reward inviter on invitee bind failed: invitee=%d inviter=%d err=%v", userID, *user.ReferredBy, err)
		}
	}

	return bonusAmount, user, nil
}

// ExistsByPhoneNumber checks if a phone number is already bound to any account.
func (s *UserService) ExistsByPhoneNumber(ctx context.Context, phone string) (bool, error) {
	return s.userRepo.ExistsByPhoneNumber(ctx, phone)
}
