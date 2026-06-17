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

// CanBindGroup checks whether a user can bind to a given group based on its visibility.
//   - public:     all users can bind
//   - private:    only users with the group in AllowedGroups (admin-assigned) can bind
//   - subscriber: only users holding an active subscription whose plan is in the group's
//     visible-plan set can bind (OR semantics: any matching plan grants access).
//
// groupVisiblePlanIDs is the group's bound plan set; userActivePlanIDs is the set of plan
// IDs the user currently holds an active (non-expired) subscription for.
func (u *User) CanBindGroup(groupID int64, visibility string, groupVisiblePlanIDs, userActivePlanIDs []int64) bool {
	switch visibility {
	case VisibilityPublic:
		return true
	case VisibilityPrivate:
		// 专属分组：需要在 AllowedGroups 中（管理员单独授权）。
		for _, id := range u.AllowedGroups {
			if id == groupID {
				return true
			}
		}
		return false
	case VisibilitySubscriber:
		// 订阅会员可见：用户持有的有效订阅 plan 与分组绑定 plan 有交集即可见。
		if len(groupVisiblePlanIDs) == 0 || len(userActivePlanIDs) == 0 {
			return false
		}
		active := make(map[int64]struct{}, len(userActivePlanIDs))
		for _, pid := range userActivePlanIDs {
			active[pid] = struct{}{}
		}
		for _, pid := range groupVisiblePlanIDs {
			if _, ok := active[pid]; ok {
				return true
			}
		}
		return false
	default:
		// 未知可见性按最安全处理：不可见。
		return false
	}
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
