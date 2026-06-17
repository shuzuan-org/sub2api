package service

import (
	"context"
	"fmt"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

var (
	ErrGroupNotFound = infraerrors.NotFound("GROUP_NOT_FOUND", "group not found")
	ErrGroupExists   = infraerrors.Conflict("GROUP_EXISTS", "group name already exists")
)

type GroupRepository interface {
	Create(ctx context.Context, group *Group) error
	GetByID(ctx context.Context, id int64) (*Group, error)
	GetByIDLite(ctx context.Context, id int64) (*Group, error)
	Update(ctx context.Context, group *Group) error
	Delete(ctx context.Context, id int64) error
	DeleteCascade(ctx context.Context, id int64) ([]int64, error)

	List(ctx context.Context, params pagination.PaginationParams) ([]Group, *pagination.PaginationResult, error)
	ListWithFilters(ctx context.Context, params pagination.PaginationParams, platform, status, search, visibility string) ([]Group, *pagination.PaginationResult, error)
	ListActive(ctx context.Context) ([]Group, error)
	ListActiveByPlatform(ctx context.Context, platform string) ([]Group, error)

	ExistsByName(ctx context.Context, name string) (bool, error)
	GetAccountCount(ctx context.Context, groupID int64) (total int64, active int64, err error)
	DeleteAccountGroupsByGroupID(ctx context.Context, groupID int64) (int64, error)
	// GetAccountIDsByGroupIDs 获取多个分组的所有账号 ID（去重）
	GetAccountIDsByGroupIDs(ctx context.Context, groupIDs []int64) ([]int64, error)
	// BindAccountsToGroup 将多个账号绑定到指定分组
	BindAccountsToGroup(ctx context.Context, groupID int64, accountIDs []int64) error
	// UpdateSortOrders 批量更新分组排序
	UpdateSortOrders(ctx context.Context, updates []GroupSortOrderUpdate) error

	// LoadVisiblePlansByGroupIDs 批量加载分组绑定的订阅计划 ID（subscriber 可见性用）。
	LoadVisiblePlansByGroupIDs(ctx context.Context, groupIDs []int64) (map[int64][]int64, error)
	// SetVisiblePlans 全量替换某分组绑定的订阅计划集合。
	SetVisiblePlans(ctx context.Context, groupID int64, planIDs []int64) error
}

// GroupSortOrderUpdate 分组排序更新
type GroupSortOrderUpdate struct {
	ID        int64 `json:"id"`
	SortOrder int   `json:"sort_order"`
}

// CreateGroupRequest 创建分组请求
type CreateGroupRequest struct {
	Name           string  `json:"name"`
	Description    string  `json:"description"`
	RateMultiplier float64 `json:"rate_multiplier"`
	// Visibility 可见性三档：public / subscriber / private。空值时由 IsExclusive 回退派生。
	Visibility string `json:"visibility"`
	// IsExclusive 已废弃：仅当 Visibility 为空时用于兼容回退（true→private，false→public）。
	IsExclusive bool `json:"is_exclusive"`
	// VisiblePlanIDs 当 Visibility==subscriber 时绑定的订阅计划集合。
	VisiblePlanIDs []int64 `json:"visible_plan_ids"`
}

// UpdateGroupRequest 更新分组请求
type UpdateGroupRequest struct {
	Name           *string  `json:"name"`
	Description    *string  `json:"description"`
	RateMultiplier *float64 `json:"rate_multiplier"`
	// Visibility 可见性三档；与 IsExclusive 二选一（优先 Visibility）。
	Visibility *string `json:"visibility"`
	// IsExclusive 已废弃：仅当 Visibility 未提供时用于兼容回退。
	IsExclusive *bool `json:"is_exclusive"`
	// VisiblePlanIDs 非 nil 时全量替换分组绑定的订阅计划集合。
	VisiblePlanIDs *[]int64 `json:"visible_plan_ids"`
	Status         *string  `json:"status"`
}

// ResolveVisibility 将请求中的 visibility/is_exclusive 归一为三档枚举值。
// 优先 visibility；为空时由 is_exclusive 回退（true→private，false→public）。
func ResolveVisibility(visibility string, isExclusive bool) string {
	switch visibility {
	case VisibilityPublic, VisibilitySubscriber, VisibilityPrivate:
		return visibility
	default:
		if isExclusive {
			return VisibilityPrivate
		}
		return VisibilityPublic
	}
}

// GroupService 分组管理服务
type GroupService struct {
	groupRepo            GroupRepository
	authCacheInvalidator APIKeyAuthCacheInvalidator
}

// NewGroupService 创建分组服务实例
func NewGroupService(groupRepo GroupRepository, authCacheInvalidator APIKeyAuthCacheInvalidator) *GroupService {
	return &GroupService{
		groupRepo:            groupRepo,
		authCacheInvalidator: authCacheInvalidator,
	}
}

// Create 创建分组
func (s *GroupService) Create(ctx context.Context, req CreateGroupRequest) (*Group, error) {
	// 检查名称是否已存在
	exists, err := s.groupRepo.ExistsByName(ctx, req.Name)
	if err != nil {
		return nil, fmt.Errorf("check group exists: %w", err)
	}
	if exists {
		return nil, ErrGroupExists
	}

	// 解析三档可见性（优先 Visibility，回退 IsExclusive）
	visibility := ResolveVisibility(req.Visibility, req.IsExclusive)

	// 创建分组
	group := &Group{
		Name:           req.Name,
		Description:    req.Description,
		Platform:       PlatformAnthropic,
		RateMultiplier: req.RateMultiplier,
		Visibility:     visibility,
		IsExclusive:    visibility == VisibilityPrivate,
		Status:         StatusActive,
	}

	if err := s.groupRepo.Create(ctx, group); err != nil {
		return nil, fmt.Errorf("create group: %w", err)
	}

	// subscriber 可见性：写入绑定的订阅计划集合。
	if visibility == VisibilitySubscriber && len(req.VisiblePlanIDs) > 0 {
		if err := s.groupRepo.SetVisiblePlans(ctx, group.ID, req.VisiblePlanIDs); err != nil {
			return nil, fmt.Errorf("set visible plans: %w", err)
		}
		group.VisiblePlanIDs = req.VisiblePlanIDs
	}

	return group, nil
}

// GetByID 根据ID获取分组
func (s *GroupService) GetByID(ctx context.Context, id int64) (*Group, error) {
	group, err := s.groupRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get group: %w", err)
	}
	return group, nil
}

// List 获取分组列表
func (s *GroupService) List(ctx context.Context, params pagination.PaginationParams) ([]Group, *pagination.PaginationResult, error) {
	groups, pagination, err := s.groupRepo.List(ctx, params)
	if err != nil {
		return nil, nil, fmt.Errorf("list groups: %w", err)
	}
	return groups, pagination, nil
}

// ListActive 获取活跃分组列表
func (s *GroupService) ListActive(ctx context.Context) ([]Group, error) {
	groups, err := s.groupRepo.ListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active groups: %w", err)
	}
	return groups, nil
}

// Update 更新分组
func (s *GroupService) Update(ctx context.Context, id int64, req UpdateGroupRequest) (*Group, error) {
	group, err := s.groupRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get group: %w", err)
	}

	// 更新字段
	if req.Name != nil && *req.Name != group.Name {
		// 检查新名称是否已存在
		exists, err := s.groupRepo.ExistsByName(ctx, *req.Name)
		if err != nil {
			return nil, fmt.Errorf("check group exists: %w", err)
		}
		if exists {
			return nil, ErrGroupExists
		}
		group.Name = *req.Name
	}

	if req.Description != nil {
		group.Description = *req.Description
	}

	if req.RateMultiplier != nil {
		group.RateMultiplier = *req.RateMultiplier
	}

	// 可见性：优先 Visibility，回退 IsExclusive。
	if req.Visibility != nil {
		group.Visibility = ResolveVisibility(*req.Visibility, false)
	} else if req.IsExclusive != nil {
		group.Visibility = ResolveVisibility("", *req.IsExclusive)
	}
	group.IsExclusive = group.Visibility == VisibilityPrivate

	if req.Status != nil {
		group.Status = *req.Status
	}

	if err := s.groupRepo.Update(ctx, group); err != nil {
		return nil, fmt.Errorf("update group: %w", err)
	}

	// subscriber 可见性绑定计划：非 nil 即全量替换。非 subscriber 档则清空绑定。
	if req.VisiblePlanIDs != nil {
		planIDs := *req.VisiblePlanIDs
		if group.Visibility != VisibilitySubscriber {
			planIDs = nil
		}
		if err := s.groupRepo.SetVisiblePlans(ctx, id, planIDs); err != nil {
			return nil, fmt.Errorf("set visible plans: %w", err)
		}
		group.VisiblePlanIDs = planIDs
	} else if group.Visibility != VisibilitySubscriber {
		// 切换到非 subscriber 档时，清理残留绑定。
		if err := s.groupRepo.SetVisiblePlans(ctx, id, nil); err != nil {
			return nil, fmt.Errorf("clear visible plans: %w", err)
		}
		group.VisiblePlanIDs = nil
	}

	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByGroupID(ctx, id)
	}

	return group, nil
}

// Delete 删除分组
func (s *GroupService) Delete(ctx context.Context, id int64) error {
	// 检查分组是否存在
	_, err := s.groupRepo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get group: %w", err)
	}

	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByGroupID(ctx, id)
	}
	if err := s.groupRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete group: %w", err)
	}

	return nil
}

// GetStats 获取分组统计信息
func (s *GroupService) GetStats(ctx context.Context, id int64) (map[string]any, error) {
	group, err := s.groupRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get group: %w", err)
	}

	// 获取账号数量
	accountCount, _, err := s.groupRepo.GetAccountCount(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get account count: %w", err)
	}

	stats := map[string]any{
		"id":              group.ID,
		"name":            group.Name,
		"rate_multiplier": group.RateMultiplier,
		"visibility":      group.Visibility,
		"is_exclusive":    group.IsExclusive,
		"status":          group.Status,
		"account_count":   accountCount,
	}

	return stats, nil
}
