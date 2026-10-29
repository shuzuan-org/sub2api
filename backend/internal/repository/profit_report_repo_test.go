package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestProfitReportRepositoryAggregateUsage(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &profitReportRepository{sql: db}

	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 2)

	mock.ExpectQuery(`SELECT\s+TO_CHAR\(created_at AT TIME ZONE \$3, 'YYYY-MM-DD'\) AS day,\s+LOWER\(COALESCE\(NULLIF\(TRIM\(billing_model\), ''\), model\)\) AS model`).
		WithArgs(start, end, "Asia/Shanghai").
		WillReturnRows(sqlmock.NewRows([]string{
			"day", "model", "account_id", "group_id", "requests",
			"input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens",
			"list_price", "revenue",
		}).
			AddRow("2026-10-01", "glm-5.3-flash", int64(13), int64(7), int64(5), int64(100), int64(20), int64(0), int64(30), 1.5, 1.275).
			AddRow("2026-10-02", "deepseek-v4-flash", int64(72), nil, int64(1), int64(10), int64(2), int64(0), int64(0), 0.2, 0.2))

	rows, err := repo.AggregateUsage(context.Background(), start, end, "Asia/Shanghai")
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "glm-5.3-flash", rows[0].Model)
	require.Equal(t, int64(13), rows[0].AccountID)
	require.NotNil(t, rows[0].GroupID)
	require.Equal(t, int64(7), *rows[0].GroupID)
	require.InDelta(t, 1.275, rows[0].Revenue, 1e-9)
	require.Nil(t, rows[1].GroupID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProfitReportRepositoryAggregateUsageDefaultsTimezone(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &profitReportRepository{sql: db}
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)

	mock.ExpectQuery(`AT TIME ZONE \$3`).
		WithArgs(start, end, resolveUsageStatsTimezone()).
		WillReturnRows(sqlmock.NewRows([]string{
			"day", "model", "account_id", "group_id", "requests",
			"input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens",
			"list_price", "revenue",
		}))

	rows, err := repo.AggregateUsage(context.Background(), start, end, "")
	require.NoError(t, err)
	require.NotNil(t, rows)
	require.Empty(t, rows)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProfitReportRepositoryListAccountsAndGroups(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &profitReportRepository{sql: db}

	lastUsed := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`FROM accounts`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "platform", "rate_multiplier", "status", "schedulable", "deleted", "last_used_at"}).
			AddRow(int64(13), "GLM5-小豪", "openai", 1.0, "active", true, false, lastUsed).
			AddRow(int64(80), "old", "anthropic", 0.5, "disabled", false, true, nil))

	accounts, err := repo.ListAccounts(context.Background())
	require.NoError(t, err)
	require.Len(t, accounts, 2)
	require.NotNil(t, accounts[0].LastUsedAt)
	require.True(t, accounts[0].LastUsedAt.Equal(lastUsed))
	require.Nil(t, accounts[1].LastUsedAt)
	require.True(t, accounts[1].Deleted)
	require.False(t, accounts[1].Schedulable)
	require.InDelta(t, 0.5, accounts[1].RateMultiplier, 1e-9)

	mock.ExpectQuery(`FROM groups\s+WHERE id = ANY\(\$1\)`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "platform", "rate_multiplier", "deleted"}).
			AddRow(int64(7), "GLM-官方", "openai", 0.85, false))

	groups, err := repo.ListGroupsByIDs(context.Background(), []int64{7})
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.InDelta(t, 0.85, groups[0].RateMultiplier, 1e-9)

	// 空 id 列表不发 SQL。
	groups, err = repo.ListGroupsByIDs(context.Background(), nil)
	require.NoError(t, err)
	require.Empty(t, groups)
	require.NoError(t, mock.ExpectationsWereMet())
}
