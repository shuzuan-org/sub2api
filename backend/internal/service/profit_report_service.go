package service

import (
	"context"
	"fmt"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// profitReportMaxRangeDays 收入看板单次查询允许的最大天数。
// usage_logs 按 日×模型×账号×分组 聚合后每天只有几十行，一年以内都能一次返回。
const profitReportMaxRangeDays = 366

// ErrProfitReportRangeInvalid 起止时间倒置。
var ErrProfitReportRangeInvalid = infraerrors.BadRequest("PROFIT_REPORT_RANGE_INVALID", "start_date must be before end_date")

// ErrProfitReportRangeTooLarge 查询跨度超过上限。
var ErrProfitReportRangeTooLarge = infraerrors.BadRequest("PROFIT_REPORT_RANGE_TOO_LARGE", fmt.Sprintf("date range must not exceed %d days", profitReportMaxRangeDays))

// ProfitReportRow usage_logs 按 日 × 计费模型 × 账号 × 分组 的聚合行。
// ListPrice = Σ total_cost（模型标价），Revenue = Σ actual_cost（用户实付，已含分组/用户倍率）。
// 成本不在这里算：前端按 ListPrice × 账号成本折扣重算，折扣可现场试填。
type ProfitReportRow struct {
	Day                 string // YYYY-MM-DD，按请求方时区切日
	Model               string // lower(coalesce(billing_model, model))
	AccountID           int64  // 0 表示日志未关联账号
	GroupID             *int64 // nil 表示未关联分组
	Requests            int64
	InputTokens         int64
	OutputTokens        int64
	CacheCreationTokens int64
	CacheReadTokens     int64
	ListPrice           float64
	Revenue             float64
}

// ProfitReportAccount 供应商账号维表（含已软删/停调账号，便于历史日志对得上名字）。
type ProfitReportAccount struct {
	ID             int64
	Name           string
	Platform       string
	RateMultiplier float64 // accounts.rate_multiplier，即库中成本折扣
	Status         string
	Schedulable    bool
	Deleted        bool
	LastUsedAt     *time.Time
}

// ProfitReportGroup 分组维表，只取聚合行里出现过的分组。
type ProfitReportGroup struct {
	ID             int64
	Name           string
	Platform       string
	RateMultiplier float64
	Deleted        bool
}

// ProfitReport 收入看板一次查询的完整结果。
type ProfitReport struct {
	GeneratedAt time.Time
	StartTime   time.Time
	EndTime     time.Time // 开区间
	Timezone    string
	Rows        []ProfitReportRow
	Accounts    []ProfitReportAccount
	Groups      []ProfitReportGroup
}

// ProfitReportRepository 收入看板数据访问接口。
type ProfitReportRepository interface {
	// AggregateUsage 按 日×模型×账号×分组 聚合 [startTime, endTime) 内的 usage_logs。
	// tzName 为 IANA 时区名，用于切日；为空时由实现方取应用时区。
	AggregateUsage(ctx context.Context, startTime, endTime time.Time, tzName string) ([]ProfitReportRow, error)
	// ListAccounts 返回全部账号（含软删）。
	ListAccounts(ctx context.Context) ([]ProfitReportAccount, error)
	// ListGroupsByIDs 按 id 返回分组（含软删）。
	ListGroupsByIDs(ctx context.Context, ids []int64) ([]ProfitReportGroup, error)
}

// ProfitReportService 网关收入看板：把日志里的标价/实付与账号成本折扣放到一起，
// 由前端按折扣实时算成本与毛利。
type ProfitReportService struct {
	repo ProfitReportRepository
}

// NewProfitReportService 创建收入看板服务。
func NewProfitReportService(repo ProfitReportRepository) *ProfitReportService {
	return &ProfitReportService{repo: repo}
}

// GetReport 返回时间范围内的聚合行与账号/分组维表。
// userTZ 是请求方的 IANA 时区名；无法加载时退回空串，由仓储层用应用时区切日。
func (s *ProfitReportService) GetReport(ctx context.Context, startTime, endTime time.Time, userTZ string) (*ProfitReport, error) {
	if !startTime.Before(endTime) {
		return nil, ErrProfitReportRangeInvalid
	}
	if endTime.Sub(startTime) > time.Duration(profitReportMaxRangeDays)*24*time.Hour {
		return nil, ErrProfitReportRangeTooLarge
	}

	tzName := ""
	if userTZ != "" {
		if _, err := time.LoadLocation(userTZ); err == nil {
			tzName = userTZ
		}
	}

	rows, err := s.repo.AggregateUsage(ctx, startTime, endTime, tzName)
	if err != nil {
		return nil, fmt.Errorf("aggregate usage for profit report: %w", err)
	}
	accounts, err := s.repo.ListAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list accounts for profit report: %w", err)
	}

	groupIDs := make([]int64, 0, 8)
	seen := make(map[int64]struct{})
	for _, r := range rows {
		if r.GroupID == nil {
			continue
		}
		if _, ok := seen[*r.GroupID]; ok {
			continue
		}
		seen[*r.GroupID] = struct{}{}
		groupIDs = append(groupIDs, *r.GroupID)
	}
	groups := []ProfitReportGroup{}
	if len(groupIDs) > 0 {
		groups, err = s.repo.ListGroupsByIDs(ctx, groupIDs)
		if err != nil {
			return nil, fmt.Errorf("list groups for profit report: %w", err)
		}
	}

	if rows == nil {
		rows = []ProfitReportRow{}
	}
	if accounts == nil {
		accounts = []ProfitReportAccount{}
	}
	return &ProfitReport{
		GeneratedAt: time.Now(),
		StartTime:   startTime,
		EndTime:     endTime,
		Timezone:    tzName,
		Rows:        rows,
		Accounts:    accounts,
		Groups:      groups,
	}, nil
}
