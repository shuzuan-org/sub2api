package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestSupplierBillRepositoryAggregateUsageMonthly(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &supplierBillRepository{db: db}
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)

	mock.ExpectQuery(`TO_CHAR\(created_at AT TIME ZONE \$3, 'YYYY-MM'\) AS period,\s+LOWER\(COALESCE\(NULLIF\(TRIM\(billing_model\), ''\), model\)\) AS model`).
		WithArgs(start, end, "Asia/Shanghai").
		WillReturnRows(sqlmock.NewRows([]string{
			"period", "model", "account_id", "requests",
			"input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens", "list_price", "revenue",
		}).AddRow("2026-08", "glm-5.2", int64(7), int64(157813), int64(10), int64(20), int64(0), int64(30), 464598.7, 400189.8))

	rows, err := repo.AggregateUsageMonthly(context.Background(), start, end, "Asia/Shanghai")
	require.NoError(t, err)
	require.Equal(t, []service.ReconcileUsageRow{{
		Period: "2026-08", Model: "glm-5.2", AccountID: 7, Requests: 157813,
		InputTokens: 10, OutputTokens: 20, CacheReadTokens: 30, ListPrice: 464598.7, Revenue: 400189.8,
	}}, rows)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSupplierBillRepositoryListBillsKeepsNulls(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &supplierBillRepository{db: db}
	updated := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	cols := []string{"id", "period", "supplier", "supplier_account", "standard_model", "platform_models", "cost_discount", "currency",
		"input_tokens", "output_tokens", "cache_read_tokens", "cache_write_tokens", "other_tokens",
		"resource_fee", "discount_amount", "adjustment", "tax", "fx_rate", "note", "updated_at"}
	mock.ExpectQuery(`FROM supplier_bill_lines\s+WHERE period >= \$1 AND period <= \$2`).
		WithArgs("2026-08", "2026-08").
		WillReturnRows(sqlmock.NewRows(cols).
			AddRow(int64(1), "2026-08", "金山云", "2000186841", "glm-5.2", "glm-5.2", 0.55, "RMB",
				1296590057.0, nil, nil, nil, nil, 46646.86, 20993.34, nil, 0.0, 1.0, "", updated).
			AddRow(int64(2), "2026-08", "金山云", "2000186841", "调账", "", nil, "RMB",
				nil, nil, nil, nil, nil, nil, nil, -6120.65, nil, 1.0, "8/7-8/17 折扣补退", updated))

	lines, err := repo.ListBills(context.Background(), "2026-08", "2026-08")
	require.NoError(t, err)
	require.Len(t, lines, 2)
	require.InDelta(t, 0.55, *lines[0].CostDiscount, 1e-9)
	require.InDelta(t, 1296590057.0, *lines[0].InputTokens, 1e-9)
	require.Nil(t, lines[0].OutputTokens)
	require.Nil(t, lines[0].Adjustment)
	require.NotNil(t, lines[0].Tax, "explicit 0 must survive as non-nil")
	require.Nil(t, lines[1].CostDiscount)
	require.InDelta(t, -6120.65, *lines[1].Adjustment, 1e-9)
	require.Equal(t, "8/7-8/17 折扣补退", lines[1].Note)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSupplierBillRepositoryReplaceBillsIsTransactional(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &supplierBillRepository{db: db}
	fee := 25653.52

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM supplier_bill_lines WHERE period = ANY\(\$1\)`).
		WithArgs(pq.Array([]string{"2026-08"})).
		WillReturnResult(sqlmock.NewResult(0, 3))
	prep := mock.ExpectPrepare(`INSERT INTO supplier_bill_lines`)
	prep.ExpectExec().
		WithArgs("2026-08", "金山云", "2000186841", "glm-5.2", "", nil, "RMB",
			nil, nil, nil, nil, nil, fee, nil, nil, nil, nil, "").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	err := repo.ReplaceBills(context.Background(), []string{"2026-08"}, []service.SupplierBillLine{{
		Period: "2026-08", Supplier: "金山云", SupplierAccount: "2000186841", StandardModel: "glm-5.2", Currency: "RMB", ResourceFee: &fee,
	}})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSupplierBillRepositoryReplaceBillsRollsBackOnError(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &supplierBillRepository{db: db}

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM supplier_bill_lines`).WillReturnResult(sqlmock.NewResult(0, 1))
	prep := mock.ExpectPrepare(`INSERT INTO supplier_bill_lines`)
	prep.ExpectExec().WillReturnError(errors.New("constraint"))
	mock.ExpectRollback()

	err := repo.ReplaceBills(context.Background(), []string{"2026-08"}, []service.SupplierBillLine{{Period: "2026-08", StandardModel: "x"}})
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSupplierBillRepositoryBindings(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &supplierBillRepository{db: db}

	mock.ExpectExec(`INSERT INTO account_supplier_bindings .* ON CONFLICT \(account_id\) DO UPDATE`).
		WithArgs(int64(7), "金山云", "2000186841").
		WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.UpsertBinding(context.Background(), service.SupplierBinding{AccountID: 7, Supplier: "金山云", SupplierAccount: "2000186841"}))

	mock.ExpectExec(`DELETE FROM account_supplier_bindings WHERE account_id = \$1`).
		WithArgs(int64(7)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.DeleteBinding(context.Background(), 7))

	updated := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`FROM account_supplier_bindings`).
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "supplier", "supplier_account", "updated_at"}).
			AddRow(int64(7), "金山云", "2000186841", updated))
	bs, err := repo.ListBindings(context.Background())
	require.NoError(t, err)
	require.Equal(t, []service.SupplierBinding{{AccountID: 7, Supplier: "金山云", SupplierAccount: "2000186841", UpdatedAt: updated}}, bs)
	require.NoError(t, mock.ExpectationsWereMet())
}
