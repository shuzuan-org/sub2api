package service

import (
	"testing"

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
