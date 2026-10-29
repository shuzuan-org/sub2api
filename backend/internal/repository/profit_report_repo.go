package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type profitReportRepository struct {
	sql sqlExecutor
}

// NewProfitReportRepository 创建收入看板仓储（只读聚合，走裸 SQL）。
func NewProfitReportRepository(sqlDB *sql.DB) service.ProfitReportRepository {
	return &profitReportRepository{sql: sqlDB}
}

// AggregateUsage 口径与 GetAccountTodayStats / usage 统计一致：
// list_price = Σ total_cost（标价），revenue = Σ actual_cost（实付）。
// 模型维度用 lower(coalesce(billing_model, model))，即真正计费的模型名，
// 这样同一模型经不同账号/映射走出去的流量能并到一行比价。
func (r *profitReportRepository) AggregateUsage(ctx context.Context, startTime, endTime time.Time, tzName string) (results []service.ProfitReportRow, err error) {
	if tzName == "" {
		tzName = resolveUsageStatsTimezone()
	}
	query := `
		SELECT
			TO_CHAR(created_at AT TIME ZONE $3, 'YYYY-MM-DD') AS day,
			LOWER(COALESCE(NULLIF(TRIM(billing_model), ''), model)) AS model,
			COALESCE(account_id, 0) AS account_id,
			group_id,
			COUNT(*) AS requests,
			COALESCE(SUM(input_tokens), 0) AS input_tokens,
			COALESCE(SUM(output_tokens), 0) AS output_tokens,
			COALESCE(SUM(cache_creation_tokens), 0) AS cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens), 0) AS cache_read_tokens,
			COALESCE(SUM(total_cost), 0) AS list_price,
			COALESCE(SUM(actual_cost), 0) AS revenue
		FROM usage_logs
		WHERE created_at >= $1 AND created_at < $2
		GROUP BY 1, 2, 3, 4
		ORDER BY 1, 2, 3, 4
	`
	rows, err := r.sql.QueryContext(ctx, query, startTime, endTime, tzName)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
			results = nil
		}
	}()

	results = make([]service.ProfitReportRow, 0, 256)
	for rows.Next() {
		var row service.ProfitReportRow
		var groupID sql.NullInt64
		if err = rows.Scan(
			&row.Day,
			&row.Model,
			&row.AccountID,
			&groupID,
			&row.Requests,
			&row.InputTokens,
			&row.OutputTokens,
			&row.CacheCreationTokens,
			&row.CacheReadTokens,
			&row.ListPrice,
			&row.Revenue,
		); err != nil {
			return nil, err
		}
		if groupID.Valid {
			gid := groupID.Int64
			row.GroupID = &gid
		}
		results = append(results, row)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// ListAccounts 返回全部账号，含软删与停调的，因为历史日志仍引用它们。
func (r *profitReportRepository) ListAccounts(ctx context.Context) (results []service.ProfitReportAccount, err error) {
	query := `
		SELECT id, name, COALESCE(platform, ''), COALESCE(rate_multiplier, 1), COALESCE(status, ''),
		       COALESCE(schedulable, true), deleted_at IS NOT NULL, last_used_at
		FROM accounts
		ORDER BY id
	`
	rows, err := r.sql.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
			results = nil
		}
	}()

	results = make([]service.ProfitReportAccount, 0, 64)
	for rows.Next() {
		var a service.ProfitReportAccount
		var lastUsed sql.NullTime
		if err = rows.Scan(&a.ID, &a.Name, &a.Platform, &a.RateMultiplier, &a.Status, &a.Schedulable, &a.Deleted, &lastUsed); err != nil {
			return nil, err
		}
		if lastUsed.Valid {
			t := lastUsed.Time
			a.LastUsedAt = &t
		}
		results = append(results, a)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// ListGroupsByIDs 按 id 返回分组，含软删的。
func (r *profitReportRepository) ListGroupsByIDs(ctx context.Context, ids []int64) (results []service.ProfitReportGroup, err error) {
	if len(ids) == 0 {
		return []service.ProfitReportGroup{}, nil
	}
	query := `
		SELECT id, name, COALESCE(platform, ''), COALESCE(rate_multiplier, 1), deleted_at IS NOT NULL
		FROM groups
		WHERE id = ANY($1)
		ORDER BY id
	`
	rows, err := r.sql.QueryContext(ctx, query, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
			results = nil
		}
	}()

	results = make([]service.ProfitReportGroup, 0, len(ids))
	for rows.Next() {
		var g service.ProfitReportGroup
		if err = rows.Scan(&g.ID, &g.Name, &g.Platform, &g.RateMultiplier, &g.Deleted); err != nil {
			return nil, err
		}
		results = append(results, g)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}
