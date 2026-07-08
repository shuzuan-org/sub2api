package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

// ciOwnerRepoStub 嵌接口 + 覆写 owner 路径所需方法（同包既有 stub 模式）。
type ciOwnerRepoStub struct {
	ChannelInviteRepository
	batchesByCreator map[int64][]ChannelInviteBatch
	batchByID        map[int64]*ChannelInviteBatch
	usages           []ChannelInviteCodeUsage
	usagesResult     *pagination.PaginationResult
	gotBatchID       int64
	gotParams        pagination.PaginationParams
}

func (s *ciOwnerRepoStub) ListBatchesByCreator(_ context.Context, createdBy int64) ([]ChannelInviteBatch, error) {
	return s.batchesByCreator[createdBy], nil
}

func (s *ciOwnerRepoStub) GetBatch(_ context.Context, id int64) (*ChannelInviteBatch, error) {
	if b, ok := s.batchByID[id]; ok {
		return b, nil
	}
	return nil, ErrChannelInviteBatchNotFound
}

func (s *ciOwnerRepoStub) ListUsagesByBatch(_ context.Context, batchID int64, params pagination.PaginationParams) ([]ChannelInviteCodeUsage, *pagination.PaginationResult, error) {
	s.gotBatchID = batchID
	s.gotParams = params
	return s.usages, s.usagesResult, nil
}

func newOwnerService(repo ChannelInviteRepository) *ChannelInviteService {
	// owner 视角路径只依赖 repo，其余依赖传 nil 即可。
	return NewChannelInviteService(repo, nil, nil, nil, nil)
}

func TestChannelInviteOwnerSummary_CountsFromPreloadedCodes(t *testing.T) {
	repo := &ciOwnerRepoStub{batchesByCreator: map[int64][]ChannelInviteBatch{
		42: {
			{
				ID: 1, Name: "活动A", CreatedBy: 42,
				Codes: []ChannelInviteCode{
					{Code: "AAA", UsedCount: 3},
					{Code: "BBB", UsedCount: 4},
				},
			},
			{ID: 2, Name: "活动B", CreatedBy: 42}, // 无码
		},
	}}
	svc := newOwnerService(repo)

	batches, err := svc.GetOwnerSummary(context.Background(), 42)
	require.NoError(t, err)
	require.Len(t, batches, 2)
	require.Equal(t, 2, batches[0].CodeCount)
	require.Equal(t, 7, batches[0].UsedCount)
	require.Equal(t, 0, batches[1].CodeCount)
	require.Equal(t, 0, batches[1].UsedCount)
}

func TestChannelInviteOwnerSummary_EmptyForNonPartner(t *testing.T) {
	svc := newOwnerService(&ciOwnerRepoStub{batchesByCreator: map[int64][]ChannelInviteBatch{}})

	batches, err := svc.GetOwnerSummary(context.Background(), 99)
	require.NoError(t, err)
	require.Empty(t, batches)
}

func TestChannelInviteOwnerUsages_NonOwnerGetsNotFound(t *testing.T) {
	repo := &ciOwnerRepoStub{batchByID: map[int64]*ChannelInviteBatch{
		1: {ID: 1, CreatedBy: 42},
	}}
	svc := newOwnerService(repo)

	_, _, err := svc.ListOwnerBatchUsages(context.Background(), 7, 1, pagination.PaginationParams{Page: 1, PageSize: 10})
	require.ErrorIs(t, err, ErrChannelInviteBatchNotFound)

	// 批次不存在同样 NotFound
	_, _, err = svc.ListOwnerBatchUsages(context.Background(), 42, 999, pagination.PaginationParams{Page: 1, PageSize: 10})
	require.ErrorIs(t, err, ErrChannelInviteBatchNotFound)
}

func TestChannelInviteOwnerUsages_OwnerPassthrough(t *testing.T) {
	repo := &ciOwnerRepoStub{
		batchByID: map[int64]*ChannelInviteBatch{1: {ID: 1, CreatedBy: 42}},
		usages: []ChannelInviteCodeUsage{
			{ID: 10, UserID: 100, BonusGranted: true, User: &User{Email: "a@test.com", Username: "alice"}},
		},
		usagesResult: &pagination.PaginationResult{Total: 1, Page: 2, PageSize: 5, Pages: 1},
	}
	svc := newOwnerService(repo)

	usages, result, err := svc.ListOwnerBatchUsages(context.Background(), 42, 1, pagination.PaginationParams{Page: 2, PageSize: 5})
	require.NoError(t, err)
	require.Len(t, usages, 1)
	require.EqualValues(t, 1, result.Total)
	require.EqualValues(t, 1, repo.gotBatchID)
	require.Equal(t, 2, repo.gotParams.Page)
	require.Equal(t, 5, repo.gotParams.PageSize)
}
