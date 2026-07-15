package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type stubOperationsReporter struct {
	result     *service.UserOperationsLookupResult
	err        error
	gotAccount string
	gotUserID  int64
	callCount  int
}

func (s *stubOperationsReporter) GetReport(_ context.Context, account string, userID int64) (*service.UserOperationsLookupResult, error) {
	s.callCount++
	s.gotAccount = account
	s.gotUserID = userID
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

func setupOperationsRouter(stub *stubOperationsReporter) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := &UserOperationsHandler{svc: stub}
	router.GET("/api/v1/admin/users/operations-report", h.GetReport)
	return router
}

func doOperationsRequest(t *testing.T, router *gin.Engine, query string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users/operations-report"+query, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return w, body
}

func TestOperationsReport_MissingParams(t *testing.T) {
	stub := &stubOperationsReporter{}
	router := setupOperationsRouter(stub)

	w, _ := doOperationsRequest(t, router, "")
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Zero(t, stub.callCount)
}

func TestOperationsReport_InvalidUserID(t *testing.T) {
	stub := &stubOperationsReporter{}
	router := setupOperationsRouter(stub)

	for _, q := range []string{"?user_id=abc", "?user_id=0", "?user_id=-1"} {
		w, _ := doOperationsRequest(t, router, q)
		require.Equal(t, http.StatusBadRequest, w.Code, "query %s", q)
	}
	require.Zero(t, stub.callCount)
}

func TestOperationsReport_NotFound(t *testing.T) {
	stub := &stubOperationsReporter{err: service.ErrUserNotFound}
	router := setupOperationsRouter(stub)

	w, _ := doOperationsRequest(t, router, "?account=nobody@test.com")
	require.Equal(t, http.StatusNotFound, w.Code)
	require.Equal(t, "nobody@test.com", stub.gotAccount)
}

func TestOperationsReport_Matched(t *testing.T) {
	code := "AB3CD9"
	inviterID := int64(3)
	created := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	stub := &stubOperationsReporter{result: &service.UserOperationsLookupResult{
		Report: &service.UserOperationsReport{
			User: &service.User{
				ID: 12, Email: "a@x.com", Username: "alice", Role: "user",
				Status: "active", Balance: 123.45, CreatedAt: created,
			},
			ReferralCode: &code,
			InviterID:    &inviterID,
			Inviter: &service.UserOperationsBrief{
				ID: 3, Email: "inv@x.com", Username: "bob", CreatedAt: created,
			},
			InvitedCount: 152,
			Invitees: []service.UserOperationsBrief{
				{ID: 88, Email: "c@z.com", Username: "carol", CreatedAt: created},
			},
			InviteesTruncated:  true,
			AlipayPaidCount:    4,
			AlipayCnyFeeTotal:  39800,
			AlipayUsdTotal:     400,
			RedeemBalanceTotal: 250,
			Usage: &usagestats.UsageStats{
				TotalRequests: 10234, TotalInputTokens: 1000000, TotalOutputTokens: 500000,
				TotalCacheTokens: 250000, TotalTokens: 1750000, TotalCost: 812.34, TotalActualCost: 640.11,
			},
		},
	}}
	router := setupOperationsRouter(stub)

	w, body := doOperationsRequest(t, router, "?account=a@x.com")
	require.Equal(t, http.StatusOK, w.Code)

	data := body["data"].(map[string]any)
	require.Equal(t, true, data["matched"])
	require.NotContains(t, data, "candidates")

	report := data["report"].(map[string]any)
	user := report["user"].(map[string]any)
	require.EqualValues(t, 12, user["id"])
	require.Equal(t, "alice", user["username"])

	invitation := report["invitation"].(map[string]any)
	require.Equal(t, code, invitation["referral_code"])
	require.EqualValues(t, 3, invitation["inviter_id"])
	require.Equal(t, "inv@x.com", invitation["inviter"].(map[string]any)["email"])
	require.EqualValues(t, 152, invitation["invited_count"])
	require.Len(t, invitation["invitees"].([]any), 1)
	require.Equal(t, true, invitation["invitees_truncated"])

	recharge := report["recharge"].(map[string]any)
	alipay := recharge["alipay"].(map[string]any)
	require.EqualValues(t, 4, alipay["paid_order_count"])
	require.EqualValues(t, 39800, alipay["cny_fee_total"])
	require.EqualValues(t, 400, alipay["usd_amount_total"])
	require.EqualValues(t, 250, recharge["redeem_balance_total"])
	require.EqualValues(t, 650, recharge["combined_usd_total"])

	usage := report["usage"].(map[string]any)
	require.EqualValues(t, 10234, usage["total_requests"])
	require.EqualValues(t, 1750000, usage["total_tokens"])
}

func TestOperationsReport_Candidates(t *testing.T) {
	created := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	stub := &stubOperationsReporter{result: &service.UserOperationsLookupResult{
		Candidates: []service.UserOperationsBrief{
			{ID: 12, Email: "a@x.com", Username: "alice", CreatedAt: created},
			{ID: 47, Email: "b@y.com", Username: "alice", CreatedAt: created},
		},
	}}
	router := setupOperationsRouter(stub)

	w, body := doOperationsRequest(t, router, "?account=alice")
	require.Equal(t, http.StatusOK, w.Code)

	data := body["data"].(map[string]any)
	require.Equal(t, false, data["matched"])
	require.NotContains(t, data, "report")

	candidates := data["candidates"].([]any)
	require.Len(t, candidates, 2)
	require.EqualValues(t, 12, candidates[0].(map[string]any)["id"])
	require.EqualValues(t, 47, candidates[1].(map[string]any)["id"])
}

func TestOperationsReport_UserIDPassedThrough(t *testing.T) {
	stub := &stubOperationsReporter{result: &service.UserOperationsLookupResult{
		Report: &service.UserOperationsReport{
			User:  &service.User{ID: 12},
			Usage: &usagestats.UsageStats{},
		},
	}}
	router := setupOperationsRouter(stub)

	w, _ := doOperationsRequest(t, router, "?user_id=12")
	require.Equal(t, http.StatusOK, w.Code)
	require.EqualValues(t, 12, stub.gotUserID)
	require.Empty(t, stub.gotAccount)
}
