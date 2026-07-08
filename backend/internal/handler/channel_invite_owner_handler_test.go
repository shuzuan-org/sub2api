package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// ciOwnerHandlerRepoStub 嵌接口 stub，仅覆写 owner 路径方法。
type ciOwnerHandlerRepoStub struct {
	service.ChannelInviteRepository
	batchesByCreator map[int64][]service.ChannelInviteBatch
	batchByID        map[int64]*service.ChannelInviteBatch
	usages           []service.ChannelInviteCodeUsage
	usagesResult     *pagination.PaginationResult
}

func (s *ciOwnerHandlerRepoStub) ListBatchesByCreator(_ context.Context, createdBy int64) ([]service.ChannelInviteBatch, error) {
	return s.batchesByCreator[createdBy], nil
}

func (s *ciOwnerHandlerRepoStub) GetBatch(_ context.Context, id int64) (*service.ChannelInviteBatch, error) {
	if b, ok := s.batchByID[id]; ok {
		return b, nil
	}
	return nil, service.ErrChannelInviteBatchNotFound
}

func (s *ciOwnerHandlerRepoStub) ListUsagesByBatch(_ context.Context, _ int64, _ pagination.PaginationParams) ([]service.ChannelInviteCodeUsage, *pagination.PaginationResult, error) {
	return s.usages, s.usagesResult, nil
}

func setupOwnerRouter(repo service.ChannelInviteRepository, userID int64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if userID > 0 {
		router.Use(func(c *gin.Context) {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: userID})
			c.Next()
		})
	}
	svc := service.NewChannelInviteService(repo, nil, nil, nil, nil)
	h := NewChannelInviteHandler(svc)
	router.GET("/api/v1/channel-invite/summary", h.GetOwnerSummary)
	router.GET("/api/v1/channel-invite/batches/:id/usages", h.ListOwnerBatchUsages)
	return router
}

func doOwnerRequest(t *testing.T, router *gin.Engine, path string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return w, body
}

func TestChannelInviteOwner_Unauthenticated(t *testing.T) {
	router := setupOwnerRouter(&ciOwnerHandlerRepoStub{}, 0)

	w, _ := doOwnerRequest(t, router, "/api/v1/channel-invite/summary")
	require.Equal(t, http.StatusUnauthorized, w.Code)

	w, _ = doOwnerRequest(t, router, "/api/v1/channel-invite/batches/1/usages")
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestChannelInviteOwner_SummaryShape(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	repo := &ciOwnerHandlerRepoStub{batchesByCreator: map[int64][]service.ChannelInviteBatch{
		42: {{
			ID: 3, Name: "B站国庆活动", Status: service.ChannelInviteBatchStatusActive,
			BonusAmount: 50, StartTime: &start, ActivityCopyText: "文案",
			Notes: "内部备注-不应泄露", CreatedBy: 42,
			Codes: []service.ChannelInviteCode{{Code: "A1B2C3D4E5F6", Status: "unused", MaxUses: 100, UsedCount: 27}},
		}},
	}}
	router := setupOwnerRouter(repo, 42)

	w, body := doOwnerRequest(t, router, "/api/v1/channel-invite/summary")
	require.Equal(t, http.StatusOK, w.Code)

	data := body["data"].(map[string]any)
	batches := data["batches"].([]any)
	require.Len(t, batches, 1)
	b := batches[0].(map[string]any)
	require.EqualValues(t, 3, b["id"])
	require.Equal(t, "B站国庆活动", b["name"])
	require.Equal(t, true, b["is_active"])
	require.EqualValues(t, 50, b["bonus_amount"])
	require.EqualValues(t, 1, b["code_count"])
	require.EqualValues(t, 27, b["used_count"])
	require.Equal(t, "2026-07-01 00:00:00", b["start_time"])
	require.Nil(t, b["end_time"])

	codes := b["codes"].([]any)
	require.Equal(t, "A1B2C3D4E5F6", codes[0].(map[string]any)["code"])

	// admin 专属字段不得泄露
	require.NotContains(t, b, "notes")
	require.NotContains(t, b, "created_by")
	require.NotContains(t, b, "creator")
}

func TestChannelInviteOwner_SummaryEmptyForNonPartner(t *testing.T) {
	router := setupOwnerRouter(&ciOwnerHandlerRepoStub{batchesByCreator: map[int64][]service.ChannelInviteBatch{}}, 7)

	w, body := doOwnerRequest(t, router, "/api/v1/channel-invite/summary")
	require.Equal(t, http.StatusOK, w.Code)
	require.Empty(t, body["data"].(map[string]any)["batches"])
}

func TestChannelInviteOwner_UsagesInvalidID(t *testing.T) {
	router := setupOwnerRouter(&ciOwnerHandlerRepoStub{}, 42)

	for _, path := range []string{
		"/api/v1/channel-invite/batches/abc/usages",
		"/api/v1/channel-invite/batches/0/usages",
		"/api/v1/channel-invite/batches/-1/usages",
	} {
		w, _ := doOwnerRequest(t, router, path)
		require.Equal(t, http.StatusBadRequest, w.Code, "path %s", path)
	}
}

func TestChannelInviteOwner_UsagesNonOwner404(t *testing.T) {
	repo := &ciOwnerHandlerRepoStub{batchByID: map[int64]*service.ChannelInviteBatch{
		1: {ID: 1, CreatedBy: 42},
	}}
	router := setupOwnerRouter(repo, 7) // 非 owner

	w, _ := doOwnerRequest(t, router, "/api/v1/channel-invite/batches/1/usages")
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestChannelInviteOwner_UsagesPaginated(t *testing.T) {
	claimed := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	repo := &ciOwnerHandlerRepoStub{
		batchByID: map[int64]*service.ChannelInviteBatch{1: {ID: 1, CreatedBy: 42}},
		usages: []service.ChannelInviteCodeUsage{
			{ID: 10, ClaimedAt: claimed, BonusGranted: true, User: &service.User{Email: "a@test.com", Username: "alice"}},
			{ID: 11, ClaimedAt: claimed, BonusGranted: false, User: nil}, // nil-guard
		},
		usagesResult: &pagination.PaginationResult{Total: 12, Page: 2, PageSize: 2, Pages: 6},
	}
	router := setupOwnerRouter(repo, 42)

	w, body := doOwnerRequest(t, router, "/api/v1/channel-invite/batches/1/usages?page=2&page_size=2")
	require.Equal(t, http.StatusOK, w.Code)

	data := body["data"].(map[string]any)
	require.EqualValues(t, 12, data["total"])
	require.EqualValues(t, 2, data["page"])
	items := data["items"].([]any)
	require.Len(t, items, 2)

	first := items[0].(map[string]any)
	require.Equal(t, "a@test.com", first["user_email"])
	require.Equal(t, "alice", first["user_username"])
	require.Equal(t, "2026-07-02 12:00:00", first["claimed_at"])
	require.Equal(t, true, first["bonus_granted"])

	second := items[1].(map[string]any)
	require.Equal(t, "", second["user_email"]) // User 为 nil 时空串
	require.Equal(t, false, second["bonus_granted"])
}
