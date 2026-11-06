package service

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// 月度对账：把 usage_logs 按 自然月×账号×计费模型 汇总，和财务每月导入的供应商账单放到一起。
// 合并与核对规则在前端（frontend/src/utils/profitReconcile.ts）里算，后端只给原材料并负责存账单。

// ReconcileTimezone 对账切月固定按北京时间：供应商账单都是按北京时间的小时出账，
// 不能随看报表的人浏览器时区漂移。
const ReconcileTimezone = "Asia/Shanghai"

// ReconcileAdjustmentModel 账单里整笔调账行的「标准模型」取值。
const ReconcileAdjustmentModel = "调账"

const (
	reconcileMaxMonths    = 24
	reconcileMaxBillLines = 5000
	reconcileMaxShortText = 200
	reconcileMaxLongText  = 2000
)

var (
	ErrReconcilePeriodInvalid  = infraerrors.BadRequest("RECONCILE_PERIOD_INVALID", "period must be YYYY-MM and start_period must not be after end_period")
	ErrReconcileRangeTooLarge  = infraerrors.BadRequest("RECONCILE_RANGE_TOO_LARGE", fmt.Sprintf("period range must not exceed %d months", reconcileMaxMonths))
	ErrReconcileBindingInvalid = infraerrors.BadRequest("RECONCILE_BINDING_INVALID", "supplier and supplier_account must both be set (or both empty to unbind), at most 200 characters")
	ErrReconcileBillsInvalid   = infraerrors.BadRequest("RECONCILE_BILLS_INVALID", "invalid supplier bill lines")
)

// ReconcileUsageRow usage_logs 按 自然月 × 计费模型 × 账号 的聚合行，口径同 ProfitReportRow。
type ReconcileUsageRow struct {
	Period              string // YYYY-MM
	Model               string // lower(coalesce(billing_model, model))
	AccountID           int64
	Requests            int64
	InputTokens         int64
	OutputTokens        int64
	CacheCreationTokens int64
	CacheReadTokens     int64
	ListPrice           float64 // U
	Revenue             float64 // U
}

// SupplierBinding sub2api 账号 → 供应商账号。
type SupplierBinding struct {
	AccountID       int64
	Supplier        string
	SupplierAccount string
	UpdatedAt       time.Time
}

// SupplierBillLine 财务导入的一行供应商账单（数值为 nil 表示留空）。
type SupplierBillLine struct {
	ID               int64
	Period           string
	Supplier         string
	SupplierAccount  string
	StandardModel    string
	PlatformModels   string
	CostDiscount     *float64
	Currency         string
	InputTokens      *float64
	OutputTokens     *float64
	CacheReadTokens  *float64
	CacheWriteTokens *float64
	OtherTokens      *float64
	ResourceFee      *float64
	DiscountAmount   *float64
	Adjustment       *float64
	Tax              *float64
	FXRate           *float64
	Note             string
	UpdatedAt        time.Time
}

// ProfitReconcile 一次月度对账查询的原材料。
type ProfitReconcile struct {
	GeneratedAt time.Time
	StartPeriod string
	EndPeriod   string
	Timezone    string
	Rows        []ReconcileUsageRow
	Accounts    []ProfitReportAccount
	Bindings    []SupplierBinding
	Bills       []SupplierBillLine
}

// SupplierBillRepository 月度对账的数据访问。
type SupplierBillRepository interface {
	// AggregateUsageMonthly 按 自然月×模型×账号 聚合 [startTime, endTime) 内的 usage_logs，tzName 用于切月。
	AggregateUsageMonthly(ctx context.Context, startTime, endTime time.Time, tzName string) ([]ReconcileUsageRow, error)
	ListBindings(ctx context.Context) ([]SupplierBinding, error)
	UpsertBinding(ctx context.Context, b SupplierBinding) error
	DeleteBinding(ctx context.Context, accountID int64) error
	// ListBills 返回 [startPeriod, endPeriod] 闭区间内的账单行。
	ListBills(ctx context.Context, startPeriod, endPeriod string) ([]SupplierBillLine, error)
	// ReplaceBills 在一个事务里删掉 periods 的全部旧行，再写入 lines。
	ReplaceBills(ctx context.Context, periods []string, lines []SupplierBillLine) error
}

// parsePeriod 解析 YYYY-MM。
func parsePeriod(p string) (time.Time, bool) {
	if len(p) != 7 {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01", p)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// GetReconcile 返回 [startPeriod, endPeriod] 每个北京时间自然月的后台用量、账号、绑定与账单。
func (s *ProfitReportService) GetReconcile(ctx context.Context, startPeriod, endPeriod string) (*ProfitReconcile, error) {
	ps, ok1 := parsePeriod(startPeriod)
	pe, ok2 := parsePeriod(endPeriod)
	if !ok1 || !ok2 || pe.Before(ps) {
		return nil, ErrReconcilePeriodInvalid
	}
	months := (pe.Year()-ps.Year())*12 + int(pe.Month()-ps.Month()) + 1
	if months > reconcileMaxMonths {
		return nil, ErrReconcileRangeTooLarge
	}
	loc, err := time.LoadLocation(ReconcileTimezone)
	if err != nil {
		return nil, fmt.Errorf("load reconcile timezone: %w", err)
	}
	startTime := time.Date(ps.Year(), ps.Month(), 1, 0, 0, 0, 0, loc)
	endTime := time.Date(pe.Year(), pe.Month(), 1, 0, 0, 0, 0, loc).AddDate(0, 1, 0)

	rows, err := s.bills.AggregateUsageMonthly(ctx, startTime, endTime, ReconcileTimezone)
	if err != nil {
		return nil, fmt.Errorf("aggregate monthly usage for reconcile: %w", err)
	}
	accounts, err := s.repo.ListAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list accounts for reconcile: %w", err)
	}
	bindings, err := s.bills.ListBindings(ctx)
	if err != nil {
		return nil, fmt.Errorf("list supplier bindings: %w", err)
	}
	bills, err := s.bills.ListBills(ctx, startPeriod, endPeriod)
	if err != nil {
		return nil, fmt.Errorf("list supplier bills: %w", err)
	}
	if rows == nil {
		rows = []ReconcileUsageRow{}
	}
	if accounts == nil {
		accounts = []ProfitReportAccount{}
	}
	if bindings == nil {
		bindings = []SupplierBinding{}
	}
	if bills == nil {
		bills = []SupplierBillLine{}
	}
	return &ProfitReconcile{
		GeneratedAt: time.Now(),
		StartPeriod: startPeriod,
		EndPeriod:   endPeriod,
		Timezone:    ReconcileTimezone,
		Rows:        rows,
		Accounts:    accounts,
		Bindings:    bindings,
		Bills:       bills,
	}, nil
}

// SaveBinding 绑定或解绑（两个字段都为空即解绑）。
func (s *ProfitReportService) SaveBinding(ctx context.Context, accountID int64, supplier, supplierAccount string) error {
	supplier = strings.TrimSpace(supplier)
	supplierAccount = strings.TrimSpace(supplierAccount)
	if accountID <= 0 {
		return ErrReconcileBindingInvalid
	}
	if supplier == "" && supplierAccount == "" {
		return s.bills.DeleteBinding(ctx, accountID)
	}
	if supplier == "" || supplierAccount == "" ||
		utf8.RuneCountInString(supplier) > 100 || utf8.RuneCountInString(supplierAccount) > reconcileMaxShortText {
		return ErrReconcileBindingInvalid
	}
	return s.bills.UpsertBinding(ctx, SupplierBinding{AccountID: accountID, Supplier: supplier, SupplierAccount: supplierAccount})
}

func billsInvalid(format string, args ...any) error {
	// Reason 与 ErrReconcileBillsInvalid 相同，errors.Is 仍成立；message 带上具体哪一行错了。
	return infraerrors.BadRequest(ErrReconcileBillsInvalid.Reason, "invalid supplier bill lines: "+fmt.Sprintf(format, args...))
}

// ReplaceBills 用导入的账单整月覆盖 periods（每个账期先删后写，同一事务）。
func (s *ProfitReportService) ReplaceBills(ctx context.Context, periods []string, lines []SupplierBillLine) error {
	if len(periods) == 0 {
		return billsInvalid("periods is empty")
	}
	if len(periods) > reconcileMaxMonths {
		return billsInvalid("too many periods: %d", len(periods))
	}
	if len(lines) > reconcileMaxBillLines {
		return billsInvalid("too many lines: %d (max %d)", len(lines), reconcileMaxBillLines)
	}
	allowed := make(map[string]struct{}, len(periods))
	for _, p := range periods {
		if _, ok := parsePeriod(p); !ok {
			return billsInvalid("bad period %q", p)
		}
		allowed[p] = struct{}{}
	}
	uniq := make([]string, 0, len(allowed))
	for p := range allowed {
		uniq = append(uniq, p)
	}
	for i := range lines {
		l := &lines[i]
		l.Period = strings.TrimSpace(l.Period)
		l.Supplier = strings.TrimSpace(l.Supplier)
		l.SupplierAccount = strings.TrimSpace(l.SupplierAccount)
		l.StandardModel = strings.TrimSpace(l.StandardModel)
		l.PlatformModels = strings.TrimSpace(l.PlatformModels)
		l.Currency = strings.TrimSpace(l.Currency)
		row := i + 1
		if _, ok := allowed[l.Period]; !ok {
			return billsInvalid("line %d: period %q not in periods", row, l.Period)
		}
		if l.StandardModel == "" {
			return billsInvalid("line %d: standard model is empty", row)
		}
		if utf8.RuneCountInString(l.Supplier) > 100 || utf8.RuneCountInString(l.SupplierAccount) > reconcileMaxShortText ||
			utf8.RuneCountInString(l.StandardModel) > reconcileMaxShortText || utf8.RuneCountInString(l.Currency) > 16 ||
			utf8.RuneCountInString(l.PlatformModels) > reconcileMaxLongText || utf8.RuneCountInString(l.Note) > reconcileMaxLongText {
			return billsInvalid("line %d: text too long", row)
		}
		if l.CostDiscount != nil && (*l.CostDiscount < 0 || *l.CostDiscount > 100) {
			return billsInvalid("line %d: cost discount out of range", row)
		}
		if l.FXRate != nil && *l.FXRate < 0 {
			return billsInvalid("line %d: fx rate is negative", row)
		}
	}
	return s.bills.ReplaceBills(ctx, uniq, lines)
}
