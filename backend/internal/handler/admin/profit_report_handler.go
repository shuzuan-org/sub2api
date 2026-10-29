package admin

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// profitReporter 供 handler 依赖的窄接口（便于测试 stub）。
type profitReporter interface {
	GetReport(ctx context.Context, startTime, endTime time.Time, userTZ string) (*service.ProfitReport, error)
}

// ProfitReportHandler 管理端网关收入看板：返回按 日×模型×账号×分组 聚合的
// 标价/实付，以及账号成本折扣与分组倍率维表，成本和毛利由前端按折扣实时计算。
type ProfitReportHandler struct {
	svc profitReporter
}

// NewProfitReportHandler 创建收入看板 handler。
func NewProfitReportHandler(svc *service.ProfitReportService) *ProfitReportHandler {
	return &ProfitReportHandler{svc: svc}
}

type profitReportRowDTO struct {
	Day                 string  `json:"day"`
	Model               string  `json:"model"`
	AccountID           int64   `json:"account_id"`
	GroupID             *int64  `json:"group_id"`
	Requests            int64   `json:"requests"`
	InputTokens         int64   `json:"input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	ListPrice           float64 `json:"list_price"`
	Revenue             float64 `json:"revenue"`
}

type profitReportAccountDTO struct {
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	Platform       string  `json:"platform"`
	RateMultiplier float64 `json:"rate_multiplier"`
	Status         string  `json:"status"`
	Schedulable    bool    `json:"schedulable"`
	Deleted        bool    `json:"deleted"`
	LastUsedAt     *string `json:"last_used_at"`
}

type profitReportGroupDTO struct {
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	Platform       string  `json:"platform"`
	RateMultiplier float64 `json:"rate_multiplier"`
	Deleted        bool    `json:"deleted"`
}

type profitReportDTO struct {
	GeneratedAt string                   `json:"generated_at"`
	StartDate   string                   `json:"start_date"`
	EndDate     string                   `json:"end_date"`
	Timezone    string                   `json:"timezone"`
	Rows        []profitReportRowDTO     `json:"rows"`
	Accounts    []profitReportAccountDTO `json:"accounts"`
	Groups      []profitReportGroupDTO   `json:"groups"`
}

// GetReport 返回收入看板数据。
// GET /api/v1/admin/profit-report?start_date=YYYY-MM-DD&end_date=YYYY-MM-DD&timezone=Asia/Shanghai
// 日期含义与仪表盘一致：按请求方时区的自然日，end_date 含当天。
func (h *ProfitReportHandler) GetReport(c *gin.Context) {
	startTime, endTime := parseTimeRange(c)
	report, err := h.svc.GetReport(c.Request.Context(), startTime, endTime, c.Query("timezone"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, toProfitReportDTO(report))
}

func toProfitReportDTO(r *service.ProfitReport) profitReportDTO {
	rows := make([]profitReportRowDTO, 0, len(r.Rows))
	for _, x := range r.Rows {
		rows = append(rows, profitReportRowDTO{
			Day:                 x.Day,
			Model:               x.Model,
			AccountID:           x.AccountID,
			GroupID:             x.GroupID,
			Requests:            x.Requests,
			InputTokens:         x.InputTokens,
			OutputTokens:        x.OutputTokens,
			CacheCreationTokens: x.CacheCreationTokens,
			CacheReadTokens:     x.CacheReadTokens,
			ListPrice:           x.ListPrice,
			Revenue:             x.Revenue,
		})
	}
	accounts := make([]profitReportAccountDTO, 0, len(r.Accounts))
	for _, a := range r.Accounts {
		dto := profitReportAccountDTO{
			ID:             a.ID,
			Name:           a.Name,
			Platform:       a.Platform,
			RateMultiplier: a.RateMultiplier,
			Status:         a.Status,
			Schedulable:    a.Schedulable,
			Deleted:        a.Deleted,
		}
		if a.LastUsedAt != nil {
			s := a.LastUsedAt.Format(time.RFC3339)
			dto.LastUsedAt = &s
		}
		accounts = append(accounts, dto)
	}
	groups := make([]profitReportGroupDTO, 0, len(r.Groups))
	for _, g := range r.Groups {
		groups = append(groups, profitReportGroupDTO{
			ID:             g.ID,
			Name:           g.Name,
			Platform:       g.Platform,
			RateMultiplier: g.RateMultiplier,
			Deleted:        g.Deleted,
		})
	}
	return profitReportDTO{
		GeneratedAt: r.GeneratedAt.Format(time.RFC3339),
		StartDate:   r.StartTime.Format("2006-01-02"),
		EndDate:     r.EndTime.Add(-24 * time.Hour).Format("2006-01-02"),
		Timezone:    r.Timezone,
		Rows:        rows,
		Accounts:    accounts,
		Groups:      groups,
	}
}
