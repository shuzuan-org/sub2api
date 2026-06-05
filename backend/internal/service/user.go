package service

import (
	"time"

	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID            int64
	Email         string
	Username      string
	Notes         string
	PasswordHash  string
	Role          string
	Balance       float64
	Concurrency   int
	Status        string
	AllowedGroups []int64
	TokenVersion  int64 // Incremented on password change to invalidate existing tokens
	CreatedAt     time.Time
	UpdatedAt     time.Time

	// GroupRates 用户专属分组倍率配置
	// map[groupID]rateMultiplier
	GroupRates map[int64]float64

	// Sora 存储配额
	SoraStorageQuotaBytes int64 // 用户级 Sora 存储配额（0 表示使用分组或系统默认值）
	SoraStorageUsedBytes  int64 // Sora 存储已用量

	// TOTP 双因素认证字段
	TotpSecretEncrypted *string    // AES-256-GCM 加密的 TOTP 密钥
	TotpEnabled         bool       // 是否启用 TOTP
	TotpEnabledAt       *time.Time // TOTP 启用时间

	// 手机号登录
	Phone        string // 手机号（唯一约束通过部分索引实现）
	PhoneVerified bool  // 手机号是否已验证

	// 邀请好友
	ReferralCode *string // 用户专属邀请码（懒创建，6 位大写字母+数字）
	ReferredBy   *int64  // 邀请人 user_id

	// 手机号绑定
	PhoneNumber        *string    // 绑定手机号（E.164 格式，如 +8613800138000）
	PhoneBoundAt       *time.Time // 绑定时间
	PhoneBonusGrantedAt *time.Time // 绑定赠送 100U 发放时间

	APIKeys       []APIKey
	Subscriptions []UserSubscription
}

func (u *User) IsAdmin() bool {
	return u.Role == RoleAdmin
}

func (u *User) IsActive() bool {
	return u.Status == StatusActive
}

// CanBindGroup checks whether a user can bind to a given group.
// For standard groups:
// - Public groups (non-exclusive): all users can bind
// - Exclusive groups: only users with the group in AllowedGroups can bind
func (u *User) CanBindGroup(groupID int64, isExclusive bool) bool {
	// 公开分组（非专属）：所有用户都可以绑定
	if !isExclusive {
		return true
	}
	// 专属分组：需要在 AllowedGroups 中
	for _, id := range u.AllowedGroups {
		if id == groupID {
			return true
		}
	}
	return false
}

func (u *User) SetPassword(password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.PasswordHash = string(hash)
	return nil
}

func (u *User) CheckPassword(password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) == nil
}
