package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAccount_DeepSeekHelpers(t *testing.T) {
	t.Run("non-deepseek account returns empty base URL and api key", func(t *testing.T) {
		a := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": "https://api.openai.com", "api_key": "sk-x"}}
		require.False(t, a.IsDeepSeek())
		require.Equal(t, "", a.GetDeepSeekBaseURL())
		require.Equal(t, "", a.GetDeepSeekAPIKey())
	})

	t.Run("deepseek apikey account uses configured base_url and api_key", func(t *testing.T) {
		a := &Account{
			Platform:    PlatformDeepSeek,
			Type:        AccountTypeAPIKey,
			Credentials: map[string]any{"base_url": "https://proxy.example.com/", "api_key": "sk-deep"},
		}
		require.True(t, a.IsDeepSeek())
		require.Equal(t, "https://proxy.example.com", a.GetDeepSeekBaseURL())
		require.Equal(t, "sk-deep", a.GetDeepSeekAPIKey())
	})

	t.Run("deepseek apikey account without base_url falls back to default", func(t *testing.T) {
		a := &Account{Platform: PlatformDeepSeek, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "sk-deep"}}
		require.Equal(t, "https://api.deepseek.com", a.GetDeepSeekBaseURL())
	})
}

func TestParseDeepSeekUsage(t *testing.T) {
	t.Run("returns nil when usage block is missing", func(t *testing.T) {
		require.Nil(t, parseDeepSeekUsage([]byte(`{"id":"x"}`)))
	})

	t.Run("returns nil when all token counts are zero", func(t *testing.T) {
		require.Nil(t, parseDeepSeekUsage([]byte(`{"usage":{"prompt_tokens":0,"completion_tokens":0}}`)))
	})

	t.Run("extracts deepseek-shaped cache hit tokens", func(t *testing.T) {
		usage := parseDeepSeekUsage([]byte(`{"usage":{"prompt_tokens":120,"completion_tokens":40,"prompt_cache_hit_tokens":30}}`))
		require.NotNil(t, usage)
		require.Equal(t, 120, usage.InputTokens)
		require.Equal(t, 40, usage.OutputTokens)
		require.Equal(t, 30, usage.CacheReadInputTokens)
	})

	t.Run("falls back to openai-shaped cached_tokens", func(t *testing.T) {
		usage := parseDeepSeekUsage([]byte(`{"usage":{"prompt_tokens":50,"completion_tokens":10,"prompt_tokens_details":{"cached_tokens":12}}}`))
		require.NotNil(t, usage)
		require.Equal(t, 12, usage.CacheReadInputTokens)
	})
}

func TestEnsureDeepSeekStreamIncludeUsage(t *testing.T) {
	t.Run("adds include_usage when missing", func(t *testing.T) {
		out := ensureDeepSeekStreamIncludeUsage([]byte(`{"model":"deepseek-chat","stream":true}`))
		require.True(t, gjson.GetBytes(out, "stream_options.include_usage").Bool())
	})

	t.Run("preserves include_usage when already true", func(t *testing.T) {
		in := []byte(`{"model":"deepseek-chat","stream":true,"stream_options":{"include_usage":true}}`)
		out := ensureDeepSeekStreamIncludeUsage(in)
		require.True(t, gjson.GetBytes(out, "stream_options.include_usage").Bool())
	})

	t.Run("merges into existing stream_options without losing fields", func(t *testing.T) {
		in := []byte(`{"model":"deepseek-chat","stream":true,"stream_options":{"foo":"bar"}}`)
		out := ensureDeepSeekStreamIncludeUsage(in)
		require.True(t, gjson.GetBytes(out, "stream_options.include_usage").Bool())
		require.Equal(t, "bar", gjson.GetBytes(out, "stream_options.foo").String())
	})
}

// forwardDeepSeekWithStatus drives ForwardDeepSeekChatCompletions against a stub
// upstream that answers with the given status/body, and reports the error plus
// how much the handler-visible response writer was touched.
func forwardDeepSeekWithStatus(t *testing.T, status int, body string) (error, *httptest.ResponseRecorder, int) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}}
	svc := &GatewayService{httpUpstream: upstream}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("{}"))

	account := &Account{
		ID:          7,
		Name:        "ds",
		Platform:    PlatformDeepSeek,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-deep"},
	}

	_, err := svc.ForwardDeepSeekChatCompletions(context.Background(), c, account, []byte(`{"model":"deepseek-v4-flash"}`))
	return err, rec, c.Writer.Size()
}

func TestForwardDeepSeekChatCompletions_ErrorResponses(t *testing.T) {
	// Regression: a 429 used to be written straight to the client before the
	// failover error was returned. The chat-completions handler compares
	// c.Writer.Size() before/after the forward and treats any change as
	// "response already committed", so the account was never failed over and
	// the raw upstream 429 reached the client.
	t.Run("429 returns failover error without touching the response writer", func(t *testing.T) {
		err, rec, size := forwardDeepSeekWithStatus(t, http.StatusTooManyRequests, `{"error":{"message":"rate limit"}}`)

		var failoverErr *UpstreamFailoverError
		require.ErrorAs(t, err, &failoverErr)
		require.Equal(t, http.StatusTooManyRequests, failoverErr.StatusCode)
		require.Contains(t, string(failoverErr.ResponseBody), "rate limit")

		require.Equal(t, -1, size, "writer must be untouched so the handler can fail over")
		require.Empty(t, rec.Body.String())
	})

	t.Run("5xx also fails over without writing", func(t *testing.T) {
		err, _, size := forwardDeepSeekWithStatus(t, http.StatusBadGateway, `{"error":{"message":"upstream down"}}`)

		var failoverErr *UpstreamFailoverError
		require.ErrorAs(t, err, &failoverErr)
		require.Equal(t, -1, size)
	})

	t.Run("400 is terminal and passes the upstream body through", func(t *testing.T) {
		err, rec, _ := forwardDeepSeekWithStatus(t, http.StatusBadRequest, `{"error":{"message":"bad model"}}`)

		var failoverErr *UpstreamFailoverError
		require.False(t, errors.As(err, &failoverErr), "400 must not trigger account failover")
		require.Error(t, err)
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Contains(t, rec.Body.String(), "bad model")
	})
}
