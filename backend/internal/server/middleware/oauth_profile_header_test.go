//go:build unit

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// profile 头改成「新名优先、旧名兜底」后，老的 Metacode CLI 只发 X-Metacode-Profile-ID，
// 必须继续可用；否则线上老客户端会全体 400 PROFILE_REQUIRED。
func TestOAuthProfileIDHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name    string
		headers map[string]string
		want    string
	}{
		{
			name:    "新客户端只发中性头",
			headers: map[string]string{"X-Profile-ID": "123"},
			want:    "123",
		},
		{
			name:    "老 CLI 只发历史头仍可用",
			headers: map[string]string{"X-Metacode-Profile-ID": "456"},
			want:    "456",
		},
		{
			name:    "两个都在时新头优先",
			headers: map[string]string{"X-Profile-ID": "123", "X-Metacode-Profile-ID": "456"},
			want:    "123",
		},
		{
			name:    "新头是空白时回落到历史头",
			headers: map[string]string{"X-Profile-ID": "   ", "X-Metacode-Profile-ID": "456"},
			want:    "456",
		},
		{
			name:    "两侧都去空白",
			headers: map[string]string{"X-Metacode-Profile-ID": "  789  "},
			want:    "789",
		},
		{
			name:    "都没有则为空",
			headers: nil,
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/messages", nil)
			for k, v := range tt.headers {
				c.Request.Header.Set(k, v)
			}
			require.Equal(t, tt.want, oauthProfileIDHeader(c))
		})
	}
}
