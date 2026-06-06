package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type oauthClientRepoStub struct {
	client *OAuthClient
}

func (s *oauthClientRepoStub) Create(context.Context, *OAuthClient) error { return nil }
func (s *oauthClientRepoStub) GetByClientID(_ context.Context, clientID string) (*OAuthClient, error) {
	if s.client == nil || s.client.ClientID != clientID {
		return nil, ErrOAuthInvalidClient
	}
	return s.client, nil
}

type oauthCodeRepoStub struct {
	code   *OAuthAuthorizationCode
	stored *OAuthAuthorizationCode
}

func (s *oauthCodeRepoStub) Create(_ context.Context, code *OAuthAuthorizationCode) error {
	copy := *code
	copy.ID = 1
	s.stored = &copy
	s.code = &copy
	return nil
}
func (s *oauthCodeRepoStub) GetByCodeHash(_ context.Context, codeHash string) (*OAuthAuthorizationCode, error) {
	if s.code == nil || s.code.CodeHash != codeHash {
		return nil, ErrOAuthInvalidCode
	}
	return s.code, nil
}
func (s *oauthCodeRepoStub) MarkUsed(_ context.Context, id int64, usedAt time.Time) error {
	if s.code == nil || s.code.ID != id || s.code.UsedAt != nil {
		return ErrOAuthCodeUsed
	}
	s.code.UsedAt = &usedAt
	return nil
}

type oauthUserRepoStub struct {
	user *User
}

func (s *oauthUserRepoStub) Create(context.Context, *User) error { return nil }
func (s *oauthUserRepoStub) GetByID(context.Context, int64) (*User, error) {
	return s.user, nil
}
func (s *oauthUserRepoStub) GetByEmail(context.Context, string) (*User, error) { return s.user, nil }
func (s *oauthUserRepoStub) GetFirstAdmin(context.Context) (*User, error)      { return s.user, nil }
func (s *oauthUserRepoStub) Update(context.Context, *User) error               { return nil }
func (s *oauthUserRepoStub) Delete(context.Context, int64) error               { return nil }
func (s *oauthUserRepoStub) List(context.Context, pagination.PaginationParams) ([]User, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *oauthUserRepoStub) ListWithFilters(context.Context, pagination.PaginationParams, UserListFilters) ([]User, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *oauthUserRepoStub) UpdateBalance(context.Context, int64, float64) error { return nil }
func (s *oauthUserRepoStub) DeductBalance(context.Context, int64, float64) error { return nil }
func (s *oauthUserRepoStub) UpdateConcurrency(context.Context, int64, int) error { return nil }
func (s *oauthUserRepoStub) ExistsByEmail(context.Context, string) (bool, error) { return false, nil }
func (s *oauthUserRepoStub) RemoveGroupFromAllowedGroups(context.Context, int64) (int64, error) {
	return 0, nil
}
func (s *oauthUserRepoStub) AddGroupToAllowedGroups(context.Context, int64, int64) error {
	return nil
}
func (s *oauthUserRepoStub) RemoveGroupFromUserAllowedGroups(context.Context, int64, int64) error {
	return nil
}
func (s *oauthUserRepoStub) ListUsersByGroupAllowed(context.Context, int64) ([]User, error) {
	return nil, nil
}
func (s *oauthUserRepoStub) UpdateTotpSecret(context.Context, int64, *string) error { return nil }
func (s *oauthUserRepoStub) EnableTotp(context.Context, int64) error                { return nil }
func (s *oauthUserRepoStub) DisableTotp(context.Context, int64) error               { return nil }
func (s *oauthUserRepoStub) GetByReferralCode(context.Context, string) (*User, error) {
	return nil, ErrUserNotFound
}
func (s *oauthUserRepoStub) SetReferralCode(context.Context, int64, string) error { return nil }
func (s *oauthUserRepoStub) SetReferredBy(context.Context, int64, int64) error    { return nil }
func (s *oauthUserRepoStub) GetByPhoneNumber(context.Context, string) (*User, error) {
	return nil, ErrUserNotFound
}
func (s *oauthUserRepoStub) ExistsByPhoneNumber(context.Context, string) (bool, error) {
	return false, nil
}
func (s *oauthUserRepoStub) BindPhoneAndGrantBonus(context.Context, int64, string, float64) (*User, error) {
	return s.user, nil
}

func newOAuthAuthorizationTestService(t *testing.T) (*OAuthAuthorizationService, *oauthCodeRepoStub) {
	t.Helper()
	secretHash, err := HashOAuthClientSecret("test-secret")
	require.NoError(t, err)
	clientRepo := &oauthClientRepoStub{client: &OAuthClient{
		ClientID:         "external-test-client",
		ClientSecretHash: secretHash,
		Name:             "External Test Client",
		RedirectURIs:     []string{"http://localhost:8089/callback"},
		Scopes:           []string{"api.read", "profile"},
		Status:           StatusActive,
	}}
	codeRepo := &oauthCodeRepoStub{}
	userRepo := &oauthUserRepoStub{user: &User{ID: 42, Email: "u@example.com", Status: StatusActive}}
	svc := NewOAuthAuthorizationService(clientRepo, codeRepo, userRepo, &config.Config{})
	svc.cfg.JWT.Secret = "test-jwt-secret"
	return svc, codeRepo
}

func TestOAuthAuthorizationPreviewRejectsInvalidRedirectURI(t *testing.T) {
	svc, _ := newOAuthAuthorizationTestService(t)
	_, err := svc.PreviewAuthorization(context.Background(), OAuthAuthorizeInput{
		ClientID:     "external-test-client",
		RedirectURI:  "http://evil.local/callback",
		ResponseType: "code",
		Scope:        "profile",
	})
	require.ErrorIs(t, err, ErrOAuthInvalidRedirectURI)
}

func TestOAuthAuthorizationApprovePropagatesState(t *testing.T) {
	svc, codeRepo := newOAuthAuthorizationTestService(t)
	out, err := svc.ApproveAuthorization(context.Background(), 42, OAuthAuthorizeInput{
		ClientID:     "external-test-client",
		RedirectURI:  "http://localhost:8089/callback",
		ResponseType: "code",
		Scope:        "profile api.read",
		State:        "abc123",
	})
	require.NoError(t, err)
	require.Contains(t, out.RedirectURL, "state=abc123")
	require.Contains(t, out.RedirectURL, "code=")
	require.NotNil(t, codeRepo.stored)
	require.Equal(t, []string{"api.read", "profile"}, codeRepo.stored.Scopes)
}

func TestOAuthAuthorizationCodeExchangeIsSingleUse(t *testing.T) {
	svc, codeRepo := newOAuthAuthorizationTestService(t)
	rawCode := "raw-code"
	codeRepo.code = &OAuthAuthorizationCode{
		ID:          1,
		CodeHash:    HashOAuthSecret(rawCode),
		ClientID:    "external-test-client",
		UserID:      42,
		RedirectURI: "http://localhost:8089/callback",
		Scopes:      []string{"profile"},
		ExpiresAt:   time.Now().Add(time.Minute),
	}
	input := OAuthTokenInput{
		GrantType:    "authorization_code",
		Code:         rawCode,
		RedirectURI:  "http://localhost:8089/callback",
		ClientID:     "external-test-client",
		ClientSecret: "test-secret",
	}
	out, err := svc.ExchangeAuthorizationCode(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, OAuthTokenTypeBearer, out.TokenType)
	require.True(t, strings.Count(out.AccessToken, ".") == 2)

	_, err = svc.ExchangeAuthorizationCode(context.Background(), input)
	require.ErrorIs(t, err, ErrOAuthCodeUsed)
}

func TestOAuthAuthorizationCodeExchangeRejectsExpiredCode(t *testing.T) {
	svc, codeRepo := newOAuthAuthorizationTestService(t)
	rawCode := "raw-code"
	codeRepo.code = &OAuthAuthorizationCode{
		ID:          1,
		CodeHash:    HashOAuthSecret(rawCode),
		ClientID:    "external-test-client",
		UserID:      42,
		RedirectURI: "http://localhost:8089/callback",
		Scopes:      []string{"profile"},
		ExpiresAt:   time.Now().Add(-time.Minute),
	}
	_, err := svc.ExchangeAuthorizationCode(context.Background(), OAuthTokenInput{
		GrantType:    "authorization_code",
		Code:         rawCode,
		RedirectURI:  "http://localhost:8089/callback",
		ClientID:     "external-test-client",
		ClientSecret: "test-secret",
	})
	require.ErrorIs(t, err, ErrOAuthCodeExpired)
}

func TestOAuthUserInfoUsesOAuthAccessToken(t *testing.T) {
	svc, codeRepo := newOAuthAuthorizationTestService(t)
	rawCode := "raw-code"
	codeRepo.code = &OAuthAuthorizationCode{
		ID:          1,
		CodeHash:    HashOAuthSecret(rawCode),
		ClientID:    "external-test-client",
		UserID:      42,
		RedirectURI: "http://localhost:8089/callback",
		Scopes:      []string{"profile"},
		ExpiresAt:   time.Now().Add(time.Minute),
	}
	token, err := svc.ExchangeAuthorizationCode(context.Background(), OAuthTokenInput{
		GrantType:    "authorization_code",
		Code:         rawCode,
		RedirectURI:  "http://localhost:8089/callback",
		ClientID:     "external-test-client",
		ClientSecret: "test-secret",
	})
	require.NoError(t, err)

	info, err := svc.GetUserInfo(context.Background(), token.AccessToken)
	require.NoError(t, err)
	require.Equal(t, "42", info.Subject)
	require.Equal(t, int64(42), info.UserID)
	require.Equal(t, "u@example.com", info.Email)
	require.Equal(t, "external-test-client", info.ClientID)
}

func TestOAuthAuthorizationCodeExchangeWithPKCE(t *testing.T) {
	svc, codeRepo := newOAuthAuthorizationTestService(t)
	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~123"
	challenge := ""
	{
		sum := sha256.Sum256([]byte(verifier))
		challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	}
	out, err := svc.ApproveAuthorization(context.Background(), 42, OAuthAuthorizeInput{
		ClientID:            "external-test-client",
		RedirectURI:         "http://localhost:8089/callback",
		ResponseType:        "code",
		Scope:               "profile",
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
	})
	require.NoError(t, err)
	code := extractOAuthCodeForTest(t, out.RedirectURL)

	_, err = svc.ExchangeAuthorizationCode(context.Background(), OAuthTokenInput{
		GrantType:    "authorization_code",
		Code:         code,
		RedirectURI:  "http://localhost:8089/callback",
		ClientID:     "external-test-client",
		ClientSecret: "test-secret",
		CodeVerifier: "wrong-verifier-wrong-verifier-wrong-verifier-wrong",
	})
	require.ErrorIs(t, err, ErrOAuthInvalidPKCE)
	require.Nil(t, codeRepo.code.UsedAt)

	token, err := svc.ExchangeAuthorizationCode(context.Background(), OAuthTokenInput{
		GrantType:    "authorization_code",
		Code:         code,
		RedirectURI:  "http://localhost:8089/callback",
		ClientID:     "external-test-client",
		ClientSecret: "test-secret",
		CodeVerifier: verifier,
	})
	require.NoError(t, err)
	require.NotEmpty(t, token.AccessToken)
}

func extractOAuthCodeForTest(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	require.NoError(t, err)
	code := u.Query().Get("code")
	require.NotEmpty(t, code)
	return code
}
