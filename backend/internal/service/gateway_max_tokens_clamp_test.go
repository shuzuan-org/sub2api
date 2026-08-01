//go:build unit

package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGetMaxOutputTokens(t *testing.T) {
	tests := []struct {
		name     string
		account  *Account
		expected int
	}{
		{
			name:     "nil_account",
			account:  nil,
			expected: 0,
		},
		{
			name:     "nil_credentials",
			account:  &Account{},
			expected: 0,
		},
		{
			name:     "not_configured",
			account:  &Account{Credentials: map[string]any{"api_key": "x"}},
			expected: 0,
		},
		{
			name:     "explicit_null",
			account:  &Account{Credentials: map[string]any{"max_output_tokens": nil}},
			expected: 0,
		},
		{
			name:     "float_from_json",
			account:  &Account{Credentials: map[string]any{"max_output_tokens": float64(128000)}},
			expected: 128000,
		},
		{
			name:     "int",
			account:  &Account{Credentials: map[string]any{"max_output_tokens": 64000}},
			expected: 64000,
		},
		{
			name:     "json_number",
			account:  &Account{Credentials: map[string]any{"max_output_tokens": json.Number("32000")}},
			expected: 32000,
		},
		{
			name:     "numeric_string",
			account:  &Account{Credentials: map[string]any{"max_output_tokens": " 8192 "}},
			expected: 8192,
		},
		// 非法值一律按「未配置」处理：宁可不夹紧，也不能因为配错了就把请求改坏。
		{
			name:     "zero_means_unset",
			account:  &Account{Credentials: map[string]any{"max_output_tokens": 0}},
			expected: 0,
		},
		{
			name:     "negative_means_unset",
			account:  &Account{Credentials: map[string]any{"max_output_tokens": -1}},
			expected: 0,
		},
		{
			name:     "garbage_string_means_unset",
			account:  &Account{Credentials: map[string]any{"max_output_tokens": "lots"}},
			expected: 0,
		},
		{
			name:     "bool_means_unset",
			account:  &Account{Credentials: map[string]any{"max_output_tokens": true}},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.account.GetMaxOutputTokens())
		})
	}
}

func TestClampMaxTokens(t *testing.T) {
	tests := []struct {
		name            string
		body            string
		limit           int
		wantChanged     bool
		wantMaxTokens   int64 // wantChanged 为 true 时校验
		wantBudget      int64 // >0 时校验 thinking.budget_tokens
		wantFieldAbsent bool  // 校验没有凭空写入 max_tokens
	}{
		{
			// 事故原型：客户端报 1000000，上游按 max_tokens 预扣配额把团队窗口打穿。
			name:          "clamps_the_incident_value",
			body:          `{"model":"DeepSeek-V4-Flash","max_tokens":1000000}`,
			limit:         128000,
			wantChanged:   true,
			wantMaxTokens: 128000,
		},
		{
			name:        "under_limit_untouched",
			body:        `{"max_tokens":4096}`,
			limit:       128000,
			wantChanged: false,
		},
		{
			name:        "exactly_at_limit_untouched",
			body:        `{"max_tokens":128000}`,
			limit:       128000,
			wantChanged: false,
		},
		{
			name:        "limit_unset_is_noop",
			body:        `{"max_tokens":1000000}`,
			limit:       0,
			wantChanged: false,
		},
		{
			name:        "negative_limit_is_noop",
			body:        `{"max_tokens":1000000}`,
			limit:       -5,
			wantChanged: false,
		},
		{
			// 不能凭空写入，否则会改变上游对「未声明 max_tokens」的默认行为。
			name:            "absent_field_not_injected",
			body:            `{"model":"x"}`,
			limit:           128000,
			wantChanged:     false,
			wantFieldAbsent: true,
		},
		{
			name:        "non_numeric_field_untouched",
			body:        `{"max_tokens":"1000000"}`,
			limit:       128000,
			wantChanged: false,
		},
		{
			// budget 小于 limit，夹紧后仍满足 max_tokens > budget_tokens，budget 不动。
			name:          "thinking_budget_below_limit_preserved",
			body:          `{"max_tokens":1000000,"thinking":{"type":"enabled","budget_tokens":32000}}`,
			limit:         128000,
			wantChanged:   true,
			wantMaxTokens: 128000,
			wantBudget:    32000,
		},
		{
			// budget >= limit：必须连带压低 budget，否则请求变非法（上游 400）。
			name:          "thinking_budget_lowered_with_max_tokens",
			body:          `{"max_tokens":1000000,"thinking":{"type":"enabled","budget_tokens":60000}}`,
			limit:         50000,
			wantChanged:   true,
			wantMaxTokens: 50000,
			wantBudget:    49999,
		},
		{
			// budget 压不到 1024 以上就整体放弃——宁可超限被限流，也不自造 400。
			name:        "gives_up_when_budget_cannot_fit",
			body:        `{"max_tokens":1000000,"thinking":{"type":"enabled","budget_tokens":2000}}`,
			limit:       1024,
			wantChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := ClampMaxTokens([]byte(tt.body), tt.limit)
			require.Equal(t, tt.wantChanged, changed)

			if !tt.wantChanged {
				require.JSONEq(t, tt.body, string(got), "未夹紧时请求体必须原样返回")
			}
			if tt.wantFieldAbsent {
				require.False(t, gjson.GetBytes(got, "max_tokens").Exists())
			}
			if tt.wantChanged {
				require.Equal(t, tt.wantMaxTokens, gjson.GetBytes(got, "max_tokens").Int())
			}
			if tt.wantBudget > 0 {
				require.Equal(t, tt.wantBudget, gjson.GetBytes(got, "thinking.budget_tokens").Int())
			}
		})
	}
}

// 夹紧只动 max_tokens（必要时加 budget_tokens），不得碰其它字段。
func TestClampMaxTokensPreservesOtherFields(t *testing.T) {
	body := `{"model":"DeepSeek-V4-Flash","max_tokens":1000000,"stream":true,` +
		`"messages":[{"role":"user","content":"hi"}],"metadata":{"user_id":"u1"}}`

	got, changed := ClampMaxTokens([]byte(body), 128000)
	require.True(t, changed)

	require.Equal(t, int64(128000), gjson.GetBytes(got, "max_tokens").Int())
	require.Equal(t, "DeepSeek-V4-Flash", gjson.GetBytes(got, "model").String())
	require.True(t, gjson.GetBytes(got, "stream").Bool())
	require.Equal(t, "hi", gjson.GetBytes(got, "messages.0.content").String())
	require.Equal(t, "u1", gjson.GetBytes(got, "metadata.user_id").String())
}

// 幂等：夹紧后的请求体再夹一次不应再改动。
func TestClampMaxTokensIdempotent(t *testing.T) {
	first, changed := ClampMaxTokens([]byte(`{"max_tokens":1000000}`), 128000)
	require.True(t, changed)

	second, changedAgain := ClampMaxTokens(first, 128000)
	require.False(t, changedAgain)
	require.JSONEq(t, string(first), string(second))
}
