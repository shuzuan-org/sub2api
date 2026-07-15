package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrTypeForUpstreamStatus(t *testing.T) {
	cases := map[int]string{
		400: "invalid_request_error",
		401: "authentication_error",
		403: "permission_error",
		404: "not_found_error",
		429: "rate_limit_error",
		529: "overloaded_error",
		500: "api_error",
		502: "api_error",
		503: "api_error",
		504: "api_error",
		418: "api_error",
	}
	for status, want := range cases {
		assert.Equalf(t, want, ErrTypeForUpstreamStatus(status), "status=%d", status)
	}
}

func TestGenericUpstreamMsg_NonEmpty(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 429, 500, 502, 529} {
		assert.NotEmptyf(t, GenericUpstreamMsg(status), "status=%d should have a fallback message", status)
	}
}

// TestUpstreamPassthroughDefaults 验证透传：状态码原样透传，message 取自上游 body，
// 提取不到时回落到通用文案。
func TestUpstreamPassthroughDefaults(t *testing.T) {
	t.Run("passthrough status + upstream message", func(t *testing.T) {
		body := []byte(`{"type":"error","error":{"type":"invalid_api_key","message":"upstream says key revoked"}}`)
		status, errType, msg := upstreamPassthroughDefaults(401, body)
		require.Equal(t, 401, status) // 状态码透传，不再塑形成 502
		require.Equal(t, "authentication_error", errType)
		require.Equal(t, "upstream says key revoked", msg)
	})

	t.Run("empty body falls back to generic message", func(t *testing.T) {
		status, errType, msg := upstreamPassthroughDefaults(503, nil)
		require.Equal(t, 503, status)
		require.Equal(t, "api_error", errType)
		require.Equal(t, GenericUpstreamMsg(503), msg)
		require.NotEmpty(t, msg)
	})

	t.Run("403 passthrough (not masked to 502)", func(t *testing.T) {
		status, _, _ := upstreamPassthroughDefaults(403, []byte(`{"error":{"message":"forbidden"}}`))
		require.Equal(t, 403, status)
	})
}
