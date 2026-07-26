package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

type UserSubscriptionRepository interface {
	Create(ctx context.Context, sub *UserSubscription) error
	GetByID(ctx context.Context, id int64) (*UserSubscription, error)
	GetByUserIDAndPlanID(ctx context.Context, userID, planID int64) (*UserSubscription, error)
	GetLatestByUserIDAndPlanID(ctx context.Context, userID, planID int64) (*UserSubscription, error)
	GetActiveByUserIDAndPlanID(ctx context.Context, userID, planID int64) (*UserSubscription, error)
	Update(ctx context.Context, sub *UserSubscription) error
	Delete(ctx context.Context, id int64) error

	ListByUserID(ctx context.Context, userID int64) ([]UserSubscription, error)
	ListActiveByUserID(ctx context.Context, userID int64) ([]UserSubscription, error)
	ListByPlanID(ctx context.Context, planID int64, params pagination.PaginationParams) ([]UserSubscription, *pagination.PaginationResult, error)
	List(ctx context.Context, params pagination.PaginationParams, userID, planID *int64, status, sortBy, sortOrder string) ([]UserSubscription, *pagination.PaginationResult, error)

	ExistsByUserIDAndPlanID(ctx context.Context, userID, planID int64) (bool, error)
	CountActiveByPlanID(ctx context.Context, planID int64) (int64, error)
	ExtendExpiry(ctx context.Context, subscriptionID int64, newExpiresAt time.Time) error
	UpdateStatus(ctx context.Context, subscriptionID int64, status string) error
	UpdateNotes(ctx context.Context, subscriptionID int64, notes string) error

	ActivateWindows(ctx context.Context, id int64, start time.Time) error
	ResetDailyUsage(ctx context.Context, id int64, newWindowStart time.Time) error
	ResetWeeklyUsage(ctx context.Context, id int64, newWindowStart time.Time) error
	ResetMonthlyUsage(ctx context.Context, id int64, newWindowStart time.Time) error
	IncrementUsage(ctx context.Context, id int64, costUSD float64) error
	// GetCurrentUsage 从 DB 读取指定订阅当前的用量（实时值，非缓存快照）。
	// 用于 FIFO 计费时计算非最后订阅的精确剩余容量，防止并发写入导致超额。
	GetCurrentUsage(ctx context.Context, id int64) (daily, weekly, monthly float64, err error)

	// BatchUpdateExpiredStatus 把已过期的 active 订阅批量置为 expired，
	// 返回更新条数和受影响的去重 userID 列表（供调用方失效计费缓存）。
	BatchUpdateExpiredStatus(ctx context.Context) (int64, []int64, error)
}
