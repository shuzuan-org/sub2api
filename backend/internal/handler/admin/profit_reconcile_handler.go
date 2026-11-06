package admin

import (
	"context"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// profitReconciler 月度对账的窄接口（便于测试 stub）。
type profitReconciler interface {
	GetReconcile(ctx context.Context, startPeriod, endPeriod string) (*service.ProfitReconcile, error)
	SaveBinding(ctx context.Context, accountID int64, supplier, supplierAccount string) error
	ReplaceBills(ctx context.Context, periods []string, lines []service.SupplierBillLine) error
}

type reconcileUsageRowDTO struct {
	Period              string  `json:"period"`
	Model               string  `json:"model"`
	AccountID           int64   `json:"account_id"`
	Requests            int64   `json:"requests"`
	InputTokens         int64   `json:"input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	ListPrice           float64 `json:"list_price"`
	Revenue             float64 `json:"revenue"`
}

type supplierBindingDTO struct {
	AccountID       int64  `json:"account_id"`
	Supplier        string `json:"supplier"`
	SupplierAccount string `json:"supplier_account"`
	UpdatedAt       string `json:"updated_at"`
}

// supplierBillLineDTO 数值字段为 null 表示账单上留空（与 0 区分）。
type supplierBillLineDTO struct {
	Period           string   `json:"period"`
	Supplier         string   `json:"supplier"`
	SupplierAccount  string   `json:"supplier_account"`
	StandardModel    string   `json:"standard_model"`
	PlatformModels   string   `json:"platform_models"`
	CostDiscount     *float64 `json:"cost_discount"`
	Currency         string   `json:"currency"`
	InputTokens      *float64 `json:"input_tokens"`
	OutputTokens     *float64 `json:"output_tokens"`
	CacheReadTokens  *float64 `json:"cache_read_tokens"`
	CacheWriteTokens *float64 `json:"cache_write_tokens"`
	OtherTokens      *float64 `json:"other_tokens"`
	ResourceFee      *float64 `json:"resource_fee"`
	DiscountAmount   *float64 `json:"discount_amount"`
	Adjustment       *float64 `json:"adjustment"`
	Tax              *float64 `json:"tax"`
	FXRate           *float64 `json:"fx_rate"`
	Note             string   `json:"note"`
	UpdatedAt        string   `json:"updated_at,omitempty"`
}

type profitReconcileDTO struct {
	GeneratedAt string                   `json:"generated_at"`
	StartPeriod string                   `json:"start_period"`
	EndPeriod   string                   `json:"end_period"`
	Timezone    string                   `json:"timezone"`
	Rows        []reconcileUsageRowDTO   `json:"rows"`
	Accounts    []profitReportAccountDTO `json:"accounts"`
	Bindings    []supplierBindingDTO     `json:"bindings"`
	Bills       []supplierBillLineDTO    `json:"bills"`
}

// GetReconcile 返回月度对账原材料。
// GET /api/v1/admin/profit-report/reconcile?start_period=YYYY-MM&end_period=YYYY-MM
func (h *ProfitReportHandler) GetReconcile(c *gin.Context) {
	start := c.Query("start_period")
	end := c.Query("end_period")
	if end == "" {
		end = start
	}
	rec, err := h.rec.GetReconcile(c.Request.Context(), start, end)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, toProfitReconcileDTO(rec))
}

type saveSupplierBindingRequest struct {
	Supplier        string `json:"supplier"`
	SupplierAccount string `json:"supplier_account"`
}

// SaveSupplierBinding 绑定 / 解绑（两个字段都为空即解绑）。
// PUT /api/v1/admin/profit-report/supplier-bindings/:account_id
func (h *ProfitReportHandler) SaveSupplierBinding(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("account_id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "invalid account_id")
		return
	}
	var req saveSupplierBindingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	if err := h.rec.SaveBinding(c.Request.Context(), accountID, req.Supplier, req.SupplierAccount); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"account_id": accountID})
}

type replaceSupplierBillsRequest struct {
	Periods []string              `json:"periods"`
	Lines   []supplierBillLineDTO `json:"lines"`
}

// ReplaceSupplierBills 导入账单：periods 内的旧账单整月删除后写入 lines。
// PUT /api/v1/admin/profit-report/supplier-bills
func (h *ProfitReportHandler) ReplaceSupplierBills(c *gin.Context) {
	var req replaceSupplierBillsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	lines := make([]service.SupplierBillLine, 0, len(req.Lines))
	for _, l := range req.Lines {
		lines = append(lines, service.SupplierBillLine{
			Period: l.Period, Supplier: l.Supplier, SupplierAccount: l.SupplierAccount,
			StandardModel: l.StandardModel, PlatformModels: l.PlatformModels,
			CostDiscount: l.CostDiscount, Currency: l.Currency,
			InputTokens: l.InputTokens, OutputTokens: l.OutputTokens, CacheReadTokens: l.CacheReadTokens,
			CacheWriteTokens: l.CacheWriteTokens, OtherTokens: l.OtherTokens,
			ResourceFee: l.ResourceFee, DiscountAmount: l.DiscountAmount, Adjustment: l.Adjustment,
			Tax: l.Tax, FXRate: l.FXRate, Note: l.Note,
		})
	}
	if err := h.rec.ReplaceBills(c.Request.Context(), req.Periods, lines); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"periods": req.Periods, "lines": len(lines)})
}

func toProfitReconcileDTO(r *service.ProfitReconcile) profitReconcileDTO {
	rows := make([]reconcileUsageRowDTO, 0, len(r.Rows))
	for _, x := range r.Rows {
		rows = append(rows, reconcileUsageRowDTO{
			Period: x.Period, Model: x.Model, AccountID: x.AccountID, Requests: x.Requests,
			InputTokens: x.InputTokens, OutputTokens: x.OutputTokens,
			CacheCreationTokens: x.CacheCreationTokens, CacheReadTokens: x.CacheReadTokens,
			ListPrice: x.ListPrice, Revenue: x.Revenue,
		})
	}
	// 账号维表与收入看板同构，复用其 DTO 转换。
	accounts := toProfitReportDTO(&service.ProfitReport{Accounts: r.Accounts}).Accounts
	bindings := make([]supplierBindingDTO, 0, len(r.Bindings))
	for _, b := range r.Bindings {
		bindings = append(bindings, supplierBindingDTO{
			AccountID: b.AccountID, Supplier: b.Supplier, SupplierAccount: b.SupplierAccount,
			UpdatedAt: b.UpdatedAt.Format(time.RFC3339),
		})
	}
	bills := make([]supplierBillLineDTO, 0, len(r.Bills))
	for _, l := range r.Bills {
		bills = append(bills, supplierBillLineDTO{
			Period: l.Period, Supplier: l.Supplier, SupplierAccount: l.SupplierAccount,
			StandardModel: l.StandardModel, PlatformModels: l.PlatformModels,
			CostDiscount: l.CostDiscount, Currency: l.Currency,
			InputTokens: l.InputTokens, OutputTokens: l.OutputTokens, CacheReadTokens: l.CacheReadTokens,
			CacheWriteTokens: l.CacheWriteTokens, OtherTokens: l.OtherTokens,
			ResourceFee: l.ResourceFee, DiscountAmount: l.DiscountAmount, Adjustment: l.Adjustment,
			Tax: l.Tax, FXRate: l.FXRate, Note: l.Note,
			UpdatedAt: l.UpdatedAt.Format(time.RFC3339),
		})
	}
	return profitReconcileDTO{
		GeneratedAt: r.GeneratedAt.Format(time.RFC3339),
		StartPeriod: r.StartPeriod,
		EndPeriod:   r.EndPeriod,
		Timezone:    r.Timezone,
		Rows:        rows,
		Accounts:    accounts,
		Bindings:    bindings,
		Bills:       bills,
	}
}
