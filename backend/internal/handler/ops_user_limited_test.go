package handler

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestClassifyOpsLimitAndSeverity(t *testing.T) {
	const pending = "Too many pending requests, please retry later"

	t.Run("user_limited_is_business_limited_p3", func(t *testing.T) {
		limited, severity := classifyOpsLimitAndSeverity(true, "rate_limit_error", "request", "", http.StatusTooManyRequests, pending)
		require.True(t, limited)
		require.Equal(t, "P3", severity)
	})

	t.Run("account_queue_full_stays_real_error", func(t *testing.T) {
		// 账号级排队满与用户级同文案，但未打标记：属于容量不足，必须仍计入真实错误。
		limited, severity := classifyOpsLimitAndSeverity(false, "rate_limit_error", "request", "", http.StatusTooManyRequests, pending)
		require.False(t, limited)
		require.Equal(t, "P1", severity)
	})

	t.Run("existing_code_based_limits_unchanged", func(t *testing.T) {
		limited, _ := classifyOpsLimitAndSeverity(false, "billing_error", "request", opsCodeInsufficientBalance, http.StatusForbidden, "")
		require.True(t, limited)
	})
}

func TestOpsUserLimitedMark(t *testing.T) {
	c, _ := newHelperTestContext(http.MethodPost, "/v1/messages")
	require.False(t, isOpsUserLimited(c))
	markOpsUserLimited(c)
	require.True(t, isOpsUserLimited(c))

	require.NotPanics(t, func() { markOpsUserLimited(nil) })
	require.False(t, isOpsUserLimited(nil))
}

func TestAcquireUserSlotWithWait_MarksUserLimitedOnTimeout(t *testing.T) {
	cache := &helperConcurrencyCacheStub{} // 用户槽位永远抢不到
	helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 5*time.Millisecond)
	c, _ := newHelperTestContext(http.MethodPost, "/v1/messages")
	ctx, cancel := context.WithTimeout(c.Request.Context(), 40*time.Millisecond)
	defer cancel()
	c.Request = c.Request.WithContext(ctx)

	streamStarted := false
	release, err := helper.AcquireUserSlotWithWait(c, 7, 1, false, &streamStarted)
	require.Nil(t, release)
	var cErr *ConcurrencyError
	require.ErrorAs(t, err, &cErr)
	require.True(t, isOpsUserLimited(c))
}

func TestAcquireUserSlotWithWait_InfraErrorNotMarked(t *testing.T) {
	cache := &helperUserSlotErrorStub{err: errors.New("redis unavailable")}
	helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 5*time.Millisecond)
	c, _ := newHelperTestContext(http.MethodPost, "/v1/messages")

	streamStarted := false
	_, err := helper.AcquireUserSlotWithWait(c, 7, 1, false, &streamStarted)
	require.Error(t, err)
	require.False(t, isOpsUserLimited(c), "Redis 故障是真实错误，不能被当成用户业务限制吞掉")
}

func TestAcquireAccountSlotTimeout_NotMarkedUserLimited(t *testing.T) {
	cache := &helperConcurrencyCacheStub{}
	helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 5*time.Millisecond)
	c, _ := newHelperTestContext(http.MethodPost, "/v1/messages")

	streamStarted := false
	_, err := helper.AcquireAccountSlotWithWaitTimeout(c, 301, 1, 30*time.Millisecond, false, &streamStarted)
	require.Error(t, err)
	require.False(t, isOpsUserLimited(c))
}

type helperUserSlotErrorStub struct {
	helperConcurrencyCacheStub
	err error
}

func (s *helperUserSlotErrorStub) AcquireUserSlot(ctx context.Context, userID int64, maxConcurrency int, requestID string) (bool, error) {
	return false, s.err
}
