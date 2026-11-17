package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestWindowTTLMillis(t *testing.T) {
	require.Equal(t, int64(1), windowTTLMillis(500*time.Microsecond))
	require.Equal(t, int64(1), windowTTLMillis(1500*time.Microsecond))
	require.Equal(t, int64(2), windowTTLMillis(2500*time.Microsecond))
}

func TestRateLimiterFailureModes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rdb := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  50 * time.Millisecond,
		ReadTimeout:  50 * time.Millisecond,
		WriteTimeout: 50 * time.Millisecond,
	})
	t.Cleanup(func() {
		_ = rdb.Close()
	})

	limiter := NewRateLimiter(rdb)

	failOpenRouter := gin.New()
	failOpenRouter.Use(limiter.Limit("test", 1, time.Second))
	failOpenRouter.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	recorder := httptest.NewRecorder()
	failOpenRouter.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code)

	failCloseRouter := gin.New()
	failCloseRouter.Use(limiter.LimitWithOptions("test", 1, time.Second, RateLimitOptions{
		FailureMode: RateLimitFailClose,
	}))
	failCloseRouter.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req = httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	recorder = httptest.NewRecorder()
	failCloseRouter.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
}

func TestRateLimiterDifferentIPsIndependent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	callCounts := make(map[string]int64)
	originalRun := rateLimitRun
	rateLimitRun = func(ctx context.Context, client *redis.Client, key string, windowMillis int64) (int64, bool, error) {
		callCounts[key]++
		return callCounts[key], false, nil
	}
	t.Cleanup(func() {
		rateLimitRun = originalRun
	})

	limiter := NewRateLimiter(redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"}))

	router := gin.New()
	router.Use(limiter.Limit("api", 1, time.Second))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	// 第一个 IP 的请求应通过
	req1 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req1.RemoteAddr = "10.0.0.1:1234"
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusOK, rec1.Code, "第一个 IP 的第一次请求应通过")

	// 第二个 IP 的请求应独立通过（不受第一个 IP 的计数影响）
	req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req2.RemoteAddr = "10.0.0.2:5678"
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code, "第二个 IP 的第一次请求应独立通过")

	// 第一个 IP 的第二次请求应被限流
	req3 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req3.RemoteAddr = "10.0.0.1:1234"
	rec3 := httptest.NewRecorder()
	router.ServeHTTP(rec3, req3)
	require.Equal(t, http.StatusTooManyRequests, rec3.Code, "第一个 IP 的第二次请求应被限流")
}

func TestRateLimiterSuccessAndLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalRun := rateLimitRun
	counts := []int64{1, 2}
	callIndex := 0
	rateLimitRun = func(ctx context.Context, client *redis.Client, key string, windowMillis int64) (int64, bool, error) {
		if callIndex >= len(counts) {
			return counts[len(counts)-1], false, nil
		}
		value := counts[callIndex]
		callIndex++
		return value, false, nil
	}
	t.Cleanup(func() {
		rateLimitRun = originalRun
	})

	limiter := NewRateLimiter(redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"}))

	router := gin.New()
	router.Use(limiter.Limit("test", 1, time.Second))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code)

	req = httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
}

func TestRateLimiterGlobalSharesCounterAcrossIPs(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalRun := rateLimitRun
	counters := map[string]int64{}
	rateLimitRun = func(ctx context.Context, client *redis.Client, key string, windowMillis int64) (int64, bool, error) {
		counters[key]++
		return counters[key], false, nil
	}
	t.Cleanup(func() {
		rateLimitRun = originalRun
	})

	limiter := NewRateLimiter(redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"}))

	router := gin.New()
	router.Use(limiter.LimitWithOptions("signup", 2, time.Hour, RateLimitOptions{Global: true}))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	codes := make([]int, 0, 3)
	for _, addr := range []string{"203.0.113.1:1000", "203.0.113.2:1000", "203.0.113.3:1000"} {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = addr
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		codes = append(codes, recorder.Code)
	}

	// 三个不同 IP 共用一个计数器：第三个请求触发熔断。
	require.Equal(t, []int{http.StatusOK, http.StatusOK, http.StatusTooManyRequests}, codes)
	require.Equal(t, map[string]int64{"rate_limit:signup:global": 3}, counters)
}

func TestRateLimiterLimitFunc(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalRun := rateLimitRun
	calls := 0
	rateLimitRun = func(ctx context.Context, client *redis.Client, key string, windowMillis int64) (int64, bool, error) {
		calls++
		return int64(calls), false, nil
	}
	t.Cleanup(func() {
		rateLimitRun = originalRun
	})

	limiter := NewRateLimiter(redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"}))
	dynamicLimit := 0

	router := gin.New()
	router.Use(limiter.LimitWithOptions("signup", 1, time.Hour, RateLimitOptions{
		FailureMode: RateLimitFailClose,
		LimitFunc:   func(context.Context) int { return dynamicLimit },
	}))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	do := func() int {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "203.0.113.1:1000"
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		return recorder.Code
	}

	// 上限 <= 0：不限流，也不访问 Redis。
	require.Equal(t, http.StatusOK, do())
	require.Equal(t, 0, calls)

	// 动态上限覆盖静态 limit(1)。
	dynamicLimit = 2
	require.Equal(t, http.StatusOK, do())
	require.Equal(t, http.StatusOK, do())
	require.Equal(t, http.StatusTooManyRequests, do())
}
