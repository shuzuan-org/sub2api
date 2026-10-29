package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type stubProfitReporter struct {
	report   *service.ProfitReport
	err      error
	gotStart time.Time
	gotEnd   time.Time
	gotTZ    string
}

func (s *stubProfitReporter) GetReport(_ context.Context, startTime, endTime time.Time, userTZ string) (*service.ProfitReport, error) {
	s.gotStart = startTime
	s.gotEnd = endTime
	s.gotTZ = userTZ
	if s.err != nil {
		return nil, s.err
	}
	return s.report, nil
}

func setupProfitReportRouter(stub *stubProfitReporter) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := &ProfitReportHandler{svc: stub}
	router.GET("/api/v1/admin/profit-report", h.GetReport)
	return router
}

func TestProfitReport_PassesRangeAndTimezone(t *testing.T) {
	gid := int64(7)
	lastUsed := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
	stub := &stubProfitReporter{report: &service.ProfitReport{
		GeneratedAt: time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC),
		StartTime:   time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		EndTime:     time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC),
		Timezone:    "Asia/Shanghai",
		Rows: []service.ProfitReportRow{{
			Day: "2026-10-28", Model: "glm-5.3-flash", AccountID: 13, GroupID: &gid,
			Requests: 2, InputTokens: 100, OutputTokens: 20, CacheReadTokens: 30,
			ListPrice: 1.5, Revenue: 1.275,
		}},
		Accounts: []service.ProfitReportAccount{{
			ID: 13, Name: "GLM5-小豪", Platform: "openai", RateMultiplier: 1, Status: "active", Schedulable: true, LastUsedAt: &lastUsed,
		}},
		Groups: []service.ProfitReportGroup{{ID: 7, Name: "GLM-官方", Platform: "openai", RateMultiplier: 0.85}},
	}}
	router := setupProfitReportRouter(stub)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/profit-report?start_date=2026-10-01&end_date=2026-10-28&timezone=Asia/Shanghai", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	require.Equal(t, "Asia/Shanghai", stub.gotTZ)
	require.Equal(t, 28*24*time.Hour, stub.gotEnd.Sub(stub.gotStart), "end_date is inclusive")

	var body struct {
		Code int `json:"code"`
		Data struct {
			StartDate string `json:"start_date"`
			EndDate   string `json:"end_date"`
			Timezone  string `json:"timezone"`
			Rows      []struct {
				Day       string  `json:"day"`
				Model     string  `json:"model"`
				AccountID int64   `json:"account_id"`
				GroupID   *int64  `json:"group_id"`
				Requests  int64   `json:"requests"`
				ListPrice float64 `json:"list_price"`
				Revenue   float64 `json:"revenue"`
			} `json:"rows"`
			Accounts []struct {
				ID             int64   `json:"id"`
				RateMultiplier float64 `json:"rate_multiplier"`
				Schedulable    bool    `json:"schedulable"`
				LastUsedAt     *string `json:"last_used_at"`
			} `json:"accounts"`
			Groups []struct {
				ID             int64   `json:"id"`
				RateMultiplier float64 `json:"rate_multiplier"`
			} `json:"groups"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, 0, body.Code)
	require.Equal(t, "2026-10-01", body.Data.StartDate)
	require.Equal(t, "2026-10-28", body.Data.EndDate)
	require.Len(t, body.Data.Rows, 1)
	require.Equal(t, "glm-5.3-flash", body.Data.Rows[0].Model)
	require.NotNil(t, body.Data.Rows[0].GroupID)
	require.Equal(t, int64(7), *body.Data.Rows[0].GroupID)
	require.InDelta(t, 1.275, body.Data.Rows[0].Revenue, 1e-9)
	require.Len(t, body.Data.Accounts, 1)
	require.NotNil(t, body.Data.Accounts[0].LastUsedAt)
	require.Len(t, body.Data.Groups, 1)
	require.InDelta(t, 0.85, body.Data.Groups[0].RateMultiplier, 1e-9)
}

func TestProfitReport_EmptyReportSerializesArrays(t *testing.T) {
	stub := &stubProfitReporter{report: &service.ProfitReport{
		StartTime: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		EndTime:   time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC),
	}}
	router := setupProfitReportRouter(stub)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/profit-report", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"rows":[]`)
	require.Contains(t, w.Body.String(), `"accounts":[]`)
	require.Contains(t, w.Body.String(), `"groups":[]`)
}

func TestProfitReport_ServiceErrors(t *testing.T) {
	t.Run("bad request from service", func(t *testing.T) {
		stub := &stubProfitReporter{err: service.ErrProfitReportRangeTooLarge}
		router := setupProfitReportRouter(stub)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/profit-report?start_date=2020-01-01&end_date=2026-10-28", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Contains(t, w.Body.String(), "PROFIT_REPORT_RANGE_TOO_LARGE")
	})
	t.Run("internal error", func(t *testing.T) {
		stub := &stubProfitReporter{err: errors.New("db down")}
		router := setupProfitReportRouter(stub)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/profit-report", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusInternalServerError, w.Code)
	})
	t.Run("application error passthrough", func(t *testing.T) {
		stub := &stubProfitReporter{err: infraerrors.BadRequest("X", "y")}
		router := setupProfitReportRouter(stub)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/profit-report", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusBadRequest, w.Code)
	})
}
