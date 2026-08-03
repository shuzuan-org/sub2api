//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// retryAfterAccountRepoStub records the reset time handed to SetRateLimited so
// tests can assert on the cooldown that handle429 derived.
type retryAfterAccountRepoStub struct {
	mockAccountRepoForGemini
	rateLimitCalls int
	lastResetAt    time.Time
}

func (r *retryAfterAccountRepoStub) SetRateLimited(ctx context.Context, id int64, resetAt time.Time) error {
	r.rateLimitCalls++
	r.lastResetAt = resetAt
	return nil
}

func TestParseRetryAfterHeader(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)

	t.Run("returns nil when header is absent", func(t *testing.T) {
		require.Nil(t, parseRetryAfterHeader(http.Header{}, now))
		require.Nil(t, parseRetryAfterHeader(nil, now))
	})

	t.Run("returns nil when header is unparseable", func(t *testing.T) {
		require.Nil(t, parseRetryAfterHeader(http.Header{"Retry-After": []string{"soon"}}, now))
	})

	t.Run("parses delta-seconds", func(t *testing.T) {
		got := parseRetryAfterHeader(http.Header{"Retry-After": []string{"30"}}, now)
		require.NotNil(t, got)
		require.Equal(t, now.Add(30*time.Second), *got)
	})

	t.Run("parses HTTP-date", func(t *testing.T) {
		got := parseRetryAfterHeader(http.Header{"Retry-After": []string{now.Add(90 * time.Second).Format(http.TimeFormat)}}, now)
		require.NotNil(t, got)
		require.Equal(t, now.Add(90*time.Second), *got)
	})

	t.Run("floors zero and past values so we do not immediately re-hammer upstream", func(t *testing.T) {
		zero := parseRetryAfterHeader(http.Header{"Retry-After": []string{"0"}}, now)
		require.NotNil(t, zero)
		require.Equal(t, now.Add(retryAfterMinCooldown), *zero)

		past := parseRetryAfterHeader(http.Header{"Retry-After": []string{now.Add(-time.Hour).Format(http.TimeFormat)}}, now)
		require.NotNil(t, past)
		require.Equal(t, now.Add(retryAfterMinCooldown), *past)
	})

	t.Run("caps absurd values", func(t *testing.T) {
		got := parseRetryAfterHeader(http.Header{"Retry-After": []string{"99999999"}}, now)
		require.NotNil(t, got)
		require.Equal(t, now.Add(retryAfterMaxCooldown), *got)
	})
}

func TestHandle429_DeepSeekHonorsRetryAfter(t *testing.T) {
	newDeepSeekAccount := func() *Account {
		return &Account{
			ID:          42,
			Platform:    PlatformDeepSeek,
			Type:        AccountTypeAPIKey,
			Credentials: map[string]any{"api_key": "sk-deep"},
		}
	}

	t.Run("Retry-After drives the cooldown instead of the 5m default", func(t *testing.T) {
		repo := &retryAfterAccountRepoStub{}
		svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)

		before := time.Now()
		svc.HandleUpstreamError(context.Background(), newDeepSeekAccount(), http.StatusTooManyRequests,
			http.Header{"Retry-After": []string{"20"}}, []byte(`{"error":{"message":"rate limit"}}`))

		require.Equal(t, 1, repo.rateLimitCalls)
		cooldown := repo.lastResetAt.Sub(before)
		require.Greater(t, cooldown, 15*time.Second)
		require.Less(t, cooldown, 30*time.Second)
	})

	t.Run("falls back to the 5m default when upstream omits Retry-After", func(t *testing.T) {
		repo := &retryAfterAccountRepoStub{}
		svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)

		before := time.Now()
		svc.HandleUpstreamError(context.Background(), newDeepSeekAccount(), http.StatusTooManyRequests,
			http.Header{}, []byte(`{"error":{"message":"rate limit"}}`))

		require.Equal(t, 1, repo.rateLimitCalls)
		cooldown := repo.lastResetAt.Sub(before)
		require.Greater(t, cooldown, 4*time.Minute)
		require.Less(t, cooldown, 6*time.Minute)
	})
}
