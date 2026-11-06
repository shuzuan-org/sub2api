package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type stubReconciler struct {
	rec        *service.ProfitReconcile
	err        error
	gotStart   string
	gotEnd     string
	gotAccount int64
	gotSup     [2]string
	gotPeriods []string
	gotLines   []service.SupplierBillLine
}

func (s *stubReconciler) GetReconcile(_ context.Context, start, end string) (*service.ProfitReconcile, error) {
	s.gotStart, s.gotEnd = start, end
	return s.rec, s.err
}
func (s *stubReconciler) SaveBinding(_ context.Context, id int64, sup, acc string) error {
	s.gotAccount, s.gotSup = id, [2]string{sup, acc}
	return s.err
}
func (s *stubReconciler) ReplaceBills(_ context.Context, periods []string, lines []service.SupplierBillLine) error {
	s.gotPeriods, s.gotLines = periods, lines
	return s.err
}

func setupReconcileRouter(stub *stubReconciler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := &ProfitReportHandler{rec: stub}
	router.GET("/reconcile", h.GetReconcile)
	router.PUT("/bindings/:account_id", h.SaveSupplierBinding)
	router.PUT("/bills", h.ReplaceSupplierBills)
	return router
}

func TestGetReconcile_SerializesNullsAndDefaultsEndPeriod(t *testing.T) {
	disc := 0.55
	stub := &stubReconciler{rec: &service.ProfitReconcile{
		GeneratedAt: time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC),
		StartPeriod: "2026-08", EndPeriod: "2026-08", Timezone: "Asia/Shanghai",
		Rows:     []service.ReconcileUsageRow{{Period: "2026-08", Model: "glm-5.2", AccountID: 7, Requests: 3, ListPrice: 10, Revenue: 8}},
		Accounts: []service.ProfitReportAccount{{ID: 7, Name: "ks-anthropic-glm-5.2", RateMultiplier: 0.5522}},
		Bindings: []service.SupplierBinding{{AccountID: 7, Supplier: "金山云", SupplierAccount: "2000186841"}},
		Bills:    []service.SupplierBillLine{{Period: "2026-08", StandardModel: "glm-5.2", CostDiscount: &disc}},
	}}
	router := setupReconcileRouter(stub)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/reconcile?start_period=2026-08", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "2026-08", stub.gotEnd, "end_period defaults to start_period")

	var body struct {
		Data struct {
			Timezone string `json:"timezone"`
			Rows     []map[string]any
			Accounts []map[string]any
			Bindings []map[string]any
			Bills    []map[string]any
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "Asia/Shanghai", body.Data.Timezone)
	require.Equal(t, "glm-5.2", body.Data.Rows[0]["model"])
	require.Equal(t, "ks-anthropic-glm-5.2", body.Data.Accounts[0]["name"])
	require.Equal(t, "2000186841", body.Data.Bindings[0]["supplier_account"])
	require.Equal(t, 0.55, body.Data.Bills[0]["cost_discount"])
	v, ok := body.Data.Bills[0]["resource_fee"]
	require.True(t, ok)
	require.Nil(t, v, "blank numbers serialize as null, not 0")
}

func TestGetReconcile_ServiceErrorIs400(t *testing.T) {
	stub := &stubReconciler{err: service.ErrReconcilePeriodInvalid}
	w := httptest.NewRecorder()
	setupReconcileRouter(stub).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/reconcile?start_period=bad", nil))
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSaveSupplierBinding(t *testing.T) {
	stub := &stubReconciler{}
	router := setupReconcileRouter(stub)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/bindings/7", strings.NewReader(`{"supplier":"金山云","supplier_account":"2000186841"}`)))
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, int64(7), stub.gotAccount)
	require.Equal(t, [2]string{"金山云", "2000186841"}, stub.gotSup)

	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/bindings/abc", strings.NewReader(`{}`)))
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestReplaceSupplierBills(t *testing.T) {
	stub := &stubReconciler{}
	router := setupReconcileRouter(stub)
	body := `{"periods":["2026-08"],"lines":[
		{"period":"2026-08","supplier":"金山云","supplier_account":"2000186841","standard_model":"glm-5.2","resource_fee":46646.86,"discount_amount":null},
		{"period":"2026-08","standard_model":"调账","adjustment":-6120.65}]}`
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/bills", strings.NewReader(body)))
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, []string{"2026-08"}, stub.gotPeriods)
	require.Len(t, stub.gotLines, 2)
	require.InDelta(t, 46646.86, *stub.gotLines[0].ResourceFee, 1e-9)
	require.Nil(t, stub.gotLines[0].DiscountAmount)
	require.InDelta(t, -6120.65, *stub.gotLines[1].Adjustment, 1e-9)

	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/bills", strings.NewReader(`not json`)))
	require.Equal(t, http.StatusBadRequest, w.Code)
}
