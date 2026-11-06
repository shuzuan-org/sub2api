package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type supplierBillRepository struct {
	db *sql.DB
}

// NewSupplierBillRepository 创建月度对账仓储（供应商绑定 + 供应商账单，裸 SQL）。
func NewSupplierBillRepository(sqlDB *sql.DB) service.SupplierBillRepository {
	return &supplierBillRepository{db: sqlDB}
}

// AggregateUsageMonthly 口径与 profitReportRepository.AggregateUsage 一致，只是按自然月切。
func (r *supplierBillRepository) AggregateUsageMonthly(ctx context.Context, startTime, endTime time.Time, tzName string) (results []service.ReconcileUsageRow, err error) {
	query := `
		SELECT
			TO_CHAR(created_at AT TIME ZONE $3, 'YYYY-MM') AS period,
			LOWER(COALESCE(NULLIF(TRIM(billing_model), ''), model)) AS model,
			COALESCE(account_id, 0) AS account_id,
			COUNT(*) AS requests,
			COALESCE(SUM(input_tokens), 0) AS input_tokens,
			COALESCE(SUM(output_tokens), 0) AS output_tokens,
			COALESCE(SUM(cache_creation_tokens), 0) AS cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens), 0) AS cache_read_tokens,
			COALESCE(SUM(total_cost), 0) AS list_price,
			COALESCE(SUM(actual_cost), 0) AS revenue
		FROM usage_logs
		WHERE created_at >= $1 AND created_at < $2
		GROUP BY 1, 2, 3
		ORDER BY 1, 2, 3
	`
	rows, err := r.db.QueryContext(ctx, query, startTime, endTime, tzName)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
			results = nil
		}
	}()
	results = make([]service.ReconcileUsageRow, 0, 128)
	for rows.Next() {
		var x service.ReconcileUsageRow
		if err = rows.Scan(&x.Period, &x.Model, &x.AccountID, &x.Requests,
			&x.InputTokens, &x.OutputTokens, &x.CacheCreationTokens, &x.CacheReadTokens,
			&x.ListPrice, &x.Revenue); err != nil {
			return nil, err
		}
		results = append(results, x)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (r *supplierBillRepository) ListBindings(ctx context.Context) (results []service.SupplierBinding, err error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT account_id, supplier, supplier_account, updated_at
		FROM account_supplier_bindings
		ORDER BY account_id
	`)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
			results = nil
		}
	}()
	results = make([]service.SupplierBinding, 0, 32)
	for rows.Next() {
		var b service.SupplierBinding
		if err = rows.Scan(&b.AccountID, &b.Supplier, &b.SupplierAccount, &b.UpdatedAt); err != nil {
			return nil, err
		}
		results = append(results, b)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (r *supplierBillRepository) UpsertBinding(ctx context.Context, b service.SupplierBinding) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO account_supplier_bindings (account_id, supplier, supplier_account, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (account_id) DO UPDATE
		SET supplier = EXCLUDED.supplier, supplier_account = EXCLUDED.supplier_account, updated_at = NOW()
	`, b.AccountID, b.Supplier, b.SupplierAccount)
	return err
}

func (r *supplierBillRepository) DeleteBinding(ctx context.Context, accountID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM account_supplier_bindings WHERE account_id = $1`, accountID)
	return err
}

const supplierBillColumns = `period, supplier, supplier_account, standard_model, platform_models, cost_discount, currency,
	input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, other_tokens,
	resource_fee, discount_amount, adjustment, tax, fx_rate, note`

func (r *supplierBillRepository) ListBills(ctx context.Context, startPeriod, endPeriod string) (results []service.SupplierBillLine, err error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, `+supplierBillColumns+`, updated_at
		FROM supplier_bill_lines
		WHERE period >= $1 AND period <= $2
		ORDER BY period, supplier, supplier_account, standard_model, id
	`, startPeriod, endPeriod)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
			results = nil
		}
	}()
	results = make([]service.SupplierBillLine, 0, 64)
	for rows.Next() {
		var l service.SupplierBillLine
		var nums [11]sql.NullFloat64
		if err = rows.Scan(&l.ID, &l.Period, &l.Supplier, &l.SupplierAccount, &l.StandardModel, &l.PlatformModels,
			&nums[0], &l.Currency,
			&nums[1], &nums[2], &nums[3], &nums[4], &nums[5],
			&nums[6], &nums[7], &nums[8], &nums[9], &nums[10],
			&l.Note, &l.UpdatedAt); err != nil {
			return nil, err
		}
		ptrs := []**float64{&l.CostDiscount, &l.InputTokens, &l.OutputTokens, &l.CacheReadTokens, &l.CacheWriteTokens,
			&l.OtherTokens, &l.ResourceFee, &l.DiscountAmount, &l.Adjustment, &l.Tax, &l.FXRate}
		for i, n := range nums {
			if n.Valid {
				v := n.Float64
				*ptrs[i] = &v
			}
		}
		results = append(results, l)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func nullableFloat(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func (r *supplierBillRepository) ReplaceBills(ctx context.Context, periods []string, lines []service.SupplierBillLine) (err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err = tx.ExecContext(ctx, `DELETE FROM supplier_bill_lines WHERE period = ANY($1)`, pq.Array(periods)); err != nil {
		return fmt.Errorf("delete old supplier bills: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO supplier_bill_lines (`+supplierBillColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
	`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for _, l := range lines {
		if _, err = stmt.ExecContext(ctx,
			l.Period, l.Supplier, l.SupplierAccount, l.StandardModel, l.PlatformModels, nullableFloat(l.CostDiscount), l.Currency,
			nullableFloat(l.InputTokens), nullableFloat(l.OutputTokens), nullableFloat(l.CacheReadTokens),
			nullableFloat(l.CacheWriteTokens), nullableFloat(l.OtherTokens),
			nullableFloat(l.ResourceFee), nullableFloat(l.DiscountAmount), nullableFloat(l.Adjustment),
			nullableFloat(l.Tax), nullableFloat(l.FXRate), l.Note,
		); err != nil {
			return fmt.Errorf("insert supplier bill line: %w", err)
		}
	}
	return tx.Commit()
}
