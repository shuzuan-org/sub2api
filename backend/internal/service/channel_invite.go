package service

import (
	"time"
)

// ChannelInviteBatch 渠道邀请码批次
type ChannelInviteBatch struct {
	ID             int64
	Name           string
	BonusAmount    float64
	MaxUsesPerCode int
	StartTime      *time.Time
	EndTime        *time.Time
	Status         string
	Notes          string
	CreatedBy      int64
	CreatedAt      time.Time
	UpdatedAt      time.Time

	// 关联
	Groups    []Group
	Codes     []ChannelInviteCode
	Usages    []ChannelInviteCodeUsage
	CodeCount int
	UsedCount int
	Creator   *User
}

// ChannelInviteCode 渠道邀请码（个体码）
type ChannelInviteCode struct {
	ID        int64
	BatchID   int64
	Code      string
	Status    string
	MaxUses   int
	UsedCount int
	CreatedAt time.Time
	UpdatedAt time.Time

	// 关联
	Batch  *ChannelInviteBatch
	Usages []ChannelInviteCodeUsage
}

// ChannelInviteCodeUsage 渠道邀请码使用记录
type ChannelInviteCodeUsage struct {
	ID             int64
	CodeID         int64
	BatchID        int64
	UserID         int64
	BonusGranted   bool
	BonusGrantedAt *time.Time
	ClaimedAt      time.Time

	// 关联
	Code  *ChannelInviteCode
	Batch *ChannelInviteBatch
	User  *User
}

// CanClaim 检查邀请码是否可兑换
func (c *ChannelInviteCode) CanClaim() bool {
	if c.Status != ChannelInviteCodeStatusUnused {
		return false
	}
	if c.MaxUses > 0 && c.UsedCount >= c.MaxUses {
		return false
	}
	return true
}

// IsActive 检查批次是否活跃
func (b *ChannelInviteBatch) IsActive() bool {
	if b.Status != ChannelInviteBatchStatusActive {
		return false
	}
	now := time.Now()
	if b.StartTime != nil && now.Before(*b.StartTime) {
		return false
	}
	if b.EndTime != nil && now.After(*b.EndTime) {
		return false
	}
	return true
}

// CreateChannelInviteBatchInput 创建批次输入
type CreateChannelInviteBatchInput struct {
	Name           string
	BonusAmount    float64
	MaxUsesPerCode int
	StartTime      *time.Time
	EndTime        *time.Time
	Notes          string
	CreatedBy      int64
	GroupIDs       []int64
}

// UpdateChannelInviteBatchInput 更新批次输入
type UpdateChannelInviteBatchInput struct {
	Name           *string
	BonusAmount    *float64
	MaxUsesPerCode *int
	StartTime      *time.Time
	EndTime        *time.Time
	Status         *string
	Notes          *string
	GroupIDs       []int64 // 非空时替换全部分组关联
}
