package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

// ChannelInviteRepository 渠道邀请码仓储接口
type ChannelInviteRepository interface {
	// 批次 CRUD
	CreateBatch(ctx context.Context, batch *ChannelInviteBatch, groupIDs []int64) error
	GetBatch(ctx context.Context, id int64) (*ChannelInviteBatch, error)
	UpdateBatch(ctx context.Context, id int64, input *UpdateChannelInviteBatchInput) error
	DeleteBatch(ctx context.Context, id int64) error
	ListBatches(ctx context.Context, params pagination.PaginationParams, status, search string) ([]ChannelInviteBatch, *pagination.PaginationResult, error)

	// 码操作
	CreateCodes(ctx context.Context, codes []ChannelInviteCode) error
	GetCodeByID(ctx context.Context, id int64) (*ChannelInviteCode, error)
	GetCodeByCode(ctx context.Context, code string) (*ChannelInviteCode, error)
	GetCodeByCodeForUpdate(ctx context.Context, code string) (*ChannelInviteCode, error) // 带行锁
	ListCodes(ctx context.Context, batchID int64, params pagination.PaginationParams, status, search string) ([]ChannelInviteCode, *pagination.PaginationResult, error)
	IncrementUsedCount(ctx context.Context, id int64) error

	// 使用记录
	CreateUsage(ctx context.Context, usage *ChannelInviteCodeUsage) error
	GetUsageByCodeAndUser(ctx context.Context, codeID, userID int64) (*ChannelInviteCodeUsage, error)
	ListUsagesByBatch(ctx context.Context, batchID int64, params pagination.PaginationParams) ([]ChannelInviteCodeUsage, *pagination.PaginationResult, error)
	GrantBonus(ctx context.Context, usageID int64, bonusAmount float64) error
	ListPendingBonusByUser(ctx context.Context, userID int64) ([]ChannelInviteCodeUsage, error)

	// 批次分组关联
	GetBatchGroupIDs(ctx context.Context, batchID int64) ([]int64, error)
	ReplaceBatchGroups(ctx context.Context, batchID int64, groupIDs []int64) error

	// 批量计数
	GetBatchCodeStats(ctx context.Context, batchID int64) (codeCount, usedCount int, err error)
}
