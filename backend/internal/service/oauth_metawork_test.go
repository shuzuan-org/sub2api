//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// newMetaworkOAuthTestService 构造一个只注册了 metawork-app 的服务实例。
// 客户端配置逐字段对齐 migration 105 / 生产库 oauth_clients 那行：
// public、无 secret、loopback、scopes = metawork:use + profile。
func newMetaworkOAuthTestService(t *testing.T) *OAuthAuthorizationService {
	t.Helper()
	svc, _ := newOAuthAuthorizationTestService(t)
	svc.clientRepo = &oauthClientRepoStub{client: metaworkOAuthClient()}
	return svc
}

func metaworkOAuthClient() *OAuthClient {
	return &OAuthClient{
		ClientID:              MetaworkOAuthClientID,
		ClientType:            OAuthClientTypePublic,
		Name:                  "Metawork App",
		RedirectURIs:          nil,
		AllowLoopbackRedirect: true,
		Scopes:                []string{MetaworkOAuthScope, "profile"},
		Status:                StatusActive,
	}
}

// 网关 scope 门禁是「哪些 app 能打 /v1/*」的唯一开关，
// 改成集合后必须保证老的 metacode 不回归、未登记的 scope 依然进不来。
func TestIsGatewayOAuthScope(t *testing.T) {
	tests := []struct {
		name   string
		scopes []string
		want   bool
	}{
		{"metacode 不回归", []string{MetacodeOAuthScope}, true},
		{"metawork 放行", []string{MetaworkOAuthScope}, true},
		{"多 scope 命中其一即可", []string{"profile", MetaworkOAuthScope}, true},
		{"未登记的 scope 拒绝", []string{"api.read", "profile"}, false},
		{"空列表拒绝", nil, false},
		{"scope 前缀相同也不放行", []string{"metawork:use:extra"}, false},
		{"大小写不等价", []string{"MetaWork:Use"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, IsGatewayOAuthScope(tt.scopes))
		})
	}
}

// metawork-app 是桌面应用，走 loopback 回调。isRedirectURIAllowed 把路径写死成
// /auth/callback，这里锁住该约束，避免客户端按别的路径实现后线上才发现。
func TestMetaworkOAuthLoopbackRedirectContract(t *testing.T) {
	svc := newMetaworkOAuthTestService(t)
	_, challenge := testPKCEPair()

	tests := []struct {
		name        string
		redirectURI string
		wantErr     bool
	}{
		{"127.0.0.1 动态端口", "http://127.0.0.1:39000/auth/callback", false},
		{"localhost 动态端口", "http://localhost:51234/auth/callback", false},
		{"缺端口", "http://127.0.0.1/auth/callback", true},
		{"路径不是 /auth/callback", "http://127.0.0.1:39000/callback", true},
		{"带 query", "http://127.0.0.1:39000/auth/callback?x=1", true},
		{"https loopback", "https://127.0.0.1:39000/auth/callback", true},
		{"非 loopback 主机", "http://evil.local:39000/auth/callback", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.ApproveAuthorization(context.Background(), 42, OAuthAuthorizeInput{
				ClientID:            MetaworkOAuthClientID,
				RedirectURI:         tt.redirectURI,
				ResponseType:        "code",
				Scope:               MetaworkOAuthScope,
				CodeChallenge:       challenge,
				CodeChallengeMethod: "S256",
			})
			if tt.wantErr {
				require.ErrorIs(t, err, ErrOAuthInvalidRedirectURI)
				return
			}
			require.NoError(t, err)
		})
	}
}

// 两个 app 的 scope 必须互不通用：metawork-app 不能申请 metacode:use，
// 否则一个 app 的客户端就能冒充另一个走网关。
func TestMetaworkOAuthCannotRequestMetacodeScope(t *testing.T) {
	svc := newMetaworkOAuthTestService(t)
	_, challenge := testPKCEPair()

	_, err := svc.ApproveAuthorization(context.Background(), 42, OAuthAuthorizeInput{
		ClientID:            MetaworkOAuthClientID,
		RedirectURI:         "http://127.0.0.1:39000/auth/callback",
		ResponseType:        "code",
		Scope:               MetacodeOAuthScope,
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
	})

	require.ErrorIs(t, err, ErrOAuthInvalidScope)
}

// 不传 scope 时回落到客户端注册的**全部** scope（validateOAuthScopes 的行为，
// migration 105 的注释也据此写）。metawork-app 那行含 profile，所以默认会拿到两个。
func TestMetaworkOAuthDefaultsToAllClientScopes(t *testing.T) {
	svc, codeRepo := newOAuthAuthorizationTestService(t)
	svc.clientRepo = &oauthClientRepoStub{client: metaworkOAuthClient()}
	_, challenge := testPKCEPair()

	_, err := svc.ApproveAuthorization(context.Background(), 42, OAuthAuthorizeInput{
		ClientID:            MetaworkOAuthClientID,
		RedirectURI:         "http://127.0.0.1:39000/auth/callback",
		ResponseType:        "code",
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
	})

	require.NoError(t, err)
	require.NotNil(t, codeRepo.stored)
	require.Equal(t, []string{MetaworkOAuthScope, "profile"}, codeRepo.stored.Scopes)
	require.True(t, IsGatewayOAuthScope(codeRepo.stored.Scopes))
}

// profile 是登记在 metawork-app 那行上的合法 scope，客户端可以只申请它去调 userinfo。
// 但它不该成为打业务网关的通行证——只有 metawork:use 才是。
func TestMetaworkOAuthProfileScopeAloneCannotReachGateway(t *testing.T) {
	svc := newMetaworkOAuthTestService(t)
	_, challenge := testPKCEPair()

	_, err := svc.ApproveAuthorization(context.Background(), 42, OAuthAuthorizeInput{
		ClientID:            MetaworkOAuthClientID,
		RedirectURI:         "http://127.0.0.1:39000/auth/callback",
		ResponseType:        "code",
		Scope:               "profile",
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
	})
	require.NoError(t, err, "profile 是该客户端登记过的 scope，授权本身应当成功")

	require.False(t, IsGatewayOAuthScope([]string{"profile"}),
		"只带 profile 的 token 不得通过 /v1/* 的 scope 门禁")
}

// 端到端锁住「签发的 access token 能过网关门禁」：走完 authorize → token 交换，
// 再解出 claims 校验 scope。这是 /v1/profiles 401 那条 bug 的直接回归用例。
func TestMetaworkOAuthIssuedTokenPassesGatewayScopeGate(t *testing.T) {
	svc := newMetaworkOAuthTestService(t)
	token, err := svc.generateOAuthAccessToken(42, MetaworkOAuthClientID, []string{MetaworkOAuthScope})
	require.NoError(t, err)

	claims, err := svc.ValidateOAuthAccessTokenContext(context.Background(), token)
	require.NoError(t, err)
	require.Equal(t, MetaworkOAuthClientID, claims.ClientID)
	require.Equal(t, int64(42), claims.UserID)
	require.True(t, IsGatewayOAuthScope(claims.Scope),
		"metawork-app 签发的 token 必须能过 /v1/* 的 scope 门禁，否则 /v1/profiles 会返 401 INVALID_API_KEY")
}
