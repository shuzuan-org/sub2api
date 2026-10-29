package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type stubProfitReportRepo struct {
	rows        []ProfitReportRow
	accounts    []ProfitReportAccount
	groups      []ProfitReportGroup
	gotTZ       string
	gotGroupIDs []int64
	groupsCalls int
	err         error
}

func (s *stubProfitReportRepo) AggregateUsage(_ context.Context, _, _ time.Time, tzName string) ([]ProfitReportRow, error) {
	s.gotTZ = tzName
	return s.rows, s.err
}

func (s *stubProfitReportRepo) ListAccounts(_ context.Context) ([]ProfitReportAccount, error) {
	return s.accounts, nil
}

func (s *stubProfitReportRepo) ListGroupsByIDs(_ context.Context, ids []int64) ([]ProfitReportGroup, error) {
	s.groupsCalls++
	s.gotGroupIDs = ids
	return s.groups, nil
}

func TestProfitReportService_RangeValidation(t *testing.T) {
	svc := NewProfitReportService(&stubProfitReportRepo{})
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	_, err := svc.GetReport(context.Background(), start, start, "")
	require.ErrorIs(t, err, ErrProfitReportRangeInvalid)

	_, err = svc.GetReport(context.Background(), start, start.Add(-time.Hour), "")
	require.ErrorIs(t, err, ErrProfitReportRangeInvalid)

	_, err = svc.GetReport(context.Background(), start, start.AddDate(0, 0, profitReportMaxRangeDays+1), "")
	require.ErrorIs(t, err, ErrProfitReportRangeTooLarge)

	_, err = svc.GetReport(context.Background(), start, start.AddDate(0, 0, profitReportMaxRangeDays), "")
	require.NoError(t, err)
}

func TestProfitReportService_TimezoneAndGroupLookup(t *testing.T) {
	g1, g2 := int64(1), int64(2)
	repo := &stubProfitReportRepo{
		rows: []ProfitReportRow{
			{Day: "2026-10-01", Model: "a", AccountID: 1, GroupID: &g1},
			{Day: "2026-10-01", Model: "a", AccountID: 2, GroupID: &g2},
			{Day: "2026-10-02", Model: "a", AccountID: 1, GroupID: &g1},
			{Day: "2026-10-02", Model: "b", AccountID: 3, GroupID: nil},
		},
		groups: []ProfitReportGroup{{ID: 1}, {ID: 2}},
	}
	svc := NewProfitReportService(repo)
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	report, err := svc.GetReport(context.Background(), start, start.AddDate(0, 0, 2), "Asia/Shanghai")
	require.NoError(t, err)
	require.Equal(t, "Asia/Shanghai", repo.gotTZ)
	require.Equal(t, "Asia/Shanghai", report.Timezone)
	require.Equal(t, []int64{1, 2}, repo.gotGroupIDs, "distinct group ids in first-seen order")
	require.Len(t, report.Groups, 2)
	require.NotNil(t, report.Accounts)

	// 非法时区退回空串，交给仓储层用应用时区。
	_, err = svc.GetReport(context.Background(), start, start.AddDate(0, 0, 1), "Mars/Olympus")
	require.NoError(t, err)
	require.Equal(t, "", repo.gotTZ)
}

func TestProfitReportService_NoGroupsSkipsLookup(t *testing.T) {
	repo := &stubProfitReportRepo{rows: []ProfitReportRow{{Day: "2026-10-01", Model: "a", AccountID: 1}}}
	svc := NewProfitReportService(repo)
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	report, err := svc.GetReport(context.Background(), start, start.AddDate(0, 0, 1), "")
	require.NoError(t, err)
	require.Equal(t, 0, repo.groupsCalls)
	require.NotNil(t, report.Groups)
	require.Empty(t, report.Groups)
}

func TestProfitReportService_RepoErrorWrapped(t *testing.T) {
	sentinel := errors.New("boom")
	svc := NewProfitReportService(&stubProfitReportRepo{err: sentinel})
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	_, err := svc.GetReport(context.Background(), start, start.AddDate(0, 0, 1), "")
	require.ErrorIs(t, err, sentinel)
}
