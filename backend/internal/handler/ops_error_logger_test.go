package handler

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/metrics"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func resetOpsErrorLoggerStateForTest(t *testing.T) {
	t.Helper()

	opsErrorLogMu.Lock()
	ch := opsErrorLogQueue
	opsErrorLogQueue = nil
	opsErrorLogStopping = true
	opsErrorLogMu.Unlock()

	if ch != nil {
		close(ch)
	}
	opsErrorLogWorkersWg.Wait()

	opsErrorLogOnce = sync.Once{}
	opsErrorLogStopOnce = sync.Once{}
	opsErrorLogWorkersWg = sync.WaitGroup{}
	opsErrorLogMu = sync.RWMutex{}
	opsErrorLogStopping = false

	opsErrorLogQueueLen.Store(0)
	opsErrorLogEnqueued.Store(0)
	opsErrorLogDropped.Store(0)
	opsErrorLogProcessed.Store(0)
	opsErrorLogSanitized.Store(0)
	opsErrorLogLastDropLogAt.Store(0)

	opsErrorLogShutdownCh = make(chan struct{})
	opsErrorLogShutdownOnce = sync.Once{}
	opsErrorLogDrained.Store(false)
}

func TestAttachOpsRequestBodyToEntry_SanitizeAndTrim(t *testing.T) {
	resetOpsErrorLoggerStateForTest(t)
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	raw := []byte(`{"access_token":"secret-token","messages":[{"role":"user","content":"hello"}]}`)
	setOpsRequestContext(c, "claude-3", false, raw)

	entry := &service.OpsInsertErrorLogInput{}
	attachOpsRequestBodyToEntry(c, entry)

	require.NotNil(t, entry.RequestBodyBytes)
	require.Equal(t, len(raw), *entry.RequestBodyBytes)
	require.NotNil(t, entry.RequestBodyJSON)
	require.NotContains(t, *entry.RequestBodyJSON, "secret-token")
	require.Contains(t, *entry.RequestBodyJSON, "[REDACTED]")
	require.Equal(t, int64(1), OpsErrorLogSanitizedTotal())
}

func TestAttachOpsRequestBodyToEntry_InvalidJSONKeepsSize(t *testing.T) {
	resetOpsErrorLoggerStateForTest(t)
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	raw := []byte("not-json")
	setOpsRequestContext(c, "claude-3", false, raw)

	entry := &service.OpsInsertErrorLogInput{}
	attachOpsRequestBodyToEntry(c, entry)

	require.Nil(t, entry.RequestBodyJSON)
	require.NotNil(t, entry.RequestBodyBytes)
	require.Equal(t, len(raw), *entry.RequestBodyBytes)
	require.False(t, entry.RequestBodyTruncated)
	require.Equal(t, int64(1), OpsErrorLogSanitizedTotal())
}

func TestEnqueueOpsErrorLog_QueueFullDrop(t *testing.T) {
	resetOpsErrorLoggerStateForTest(t)

	// 禁止 enqueueOpsErrorLog 触发 workers，使用测试队列验证满队列降级。
	opsErrorLogOnce.Do(func() {})

	opsErrorLogMu.Lock()
	opsErrorLogQueue = make(chan opsErrorLogJob, 1)
	opsErrorLogMu.Unlock()

	ops := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	entry := &service.OpsInsertErrorLogInput{ErrorPhase: "upstream", ErrorType: "upstream_error"}

	enqueueOpsErrorLog(ops, entry)
	enqueueOpsErrorLog(ops, entry)

	require.Equal(t, int64(1), OpsErrorLogEnqueuedTotal())
	require.Equal(t, int64(1), OpsErrorLogDroppedTotal())
	require.Equal(t, int64(1), OpsErrorLogQueueLength())
}

func TestAttachOpsRequestBodyToEntry_EarlyReturnBranches(t *testing.T) {
	resetOpsErrorLoggerStateForTest(t)
	gin.SetMode(gin.TestMode)

	entry := &service.OpsInsertErrorLogInput{}
	attachOpsRequestBodyToEntry(nil, entry)
	attachOpsRequestBodyToEntry(&gin.Context{}, nil)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	// 无请求体 key
	attachOpsRequestBodyToEntry(c, entry)
	require.Nil(t, entry.RequestBodyJSON)
	require.Nil(t, entry.RequestBodyBytes)
	require.False(t, entry.RequestBodyTruncated)

	// 错误类型
	c.Set(opsRequestBodyKey, "not-bytes")
	attachOpsRequestBodyToEntry(c, entry)
	require.Nil(t, entry.RequestBodyJSON)
	require.Nil(t, entry.RequestBodyBytes)

	// 空 bytes
	c.Set(opsRequestBodyKey, []byte{})
	attachOpsRequestBodyToEntry(c, entry)
	require.Nil(t, entry.RequestBodyJSON)
	require.Nil(t, entry.RequestBodyBytes)

	require.Equal(t, int64(0), OpsErrorLogSanitizedTotal())
}

func TestEnqueueOpsErrorLog_EarlyReturnBranches(t *testing.T) {
	resetOpsErrorLoggerStateForTest(t)

	ops := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	entry := &service.OpsInsertErrorLogInput{ErrorPhase: "upstream", ErrorType: "upstream_error"}

	// nil 入参分支
	enqueueOpsErrorLog(nil, entry)
	enqueueOpsErrorLog(ops, nil)
	require.Equal(t, int64(0), OpsErrorLogEnqueuedTotal())

	// shutdown 分支
	close(opsErrorLogShutdownCh)
	enqueueOpsErrorLog(ops, entry)
	require.Equal(t, int64(0), OpsErrorLogEnqueuedTotal())

	// stopping 分支
	resetOpsErrorLoggerStateForTest(t)
	opsErrorLogMu.Lock()
	opsErrorLogStopping = true
	opsErrorLogMu.Unlock()
	enqueueOpsErrorLog(ops, entry)
	require.Equal(t, int64(0), OpsErrorLogEnqueuedTotal())

	// queue nil 分支（防止启动 worker 干扰）
	resetOpsErrorLoggerStateForTest(t)
	opsErrorLogOnce.Do(func() {})
	opsErrorLogMu.Lock()
	opsErrorLogQueue = nil
	opsErrorLogMu.Unlock()
	enqueueOpsErrorLog(ops, entry)
	require.Equal(t, int64(0), OpsErrorLogEnqueuedTotal())
}

func TestOpsCaptureWriterPool_ResetOnRelease(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/test", nil)

	writer := acquireOpsCaptureWriter(c.Writer)
	require.NotNil(t, writer)
	_, err := writer.buf.WriteString("temp-error-body")
	require.NoError(t, err)

	releaseOpsCaptureWriter(writer)

	reused := acquireOpsCaptureWriter(c.Writer)
	defer releaseOpsCaptureWriter(reused)

	require.Zero(t, reused.buf.Len(), "writer should be reset before reuse")
}

func TestOpsErrorLoggerMiddleware_DoesNotBreakOuterMiddlewares(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(middleware2.Recovery())
	r.Use(middleware2.RequestLogger())
	r.Use(middleware2.Logger())
	r.GET("/v1/messages", OpsErrorLoggerMiddleware(nil), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/messages", nil)

	require.NotPanics(t, func() {
		r.ServeHTTP(rec, req)
	})
	require.Equal(t, http.StatusNoContent, rec.Code)
}

func TestIsKnownOpsErrorType(t *testing.T) {
	known := []string{
		"invalid_request_error",
		"authentication_error",
		"rate_limit_error",
		"billing_error",
		"subscription_error",
		"upstream_error",
		"overloaded_error",
		"api_error",
		"not_found_error",
		"forbidden_error",
	}
	for _, k := range known {
		require.True(t, isKnownOpsErrorType(k), "expected known: %s", k)
	}

	unknown := []string{"<nil>", "null", "", "random_error", "some_new_type", "<nil>\u003e"}
	for _, u := range unknown {
		require.False(t, isKnownOpsErrorType(u), "expected unknown: %q", u)
	}
}

func TestNormalizeOpsErrorType(t *testing.T) {
	tests := []struct {
		name    string
		errType string
		code    string
		want    string
	}{
		// Known types pass through.
		{"known invalid_request_error", "invalid_request_error", "", "invalid_request_error"},
		{"known rate_limit_error", "rate_limit_error", "", "rate_limit_error"},
		{"known upstream_error", "upstream_error", "", "upstream_error"},

		// Unknown/garbage types are rejected and fall through to code-based or default.
		{"nil literal from upstream", "<nil>", "", "api_error"},
		{"null string", "null", "", "api_error"},
		{"random string", "something_weird", "", "api_error"},

		// Unknown type but known code still maps correctly.
		{"nil with INSUFFICIENT_BALANCE code", "<nil>", "INSUFFICIENT_BALANCE", "billing_error"},
		{"nil with USAGE_LIMIT_EXCEEDED code", "<nil>", "USAGE_LIMIT_EXCEEDED", "subscription_error"},

		// Empty type falls through to code-based mapping.
		{"empty type with balance code", "", "INSUFFICIENT_BALANCE", "billing_error"},
		{"empty type with subscription code", "", "SUBSCRIPTION_NOT_FOUND", "subscription_error"},
		{"empty type no code", "", "", "api_error"},

		// API key 鉴权中间件的错误码只带 code，归鉴权错误而不是 api_error。
		{"empty type with API_KEY_REQUIRED", "", "API_KEY_REQUIRED", "authentication_error"},
		{"empty type with INVALID_API_KEY", "", "INVALID_API_KEY", "authentication_error"},
		{"empty type with API_KEY_DISABLED", "", "API_KEY_DISABLED", "authentication_error"},
		{"empty type with API_KEY_EXPIRED", "", "API_KEY_EXPIRED", "authentication_error"},
		{"empty type with USER_NOT_FOUND", "", "USER_NOT_FOUND", "authentication_error"},
		{"empty type with USER_INACTIVE", "", "USER_INACTIVE", "authentication_error"},

		// Known type overrides conflicting code-based mapping.
		{"known type overrides conflicting code", "rate_limit_error", "INSUFFICIENT_BALANCE", "rate_limit_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeOpsErrorType(tt.errType, tt.code)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestClassifyOps_APIKeyAuthCodesAreAuthNotInternal 回归：未带 / 无效 API key 的 401 曾被记成
// phase=internal、severity=P2，119 上未登录 gate 探测把 Sub2apiHighRealErrorRatio 顶到 5.9%。
func TestClassifyOps_APIKeyAuthCodesAreAuthNotInternal(t *testing.T) {
	for _, code := range []string{"API_KEY_REQUIRED", "INVALID_API_KEY", "API_KEY_DISABLED", "API_KEY_EXPIRED", "USER_NOT_FOUND"} {
		t.Run(code, func(t *testing.T) {
			errType := normalizeOpsErrorType("", code)
			phase := classifyOpsPhase(errType, "", code)
			limited, severity := classifyOpsLimitAndSeverity(false, errType, phase, code, http.StatusUnauthorized, "")
			require.Equal(t, "auth", phase)
			require.Equal(t, "P3", severity)
			require.False(t, limited)
		})
	}
	// USER_INACTIVE 原本就算业务限制，改类型后仍保持。
	errType := normalizeOpsErrorType("", "USER_INACTIVE")
	phase := classifyOpsPhase(errType, "", "USER_INACTIVE")
	limited, severity := classifyOpsLimitAndSeverity(false, errType, phase, "USER_INACTIVE", http.StatusUnauthorized, "")
	require.Equal(t, "auth", phase)
	require.Equal(t, "P3", severity)
	require.True(t, limited)
}

// TestClassifyOps_APIKeyAuthResponseBody 端到端：从中间件真实写出的响应体开始，走
// parseOpsErrorResponse → normalizeOpsErrorType → classifyOpsPhase，与 opsErrorLoggerMiddleware 同路径。
// （只测 normalizeOpsErrorType("", code) 不够：解析器对 {code,message} 体会先填 type=api_error。）
func TestClassifyOps_APIKeyAuthResponseBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		code    string
		status  int
		phase   string
		limited bool
	}{
		{"API_KEY_REQUIRED", http.StatusUnauthorized, "auth", false},
		{"INVALID_API_KEY", http.StatusUnauthorized, "auth", false},
		{"USER_INACTIVE", http.StatusUnauthorized, "auth", true},
		// 非鉴权码维持原分类，避免顺带改动余额 / 订阅错误。
		{"INSUFFICIENT_BALANCE", http.StatusForbidden, "request", true},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			middleware2.AbortWithError(c, tc.status, tc.code, "msg")

			parsed := parseOpsErrorResponse(w.Body.Bytes())
			errType := normalizeOpsErrorType(parsed.ErrorType, parsed.Code)
			phase := classifyOpsPhase(errType, parsed.Message, parsed.Code)
			limited, severity := classifyOpsLimitAndSeverity(false, errType, phase, parsed.Code, tc.status, parsed.Message)
			require.Equal(t, tc.phase, phase)
			require.Equal(t, tc.limited, limited)
			if tc.phase == "auth" {
				require.Equal(t, "authentication_error", errType)
				require.Equal(t, "P3", severity)
			}
		})
	}
}

// TestNormalizeOpsErrorType_OutputAlwaysBounded 是基数守卫：normalizeOpsErrorType 的输出
// 直接作为 Prometheus 标签 type，必须始终落在有界枚举内，否则会造成标签爆炸。
func TestNormalizeOpsErrorType_OutputAlwaysBounded(t *testing.T) {
	types := []string{
		"", "<nil>", "null", "weird_type", "500", "429", "SOME_RANDOM",
		"invalid_request_error", "authentication_error", "rate_limit_error",
		"billing_error", "subscription_error", "upstream_error", "overloaded_error",
		"api_error", "not_found_error", "forbidden_error",
	}
	codes := []string{
		"", "INSUFFICIENT_BALANCE", "USAGE_LIMIT_EXCEEDED", "SUBSCRIPTION_NOT_FOUND",
		"SUBSCRIPTION_INVALID", "API_KEY_REQUIRED", "INVALID_API_KEY", "USER_INACTIVE", "random_code", "123",
	}
	for _, et := range types {
		for _, code := range codes {
			got := normalizeOpsErrorType(et, code)
			require.Truef(t, isKnownOpsErrorType(got),
				"normalizeOpsErrorType(%q,%q)=%q escapes the bounded label set", et, code, got)
		}
	}
}

// TestRecordOpsErrorMetric_EmitsCorrectLabels 验证同源打点用 entry 的分类字段递增正确的标签序列。
func TestRecordOpsErrorMetric_EmitsCorrectLabels(t *testing.T) {
	tests := []struct {
		name   string
		entry  *service.OpsInsertErrorLogInput
		labels []string // platform, model, phase, type, severity, business_limited
	}{
		{
			name:   "auth error with model",
			entry:  &service.OpsInsertErrorLogInput{Platform: "anthropic", Model: "claude-sonnet-4-5", ErrorPhase: "auth", ErrorType: "authentication_error", Severity: "P2", IsBusinessLimited: false},
			labels: []string{"anthropic", "claude-sonnet-4-5", "auth", "authentication_error", "P2", "false"},
		},
		{
			name:   "business-limited rate limit",
			entry:  &service.OpsInsertErrorLogInput{Platform: "openai", Model: "gpt-5.1", ErrorPhase: "request", ErrorType: "rate_limit_error", Severity: "P3", IsBusinessLimited: true},
			labels: []string{"openai", "gpt-5.1", "request", "rate_limit_error", "P3", "true"},
		},
		{
			name:   "upstream 5xx",
			entry:  &service.OpsInsertErrorLogInput{Platform: "gemini", Model: "gemini-2.5-pro", ErrorPhase: "upstream", ErrorType: "upstream_error", Severity: "P1", IsBusinessLimited: false},
			labels: []string{"gemini", "gemini-2.5-pro", "upstream", "upstream_error", "P1", "false"},
		},
		{
			name:   "internal error empty platform and model",
			entry:  &service.OpsInsertErrorLogInput{Platform: "", Model: "", ErrorPhase: "internal", ErrorType: "api_error", Severity: "P1", IsBusinessLimited: false},
			labels: []string{"", "", "internal", "api_error", "P1", "false"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 用增量断言，避免受 init() 预初始化及其它测试对全局计数器的影响。
			before := testutil.ToFloat64(metrics.OpsErrorTotal.WithLabelValues(tt.labels...))
			recordOpsErrorMetric(tt.entry)
			after := testutil.ToFloat64(metrics.OpsErrorTotal.WithLabelValues(tt.labels...))
			require.Equal(t, before+1, after)
		})
	}
}

func TestRecordOpsErrorMetric_NilEntryNoPanic(t *testing.T) {
	require.NotPanics(t, func() { recordOpsErrorMetric(nil) })
}

func TestSetOpsEndpointContext_SetsContextKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	setOpsEndpointContext(c, "claude-3-5-sonnet-20241022", int16(2)) // stream

	v, ok := c.Get(opsUpstreamModelKey)
	require.True(t, ok)
	vStr, ok := v.(string)
	require.True(t, ok)
	require.Equal(t, "claude-3-5-sonnet-20241022", vStr)

	rt, ok := c.Get(opsRequestTypeKey)
	require.True(t, ok)
	rtVal, ok := rt.(int16)
	require.True(t, ok)
	require.Equal(t, int16(2), rtVal)
}

func TestSetOpsEndpointContext_EmptyModelNotStored(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	setOpsEndpointContext(c, "", int16(1))

	_, ok := c.Get(opsUpstreamModelKey)
	require.False(t, ok, "empty upstream model should not be stored")

	rt, ok := c.Get(opsRequestTypeKey)
	require.True(t, ok)
	rtVal, ok := rt.(int16)
	require.True(t, ok)
	require.Equal(t, int16(1), rtVal)
}

func TestSetOpsEndpointContext_NilContext(t *testing.T) {
	require.NotPanics(t, func() {
		setOpsEndpointContext(nil, "model", int16(1))
	})
}
