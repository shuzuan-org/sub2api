package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type stubSupplierBillRepo struct {
	rows       []ReconcileUsageRow
	bindings   []SupplierBinding
	bills      []SupplierBillLine
	gotStart   time.Time
	gotEnd     time.Time
	gotTZ      string
	gotBillsLo string
	gotBillsHi string
	upserted   []SupplierBinding
	deleted    []int64
	gotPeriods []string
	gotLines   []SupplierBillLine
	replaceErr error
}

func (s *stubSupplierBillRepo) AggregateUsageMonthly(_ context.Context, start, end time.Time, tz string) ([]ReconcileUsageRow, error) {
	s.gotStart, s.gotEnd, s.gotTZ = start, end, tz
	return s.rows, nil
}
func (s *stubSupplierBillRepo) ListBindings(context.Context) ([]SupplierBinding, error) {
	return s.bindings, nil
}
func (s *stubSupplierBillRepo) UpsertBinding(_ context.Context, b SupplierBinding) error {
	s.upserted = append(s.upserted, b)
	return nil
}
func (s *stubSupplierBillRepo) DeleteBinding(_ context.Context, id int64) error {
	s.deleted = append(s.deleted, id)
	return nil
}
func (s *stubSupplierBillRepo) ListBills(_ context.Context, lo, hi string) ([]SupplierBillLine, error) {
	s.gotBillsLo, s.gotBillsHi = lo, hi
	return s.bills, nil
}
func (s *stubSupplierBillRepo) ReplaceBills(_ context.Context, periods []string, lines []SupplierBillLine) error {
	s.gotPeriods, s.gotLines = periods, lines
	return s.replaceErr
}

func TestGetReconcile_MonthBoundariesInBeijingTime(t *testing.T) {
	bills := &stubSupplierBillRepo{}
	svc := NewProfitReportService(&stubProfitReportRepo{}, bills)

	rec, err := svc.GetReconcile(context.Background(), "2026-08", "2026-09")
	require.NoError(t, err)
	loc, _ := time.LoadLocation("Asia/Shanghai")
	require.True(t, bills.gotStart.Equal(time.Date(2026, 8, 1, 0, 0, 0, 0, loc)), bills.gotStart)
	require.True(t, bills.gotEnd.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, loc)), bills.gotEnd)
	require.Equal(t, "Asia/Shanghai", bills.gotTZ)
	require.Equal(t, "2026-08", bills.gotBillsLo)
	require.Equal(t, "2026-09", bills.gotBillsHi)
	require.Equal(t, "Asia/Shanghai", rec.Timezone)
	// nil 切片统一转成空切片，JSON 输出 [] 而不是 null。
	require.NotNil(t, rec.Rows)
	require.NotNil(t, rec.Accounts)
	require.NotNil(t, rec.Bindings)
	require.NotNil(t, rec.Bills)
}

func TestGetReconcile_PeriodValidation(t *testing.T) {
	svc := NewProfitReportService(&stubProfitReportRepo{}, &stubSupplierBillRepo{})
	ctx := context.Background()
	for _, tc := range [][2]string{{"", ""}, {"2026-8", "2026-09"}, {"2026-09", "2026-08"}, {"2026-13", "2026-13"}, {"2026-08-01", "2026-08"}} {
		_, err := svc.GetReconcile(ctx, tc[0], tc[1])
		require.ErrorIs(t, err, ErrReconcilePeriodInvalid, tc)
	}
	_, err := svc.GetReconcile(ctx, "2024-01", "2025-12")
	require.NoError(t, err, "24 months is allowed")
	_, err = svc.GetReconcile(ctx, "2024-01", "2026-01")
	require.ErrorIs(t, err, ErrReconcileRangeTooLarge)
}

func TestSaveBinding(t *testing.T) {
	repo := &stubSupplierBillRepo{}
	svc := NewProfitReportService(&stubProfitReportRepo{}, repo)
	ctx := context.Background()

	require.NoError(t, svc.SaveBinding(ctx, 5, " 金山云 ", " 2000186841 "))
	require.Equal(t, []SupplierBinding{{AccountID: 5, Supplier: "金山云", SupplierAccount: "2000186841"}}, repo.upserted)

	require.NoError(t, svc.SaveBinding(ctx, 5, "", "  "))
	require.Equal(t, []int64{5}, repo.deleted, "both empty means unbind")

	require.ErrorIs(t, svc.SaveBinding(ctx, 5, "金山云", ""), ErrReconcileBindingInvalid)
	require.ErrorIs(t, svc.SaveBinding(ctx, 0, "a", "b"), ErrReconcileBindingInvalid)
}

func f64(v float64) *float64 { return &v }

func TestReplaceBills_ValidatesAndTrims(t *testing.T) {
	repo := &stubSupplierBillRepo{}
	svc := NewProfitReportService(&stubProfitReportRepo{}, repo)
	ctx := context.Background()

	lines := []SupplierBillLine{
		{Period: "2026-08", Supplier: "金山云", SupplierAccount: "2000186841", StandardModel: " glm-5.2 ", ResourceFee: f64(25653.52)},
		{Period: "2026-08", Supplier: "金山云", SupplierAccount: "2000186841", StandardModel: ReconcileAdjustmentModel, Adjustment: f64(-6120.65)},
	}
	require.NoError(t, svc.ReplaceBills(ctx, []string{"2026-08", "2026-08"}, lines))
	require.Equal(t, []string{"2026-08"}, repo.gotPeriods, "periods are de-duplicated")
	require.Equal(t, "glm-5.2", repo.gotLines[0].StandardModel)

	// 空账期列表 = 允许清空？不允许：必须显式给出要覆盖的账期。
	require.ErrorIs(t, svc.ReplaceBills(ctx, nil, nil), ErrReconcileBillsInvalid)
	// 清空某个月是合法的（periods 给了、lines 为空）。
	require.NoError(t, svc.ReplaceBills(ctx, []string{"2026-09"}, nil))

	bad := []struct {
		name  string
		line  SupplierBillLine
		inSet string
	}{
		{"period not listed", SupplierBillLine{Period: "2026-09", StandardModel: "x"}, "2026-08"},
		{"empty model", SupplierBillLine{Period: "2026-08", StandardModel: " "}, "2026-08"},
		{"negative fx", SupplierBillLine{Period: "2026-08", StandardModel: "x", FXRate: f64(-1)}, "2026-08"},
		{"discount out of range", SupplierBillLine{Period: "2026-08", StandardModel: "x", CostDiscount: f64(-0.1)}, "2026-08"},
	}
	for _, tc := range bad {
		err := svc.ReplaceBills(ctx, []string{tc.inSet}, []SupplierBillLine{tc.line})
		require.ErrorIs(t, err, ErrReconcileBillsInvalid, tc.name)
		require.Contains(t, err.Error(), "line 1", tc.name)
	}
	require.ErrorIs(t, svc.ReplaceBills(ctx, []string{"2026/08"}, nil), ErrReconcileBillsInvalid)
}

func TestReplaceBills_PropagatesRepoError(t *testing.T) {
	sentinel := errors.New("boom")
	svc := NewProfitReportService(&stubProfitReportRepo{}, &stubSupplierBillRepo{replaceErr: sentinel})
	err := svc.ReplaceBills(context.Background(), []string{"2026-08"}, []SupplierBillLine{{Period: "2026-08", StandardModel: "x"}})
	require.ErrorIs(t, err, sentinel)
}
