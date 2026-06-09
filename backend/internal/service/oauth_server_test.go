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
func (s *oauthCodeRepoStub) Consume(_ context.Context, codeHash, clientID, redirectURI string, now time.Time) (*OAuthAuthorizationCode, error) {
	if s.code == nil || s.code.CodeHash != codeHash || s.code.ClientID != clientID || s.code.RedirectURI != redirectURI {
		return nil, ErrOAuthInvalidCode
	}
	if s.code.UsedAt != nil {
		return nil, ErrOAuthCodeUsed
	}
	if !s.code.ExpiresAt.After(now) {
		return nil, ErrOAuthInvalidCode
	}
	s.code.UsedAt = &now
	return s.code, nil
}

type oauthRefreshRepoStub struct {
	created []*OAuthRefreshToken
}

func (s *oauthRefreshRepoStub) Create(_ context.Context, token *OAuthRefreshToken) error {
	copy := *token
	copy.ID = int64(len(s.created) + 1)
	s.created = append(s.created, &copy)
	return nil
}

func (s *oauthRefreshRepoStub) Rotate(_ context.Context, tokenHash, clientID string, next *OAuthRefreshToken, now time.Time) (*OAuthRefreshToken, error) {
	for _, token := range s.created {
		if token.TokenHash != tokenHash || token.ClientID != clientID {
			continue
		}
		if token.Status != OAuthRefreshTokenStatusActive || !token.ExpiresAt.After(now) {
			return nil, ErrOAuthInvalidToken
		}
		token.Status = OAuthRefreshTokenStatusUsed
		token.UsedAt = &now
		next.FamilyID = token.FamilyID
		next.UserID = token.UserID
		next.ClientID = token.ClientID
		next.Scopes = append([]string(nil), token.Scopes...)
		next.ParentTokenHash = &token.TokenHash
		s.created = append(s.created, next)
		return token, nil
	}
	return nil, ErrOAuthInvalidToken
}

func (s *oauthRefreshRepoStub) RevokeByHash(context.Context, string, string, time.Time) error {
	return nil
}

type oauthDeviceRepoStub struct {
	sessions []*OAuthDeviceAuthorization
}

func (s *oauthDeviceRepoStub) Create(_ context.Context, session *OAuthDeviceAuthorization) error {
	copy := *session
	copy.ID = int64(len(s.sessions) + 1)
	s.sessions = append(s.sessions, &copy)
	return nil
}

func (s *oauthDeviceRepoStub) GetByUserCodeHash(_ context.Context, userCodeHash string) (*OAuthDeviceAuthorization, error) {
	for _, session := range s.sessions {
		if session.UserCodeHash == userCodeHash {
			return session, nil
		}
	}
	return nil, ErrOAuthInvalidUserCode
}

func (s *oauthDeviceRepoStub) Approve(_ context.Context, userCodeHash string, userID int64, now time.Time) (*OAuthDeviceAuthorization, error) {
	session, err := s.GetByUserCodeHash(context.Background(), userCodeHash)
	if err != nil || session.Status != OAuthDeviceStatusPending || !session.ExpiresAt.After(now) {
		return nil, ErrOAuthInvalidUserCode
	}
	session.Status = OAuthDeviceStatusApproved
	session.UserID = &userID
	session.ApprovedAt = &now
	return session, nil
}

func (s *oauthDeviceRepoStub) Deny(_ context.Context, userCodeHash string, userID int64, now time.Time) (*OAuthDeviceAuthorization, error) {
	session, err := s.GetByUserCodeHash(context.Background(), userCodeHash)
	if err != nil || session.Status != OAuthDeviceStatusPending || !session.ExpiresAt.After(now) {
		return nil, ErrOAuthInvalidUserCode
	}
	session.Status = OAuthDeviceStatusDenied
	session.UserID = &userID
	session.DeniedAt = &now
	return session, nil
}

func (s *oauthDeviceRepoStub) GetByDeviceCodeHash(_ context.Context, deviceCodeHash, clientID string) (*OAuthDeviceAuthorization, error) {
	for _, session := range s.sessions {
		if session.DeviceCodeHash == deviceCodeHash && session.ClientID == clientID {
			return session, nil
		}
	}
	return nil, ErrOAuthInvalidToken
}

func (s *oauthDeviceRepoStub) MarkPolled(_ context.Context, id int64, now time.Time) error {
	for _, session := range s.sessions {
		if session.ID == id {
			session.LastPollAt = &now
			return nil
		}
	}
	return nil
}

func (s *oauthDeviceRepoStub) Cancel(_ context.Context, deviceCodeHash, clientID string, now time.Time) error {
	session, err := s.GetByDeviceCodeHash(context.Background(), deviceCodeHash, clientID)
	if err == nil && session.Status == OAuthDeviceStatusPending && session.ExpiresAt.After(now) {
		session.Status = OAuthDeviceStatusCancelled
		session.CancelledAt = &now
	}
	return nil
}

func (s *oauthDeviceRepoStub) ConsumeApproved(_ context.Context, deviceCodeHash, clientID string, now time.Time) (*OAuthDeviceAuthorization, error) {
	session, err := s.GetByDeviceCodeHash(context.Background(), deviceCodeHash, clientID)
	if err != nil || session.Status != OAuthDeviceStatusApproved || !session.ExpiresAt.After(now) {
		return nil, ErrOAuthInvalidToken
	}
	copy := *session
	session.Status = OAuthDeviceStatusUsed
	session.UsedAt = &now
	copy.Status = OAuthDeviceStatusUsed
	copy.UsedAt = &now
	return &copy, nil
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
func (s *oauthUserRepoStub) GetByPhone(context.Context, string) (*User, error) {
	return nil, ErrUserNotFound
}
func (s *oauthUserRepoStub) ExistsByPhone(context.Context, string) (bool, error) { return false, nil }
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
		ClientType:       OAuthClientTypeConfidential,
		Name:             "External Test Client",
		RedirectURIs:     []string{"http://localhost:8089/callback"},
		Scopes:           []string{"api.read", "profile"},
		Status:           StatusActive,
	}}
	codeRepo := &oauthCodeRepoStub{}
	refreshRepo := &oauthRefreshRepoStub{}
	userRepo := &oauthUserRepoStub{user: &User{ID: 42, Email: "u@example.com", Status: StatusActive}}
	svc := NewOAuthAuthorizationService(clientRepo, codeRepo, refreshRepo, userRepo, nil, &config.Config{})
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
	_, challenge := testPKCEPair()
	out, err := svc.ApproveAuthorization(context.Background(), 42, OAuthAuthorizeInput{
		ClientID:            "external-test-client",
		RedirectURI:         "http://localhost:8089/callback",
		ResponseType:        "code",
		Scope:               "profile api.read",
		State:               "abc123",
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
	})
	require.NoError(t, err)
	require.Contains(t, out.RedirectURL, "state=abc123")
	require.Contains(t, out.RedirectURL, "code=")
	require.NotNil(t, codeRepo.stored)
	require.Equal(t, []string{"api.read", "profile"}, codeRepo.stored.Scopes)
}

func TestOAuthAuthorizationApproveAllowsMetacodeScopeWithoutAPIKey(t *testing.T) {
	svc, _ := newOAuthAuthorizationTestService(t)
	svc.clientRepo = &oauthClientRepoStub{client: &OAuthClient{
		ClientID:              MetacodeOAuthClientID,
		ClientType:            OAuthClientTypePublic,
		Name:                  "Metacode CLI",
		RedirectURIs:          []string{"http://127.0.0.1/callback"},
		AllowLoopbackRedirect: true,
		Scopes:                []string{MetacodeOAuthScope},
		Status:                StatusActive,
	}}
	_, challenge := testPKCEPair()

	out, err := svc.ApproveAuthorization(context.Background(), 42, OAuthAuthorizeInput{
		ClientID:            MetacodeOAuthClientID,
		RedirectURI:         "http://127.0.0.1:39000/auth/callback",
		ResponseType:        "code",
		Scope:               MetacodeOAuthScope,
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
	})

	require.NoError(t, err)
	require.Contains(t, out.RedirectURL, "code=")
}

func TestOAuthAuthorizationCodeExchangeIsSingleUse(t *testing.T) {
	svc, codeRepo := newOAuthAuthorizationTestService(t)
	rawCode := "raw-code"
	verifier, challenge := testPKCEPair()
	_, codeHash, err := svc.hashOAuthSecret(rawCode)
	require.NoError(t, err)
	codeRepo.code = &OAuthAuthorizationCode{
		ID:                  1,
		CodeHash:            codeHash,
		ClientID:            "external-test-client",
		UserID:              42,
		RedirectURI:         "http://localhost:8089/callback",
		Scopes:              []string{"profile"},
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		ExpiresAt:           time.Now().Add(time.Minute),
	}
	input := OAuthTokenInput{
		GrantType:    "authorization_code",
		Code:         rawCode,
		RedirectURI:  "http://localhost:8089/callback",
		ClientID:     "external-test-client",
		ClientSecret: "test-secret",
		CodeVerifier: verifier,
	}
	out, err := svc.ExchangeAuthorizationCode(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, OAuthTokenTypeBearer, out.TokenType)
	require.True(t, strings.Count(out.AccessToken, ".") == 2)

	_, err = svc.ExchangeAuthorizationCode(context.Background(), input)
	require.ErrorIs(t, err, ErrOAuthInvalidCode)
}

func TestOAuthAuthorizationCodeExchangeRejectsExpiredCode(t *testing.T) {
	svc, codeRepo := newOAuthAuthorizationTestService(t)
	rawCode := "raw-code"
	verifier, challenge := testPKCEPair()
	_, codeHash, err := svc.hashOAuthSecret(rawCode)
	require.NoError(t, err)
	codeRepo.code = &OAuthAuthorizationCode{
		ID:                  1,
		CodeHash:            codeHash,
		ClientID:            "external-test-client",
		UserID:              42,
		RedirectURI:         "http://localhost:8089/callback",
		Scopes:              []string{"profile"},
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		ExpiresAt:           time.Now().Add(-time.Minute),
	}
	_, err = svc.ExchangeAuthorizationCode(context.Background(), OAuthTokenInput{
		GrantType:    "authorization_code",
		Code:         rawCode,
		RedirectURI:  "http://localhost:8089/callback",
		ClientID:     "external-test-client",
		ClientSecret: "test-secret",
		CodeVerifier: verifier,
	})
	require.ErrorIs(t, err, ErrOAuthInvalidCode)
}

func TestOAuthUserInfoUsesOAuthAccessToken(t *testing.T) {
	svc, codeRepo := newOAuthAuthorizationTestService(t)
	rawCode := "raw-code"
	verifier, challenge := testPKCEPair()
	_, codeHash, err := svc.hashOAuthSecret(rawCode)
	require.NoError(t, err)
	codeRepo.code = &OAuthAuthorizationCode{
		ID:                  1,
		CodeHash:            codeHash,
		ClientID:            "external-test-client",
		UserID:              42,
		RedirectURI:         "http://localhost:8089/callback",
		Scopes:              []string{"profile"},
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		ExpiresAt:           time.Now().Add(time.Minute),
	}
	token, err := svc.ExchangeAuthorizationCode(context.Background(), OAuthTokenInput{
		GrantType:    "authorization_code",
		Code:         rawCode,
		RedirectURI:  "http://localhost:8089/callback",
		ClientID:     "external-test-client",
		ClientSecret: "test-secret",
		CodeVerifier: verifier,
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
	verifier, challenge := testPKCEPair()
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
	require.NotNil(t, codeRepo.code.UsedAt)

	token, err := svc.ExchangeAuthorizationCode(context.Background(), OAuthTokenInput{
		GrantType:    "authorization_code",
		Code:         code,
		RedirectURI:  "http://localhost:8089/callback",
		ClientID:     "external-test-client",
		ClientSecret: "test-secret",
		CodeVerifier: verifier,
	})
	require.ErrorIs(t, err, ErrOAuthInvalidCode)
	require.Nil(t, token)
}

func TestOAuthAuthorizationCodeExchangeWithPKCESuccess(t *testing.T) {
	svc, _ := newOAuthAuthorizationTestService(t)
	verifier, challenge := testPKCEPair()
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
	require.NotEmpty(t, token.RefreshToken)
}

func TestOAuthRefreshTokenRotatesToken(t *testing.T) {
	svc, _ := newOAuthAuthorizationTestService(t)
	verifier, challenge := testPKCEPair()
	out, err := svc.ApproveAuthorization(context.Background(), 42, OAuthAuthorizeInput{
		ClientID:            "external-test-client",
		RedirectURI:         "http://localhost:8089/callback",
		ResponseType:        "code",
		Scope:               "profile",
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
	})
	require.NoError(t, err)

	token, err := svc.ExchangeToken(context.Background(), OAuthTokenInput{
		GrantType:    "authorization_code",
		Code:         extractOAuthCodeForTest(t, out.RedirectURL),
		RedirectURI:  "http://localhost:8089/callback",
		ClientID:     "external-test-client",
		ClientSecret: "test-secret",
		CodeVerifier: verifier,
	})
	require.NoError(t, err)

	refreshed, err := svc.ExchangeToken(context.Background(), OAuthTokenInput{
		GrantType:    "refresh_token",
		ClientID:     "external-test-client",
		ClientSecret: "test-secret",
		RefreshToken: token.RefreshToken,
	})

	require.NoError(t, err)
	require.NotEmpty(t, refreshed.AccessToken)
	require.NotEmpty(t, refreshed.RefreshToken)
	require.NotEqual(t, token.RefreshToken, refreshed.RefreshToken)
}

func TestOAuthDeviceAuthorizationFlow(t *testing.T) {
	secretHash, err := HashOAuthClientSecret("test-secret")
	require.NoError(t, err)
	clientRepo := &oauthClientRepoStub{client: &OAuthClient{
		ClientID:         "device-client",
		ClientSecretHash: secretHash,
		ClientType:       OAuthClientTypeConfidential,
		Name:             "Device Client",
		Scopes:           []string{"profile"},
		Status:           StatusActive,
	}}
	deviceRepo := &oauthDeviceRepoStub{}
	refreshRepo := &oauthRefreshRepoStub{}
	userRepo := &oauthUserRepoStub{user: &User{ID: 42, Email: "u@example.com", Status: StatusActive}}
	svc := NewOAuthAuthorizationService(clientRepo, &oauthCodeRepoStub{}, refreshRepo, userRepo, nil, &config.Config{}, deviceRepo)
	svc.cfg.JWT.Secret = "test-jwt-secret"

	device, err := svc.CreateDeviceAuthorization(context.Background(), OAuthDeviceAuthorizationInput{
		ClientID:        "device-client",
		ClientSecret:    "test-secret",
		Scope:           "profile",
		DeviceName:      "host-1",
		VerificationURI: "https://example.com/device",
	})
	require.NoError(t, err)
	require.NotEmpty(t, device.DeviceCode)
	require.NotEmpty(t, device.UserCode)
	require.Equal(t, int(OAuthDeviceAuthorizationPoll.Seconds()), device.Interval)
	require.Len(t, deviceRepo.sessions, 1)
	require.Equal(t, OAuthDeviceStatusPending, deviceRepo.sessions[0].Status)

	preview, err := svc.PreviewDeviceAuthorization(context.Background(), device.UserCode)
	require.NoError(t, err)
	require.Equal(t, "Device Client", preview.ClientName)
	require.Equal(t, []string{"profile"}, preview.Scopes)
	require.Equal(t, "host-1", preview.DeviceName)

	_, err = svc.ExchangeDeviceCode(context.Background(), OAuthTokenInput{
		GrantType:    OAuthDeviceCodeGrantType,
		ClientID:     "device-client",
		ClientSecret: "test-secret",
		DeviceCode:   device.DeviceCode,
	})
	require.ErrorIs(t, err, ErrOAuthAuthorizationPending)
	deviceRepo.sessions[0].LastPollAt = nil

	require.NoError(t, svc.ApproveDeviceAuthorization(context.Background(), 42, device.UserCode))
	token, err := svc.ExchangeDeviceCode(context.Background(), OAuthTokenInput{
		GrantType:    OAuthDeviceCodeGrantType,
		ClientID:     "device-client",
		ClientSecret: "test-secret",
		DeviceCode:   device.DeviceCode,
	})
	require.NoError(t, err)
	require.NotEmpty(t, token.AccessToken)
	require.NotEmpty(t, token.RefreshToken)
	require.Equal(t, OAuthDeviceStatusUsed, deviceRepo.sessions[0].Status)

	_, err = svc.ExchangeDeviceCode(context.Background(), OAuthTokenInput{
		GrantType:    OAuthDeviceCodeGrantType,
		ClientID:     "device-client",
		ClientSecret: "test-secret",
		DeviceCode:   device.DeviceCode,
	})
	require.ErrorIs(t, err, ErrOAuthInvalidToken)
}

func extractOAuthCodeForTest(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	require.NoError(t, err)
	code := u.Query().Get("code")
	require.NotEmpty(t, code)
	return code
}

func testPKCEPair() (string, string) {
	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~123"
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}
