package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const (
	OAuthAuthorizationCodeTTL      = 5 * time.Minute
	OAuthAccessTokenTTL            = time.Hour
	OAuthRefreshTokenTTL           = 90 * 24 * time.Hour
	OAuthTokenTypeBearer           = "Bearer"
	OAuthClientTypePublic          = "public"
	OAuthClientTypeConfidential    = "confidential"
	OAuthRefreshTokenStatusActive  = "active"
	OAuthRefreshTokenStatusUsed    = "used"
	OAuthRefreshTokenStatusRevoked = "revoked"
	MetacodeOAuthClientID          = "metacode-cli"
	MetacodeOAuthScope             = "metacode:use"
)

var (
	ErrOAuthInvalidRequest       = infraerrors.BadRequest("OAUTH_INVALID_REQUEST", "invalid oauth request")
	ErrOAuthUnsupportedGrantType = infraerrors.BadRequest("OAUTH_UNSUPPORTED_GRANT_TYPE", "unsupported grant type")
	ErrOAuthUnsupportedResponse  = infraerrors.BadRequest("OAUTH_UNSUPPORTED_RESPONSE_TYPE", "unsupported response type")
	ErrOAuthInvalidClient        = infraerrors.Unauthorized("OAUTH_INVALID_CLIENT", "invalid oauth client")
	ErrOAuthInvalidRedirectURI   = infraerrors.BadRequest("OAUTH_INVALID_REDIRECT_URI", "invalid redirect uri")
	ErrOAuthInvalidScope         = infraerrors.BadRequest("OAUTH_INVALID_SCOPE", "invalid scope")
	ErrOAuthInvalidCode          = infraerrors.BadRequest("OAUTH_INVALID_CODE", "invalid authorization code")
	ErrOAuthInvalidToken         = infraerrors.Unauthorized("OAUTH_INVALID_TOKEN", "invalid oauth access token")
	ErrOAuthInvalidPKCE          = infraerrors.BadRequest("OAUTH_INVALID_PKCE", "invalid PKCE verifier")
	ErrOAuthCodeExpired          = infraerrors.BadRequest("OAUTH_CODE_EXPIRED", "authorization code has expired")
	ErrOAuthCodeUsed             = infraerrors.BadRequest("OAUTH_CODE_USED", "authorization code has already been used")
)

type OAuthClient struct {
	ID                    int64
	ClientID              string
	ClientSecretHash      string
	ClientType            string
	Name                  string
	RedirectURIs          []string
	AllowLoopbackRedirect bool
	Scopes                []string
	Status                string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

func (c *OAuthClient) IsActive() bool {
	return c != nil && c.Status == StatusActive
}

type OAuthAuthorizationCode struct {
	ID                  int64
	CodeHash            string
	HMACKeyID           string
	ClientID            string
	UserID              int64
	APIKeyID            *int64
	RedirectURI         string
	Scopes              []string
	CodeChallenge       string
	CodeChallengeMethod string
	ExpiresAt           time.Time
	UsedAt              *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type OAuthClientRepository interface {
	Create(ctx context.Context, client *OAuthClient) error
	GetByClientID(ctx context.Context, clientID string) (*OAuthClient, error)
}

type OAuthAuthorizationCodeRepository interface {
	Create(ctx context.Context, code *OAuthAuthorizationCode) error
	Consume(ctx context.Context, codeHash, clientID, redirectURI string, now time.Time) (*OAuthAuthorizationCode, error)
}

type OAuthRefreshToken struct {
	ID              int64
	TokenHash       string
	HMACKeyID       string
	FamilyID        string
	ParentTokenHash *string
	UserID          int64
	APIKeyID        *int64
	ClientID        string
	Scopes          []string
	Status          string
	ExpiresAt       time.Time
	UsedAt          *time.Time
	RevokedAt       *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type OAuthRefreshTokenRepository interface {
	Create(ctx context.Context, token *OAuthRefreshToken) error
	Rotate(ctx context.Context, tokenHash, clientID string, next *OAuthRefreshToken, now time.Time) (*OAuthRefreshToken, error)
	RevokeByHash(ctx context.Context, tokenHash, clientID string, now time.Time) error
}

type OAuthAuthorizationService struct {
	clientRepo       OAuthClientRepository
	codeRepo         OAuthAuthorizationCodeRepository
	refreshTokenRepo OAuthRefreshTokenRepository
	userRepo         UserRepository
	apiKeyRepo       APIKeyRepository
	cfg              *config.Config
}

func NewOAuthAuthorizationService(
	clientRepo OAuthClientRepository,
	codeRepo OAuthAuthorizationCodeRepository,
	refreshTokenRepo OAuthRefreshTokenRepository,
	userRepo UserRepository,
	apiKeyRepo APIKeyRepository,
	cfg *config.Config,
) *OAuthAuthorizationService {
	return &OAuthAuthorizationService{clientRepo: clientRepo, codeRepo: codeRepo, refreshTokenRepo: refreshTokenRepo, userRepo: userRepo, apiKeyRepo: apiKeyRepo, cfg: cfg}
}

type OAuthAuthorizeInput struct {
	ClientID            string
	RedirectURI         string
	ResponseType        string
	Scope               string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
	APIKeyID            *int64
}

type OAuthAuthorizationPreview struct {
	ClientID    string   `json:"client_id"`
	ClientName  string   `json:"client_name"`
	RedirectURI string   `json:"redirect_uri"`
	Scopes      []string `json:"scopes"`
	State       string   `json:"state,omitempty"`
}

type OAuthAuthorizationRedirect struct {
	RedirectURL string `json:"redirect_url"`
}

type OAuthTokenInput struct {
	GrantType    string
	Code         string
	RedirectURI  string
	ClientID     string
	ClientSecret string
	CodeVerifier string
	RefreshToken string
}

type OAuthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
	AccountID    string `json:"account_id,omitempty"`
}

type OAuthUserInfoResponse struct {
	Subject  string   `json:"sub"`
	UserID   int64    `json:"user_id"`
	Email    string   `json:"email"`
	Username string   `json:"username"`
	ClientID string   `json:"client_id"`
	Scopes   []string `json:"scopes"`
}

type OAuthAccessTokenClaims struct {
	UserID   int64    `json:"user_id"`
	APIKeyID int64    `json:"api_key_id,omitempty"`
	ClientID string   `json:"client_id"`
	Scope    []string `json:"scope"`
	Purpose  string   `json:"purpose"`
	jwt.RegisteredClaims
}

func (s *OAuthAuthorizationService) PreviewAuthorization(ctx context.Context, input OAuthAuthorizeInput) (*OAuthAuthorizationPreview, error) {
	client, scopes, err := s.validateAuthorizeInput(ctx, input)
	if err != nil {
		return nil, err
	}
	return &OAuthAuthorizationPreview{
		ClientID:    client.ClientID,
		ClientName:  client.Name,
		RedirectURI: input.RedirectURI,
		Scopes:      scopes,
		State:       input.State,
	}, nil
}

func (s *OAuthAuthorizationService) ApproveAuthorization(ctx context.Context, userID int64, input OAuthAuthorizeInput) (*OAuthAuthorizationRedirect, error) {
	client, scopes, err := s.validateAuthorizeInput(ctx, input)
	if err != nil {
		return nil, err
	}
	if userID <= 0 {
		return nil, ErrOAuthInvalidRequest
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !user.IsActive() {
		return nil, ErrUserNotActive
	}
	if containsExact(scopes, MetacodeOAuthScope) && input.APIKeyID == nil {
		return nil, ErrOAuthInvalidRequest
	}
	if input.APIKeyID != nil {
		if err := s.validateAPIKeyBinding(ctx, userID, *input.APIKeyID); err != nil {
			return nil, err
		}
	}

	rawCode, err := randomTokenHex(32)
	if err != nil {
		return nil, fmt.Errorf("generate authorization code: %w", err)
	}
	keyID, codeHash, err := s.hashOAuthSecret(rawCode)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if err := s.codeRepo.Create(ctx, &OAuthAuthorizationCode{
		CodeHash:            codeHash,
		HMACKeyID:           keyID,
		ClientID:            client.ClientID,
		UserID:              userID,
		APIKeyID:            input.APIKeyID,
		RedirectURI:         input.RedirectURI,
		Scopes:              scopes,
		CodeChallenge:       strings.TrimSpace(input.CodeChallenge),
		CodeChallengeMethod: normalizedCodeChallengeMethod(input.CodeChallengeMethod),
		ExpiresAt:           now.Add(OAuthAuthorizationCodeTTL),
	}); err != nil {
		return nil, err
	}

	return &OAuthAuthorizationRedirect{RedirectURL: buildOAuthRedirect(input.RedirectURI, map[string]string{
		"code":  rawCode,
		"state": input.State,
	})}, nil
}

func (s *OAuthAuthorizationService) DenyAuthorization(ctx context.Context, input OAuthAuthorizeInput) (*OAuthAuthorizationRedirect, error) {
	_, _, err := s.validateAuthorizeInput(ctx, input)
	if err != nil {
		return nil, err
	}
	return &OAuthAuthorizationRedirect{RedirectURL: buildOAuthRedirect(input.RedirectURI, map[string]string{
		"error":             "access_denied",
		"error_description": "The resource owner denied the request",
		"state":             input.State,
	})}, nil
}

func (s *OAuthAuthorizationService) ExchangeToken(ctx context.Context, input OAuthTokenInput) (*OAuthTokenResponse, error) {
	switch strings.TrimSpace(input.GrantType) {
	case "authorization_code":
		return s.ExchangeAuthorizationCode(ctx, input)
	case "refresh_token":
		return s.RefreshAccessToken(ctx, input)
	default:
		return nil, ErrOAuthUnsupportedGrantType
	}
}

func (s *OAuthAuthorizationService) ExchangeAuthorizationCode(ctx context.Context, input OAuthTokenInput) (*OAuthTokenResponse, error) {
	if strings.TrimSpace(input.GrantType) != "authorization_code" {
		return nil, ErrOAuthUnsupportedGrantType
	}
	clientID := strings.TrimSpace(input.ClientID)
	clientSecret := input.ClientSecret
	if clientID == "" {
		return nil, ErrOAuthInvalidClient
	}
	client, err := s.clientRepo.GetByClientID(ctx, clientID)
	if err != nil {
		return nil, ErrOAuthInvalidClient
	}
	if err := validateOAuthClientTokenAuth(client, clientSecret); err != nil {
		return nil, ErrOAuthInvalidClient
	}

	code := strings.TrimSpace(input.Code)
	if code == "" || len(code) > maxTokenLength {
		return nil, ErrOAuthInvalidCode
	}
	_, codeHash, err := s.hashOAuthSecret(code)
	if err != nil {
		return nil, err
	}
	authCode, err := s.codeRepo.Consume(ctx, codeHash, client.ClientID, strings.TrimSpace(input.RedirectURI), time.Now())
	if err != nil {
		return nil, ErrOAuthInvalidCode
	}
	if err := validatePKCE(authCode.CodeChallenge, authCode.CodeChallengeMethod, input.CodeVerifier); err != nil {
		return nil, err
	}
	refreshToken, err := s.createRefreshToken(ctx, authCode.UserID, authCode.APIKeyID, client.ClientID, authCode.Scopes, "", "")
	if err != nil {
		return nil, err
	}

	accessToken, err := s.generateOAuthAccessToken(authCode.UserID, authCode.APIKeyID, client.ClientID, authCode.Scopes)
	if err != nil {
		return nil, err
	}
	return &OAuthTokenResponse{
		AccessToken:  accessToken,
		TokenType:    OAuthTokenTypeBearer,
		ExpiresIn:    int(OAuthAccessTokenTTL.Seconds()),
		RefreshToken: refreshToken,
		Scope:        strings.Join(authCode.Scopes, " "),
		AccountID:    fmt.Sprintf("%d", authCode.UserID),
	}, nil
}

func (s *OAuthAuthorizationService) RefreshAccessToken(ctx context.Context, input OAuthTokenInput) (*OAuthTokenResponse, error) {
	if strings.TrimSpace(input.GrantType) != "refresh_token" {
		return nil, ErrOAuthUnsupportedGrantType
	}
	clientID := strings.TrimSpace(input.ClientID)
	if clientID == "" {
		return nil, ErrOAuthInvalidClient
	}
	client, err := s.clientRepo.GetByClientID(ctx, clientID)
	if err != nil {
		return nil, ErrOAuthInvalidClient
	}
	if err := validateOAuthClientTokenAuth(client, input.ClientSecret); err != nil {
		return nil, ErrOAuthInvalidClient
	}
	rawRefreshToken := strings.TrimSpace(input.RefreshToken)
	if rawRefreshToken == "" || len(rawRefreshToken) > maxTokenLength {
		return nil, ErrOAuthInvalidToken
	}
	_, tokenHash, err := s.hashOAuthSecret(rawRefreshToken)
	if err != nil {
		return nil, err
	}
	nextRawToken, nextToken, err := s.newRefreshTokenRecord(0, nil, client.ClientID, nil, "", tokenHash)
	if err != nil {
		return nil, err
	}
	previous, err := s.refreshTokenRepo.Rotate(ctx, tokenHash, client.ClientID, nextToken, time.Now())
	if err != nil {
		return nil, ErrOAuthInvalidToken
	}
	accessToken, err := s.generateOAuthAccessToken(previous.UserID, previous.APIKeyID, previous.ClientID, previous.Scopes)
	if err != nil {
		return nil, err
	}
	return &OAuthTokenResponse{
		AccessToken:  accessToken,
		TokenType:    OAuthTokenTypeBearer,
		ExpiresIn:    int(OAuthAccessTokenTTL.Seconds()),
		RefreshToken: nextRawToken,
		Scope:        strings.Join(previous.Scopes, " "),
		AccountID:    fmt.Sprintf("%d", previous.UserID),
	}, nil
}

func (s *OAuthAuthorizationService) RevokeToken(ctx context.Context, clientID, clientSecret, rawToken, tokenTypeHint string) error {
	clientID = strings.TrimSpace(clientID)
	rawToken = strings.TrimSpace(rawToken)
	if clientID == "" || rawToken == "" || len(rawToken) > maxTokenLength {
		return nil
	}
	client, err := s.clientRepo.GetByClientID(ctx, clientID)
	if err != nil {
		return nil
	}
	if err := validateOAuthClientTokenAuth(client, clientSecret); err != nil {
		return nil
	}
	_, tokenHash, err := s.hashOAuthSecret(rawToken)
	if err != nil {
		return err
	}
	if strings.EqualFold(strings.TrimSpace(tokenTypeHint), "access_token") && strings.Count(rawToken, ".") == 2 {
		return nil
	}
	return s.refreshTokenRepo.RevokeByHash(ctx, tokenHash, client.ClientID, time.Now())
}

func (s *OAuthAuthorizationService) GetUserInfo(ctx context.Context, accessToken string) (*OAuthUserInfoResponse, error) {
	claims, err := s.ValidateOAuthAccessToken(accessToken)
	if err != nil {
		return nil, err
	}
	if !containsExact(claims.Scope, "profile") {
		return nil, ErrOAuthInvalidScope
	}
	user, err := s.userRepo.GetByID(ctx, claims.UserID)
	if err != nil {
		return nil, err
	}
	if !user.IsActive() {
		return nil, ErrUserNotActive
	}
	return &OAuthUserInfoResponse{
		Subject:  fmt.Sprintf("%d", user.ID),
		UserID:   user.ID,
		Email:    user.Email,
		Username: user.Username,
		ClientID: claims.ClientID,
		Scopes:   append([]string(nil), claims.Scope...),
	}, nil
}

func (s *OAuthAuthorizationService) ValidateOAuthAccessToken(tokenString string) (*OAuthAccessTokenClaims, error) {
	if strings.TrimSpace(tokenString) == "" || len(tokenString) > maxTokenLength {
		return nil, ErrOAuthInvalidToken
	}
	if s.cfg == nil || s.cfg.JWT.Secret == "" {
		return nil, ErrOAuthInvalidToken
	}
	parser := jwt.NewParser(jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Name}))
	token, err := parser.ParseWithClaims(tokenString, &OAuthAccessTokenClaims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.cfg.JWT.Secret), nil
	})
	if err != nil {
		return nil, ErrOAuthInvalidToken
	}
	claims, ok := token.Claims.(*OAuthAccessTokenClaims)
	if !ok || !token.Valid || claims.Purpose != "oauth_access_token" || claims.UserID <= 0 || claims.ClientID == "" {
		return nil, ErrOAuthInvalidToken
	}
	return claims, nil
}

func (s *OAuthAuthorizationService) validateAuthorizeInput(ctx context.Context, input OAuthAuthorizeInput) (*OAuthClient, []string, error) {
	if strings.TrimSpace(input.ResponseType) != "code" {
		return nil, nil, ErrOAuthUnsupportedResponse
	}
	clientID := strings.TrimSpace(input.ClientID)
	redirectURI := strings.TrimSpace(input.RedirectURI)
	if clientID == "" || redirectURI == "" {
		return nil, nil, ErrOAuthInvalidRequest
	}
	parsed, err := url.Parse(redirectURI)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Fragment != "" {
		return nil, nil, ErrOAuthInvalidRedirectURI
	}
	client, err := s.clientRepo.GetByClientID(ctx, clientID)
	if err != nil || !client.IsActive() {
		return nil, nil, ErrOAuthInvalidClient
	}
	if !isRedirectURIAllowed(client, redirectURI) {
		return nil, nil, ErrOAuthInvalidRedirectURI
	}
	if err := validateCodeChallenge(input.CodeChallenge, input.CodeChallengeMethod); err != nil {
		return nil, nil, err
	}
	scopes := normalizeScopes(input.Scope)
	if len(scopes) == 0 {
		scopes = append([]string(nil), client.Scopes...)
		sort.Strings(scopes)
	}
	for _, scope := range scopes {
		if !containsExact(client.Scopes, scope) {
			return nil, nil, ErrOAuthInvalidScope
		}
	}
	if input.APIKeyID != nil {
		if err := s.validateAPIKeyBinding(ctx, 0, *input.APIKeyID); err != nil {
			return nil, nil, err
		}
	}
	return client, scopes, nil
}

func (s *OAuthAuthorizationService) validateAPIKeyBinding(ctx context.Context, userID, apiKeyID int64) error {
	if apiKeyID <= 0 {
		return ErrOAuthInvalidRequest
	}
	if s.apiKeyRepo == nil {
		return nil
	}
	apiKey, err := s.apiKeyRepo.GetByID(ctx, apiKeyID)
	if err != nil {
		return ErrOAuthInvalidRequest
	}
	if apiKey == nil || !apiKey.IsActive() || apiKey.User == nil || !apiKey.User.IsActive() {
		return ErrOAuthInvalidRequest
	}
	if userID > 0 && apiKey.UserID != userID {
		return ErrOAuthInvalidRequest
	}
	return nil
}

func (s *OAuthAuthorizationService) generateOAuthAccessToken(userID int64, apiKeyID *int64, clientID string, scopes []string) (string, error) {
	if s.cfg == nil || s.cfg.JWT.Secret == "" {
		return "", errors.New("jwt secret not configured")
	}
	claimAPIKeyID := int64(0)
	if apiKeyID != nil {
		claimAPIKeyID = *apiKeyID
	}
	now := time.Now()
	claims := &OAuthAccessTokenClaims{
		UserID:   userID,
		APIKeyID: claimAPIKeyID,
		ClientID: clientID,
		Scope:    append([]string(nil), scopes...),
		Purpose:  "oauth_access_token",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   fmt.Sprintf("%d", userID),
			Audience:  jwt.ClaimStrings{clientID},
			ExpiresAt: jwt.NewNumericDate(now.Add(OAuthAccessTokenTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.cfg.JWT.Secret))
}

func validateCodeChallenge(challenge, method string) error {
	challenge = strings.TrimSpace(challenge)
	method = normalizedCodeChallengeMethod(method)
	if challenge == "" {
		return ErrOAuthInvalidPKCE
	}
	if len(challenge) < 43 || len(challenge) > 128 {
		return ErrOAuthInvalidPKCE
	}
	if method != "S256" {
		return ErrOAuthInvalidPKCE
	}
	for _, r := range challenge {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '.' || r == '_' || r == '~' {
			continue
		}
		return ErrOAuthInvalidPKCE
	}
	return nil
}

func validatePKCE(challenge, method, verifier string) error {
	challenge = strings.TrimSpace(challenge)
	if challenge == "" {
		return ErrOAuthInvalidPKCE
	}
	verifier = strings.TrimSpace(verifier)
	if err := validatePKCEVerifier(verifier); err != nil {
		return ErrOAuthInvalidPKCE
	}
	method = normalizedCodeChallengeMethod(method)
	var expected string
	switch method {
	case "S256":
		sum := sha256.Sum256([]byte(verifier))
		expected = base64.RawURLEncoding.EncodeToString(sum[:])
	default:
		return ErrOAuthInvalidPKCE
	}
	if subtle.ConstantTimeCompare([]byte(expected), []byte(challenge)) != 1 {
		return ErrOAuthInvalidPKCE
	}
	return nil
}

func validatePKCEVerifier(verifier string) error {
	if len(verifier) < 43 || len(verifier) > 128 {
		return ErrOAuthInvalidPKCE
	}
	for _, r := range verifier {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '.' || r == '_' || r == '~' {
			continue
		}
		return ErrOAuthInvalidPKCE
	}
	return nil
}

func normalizedCodeChallengeMethod(method string) string {
	method = strings.TrimSpace(method)
	if method == "" {
		return ""
	}
	return method
}

func validateOAuthClientTokenAuth(client *OAuthClient, clientSecret string) error {
	if client == nil || !client.IsActive() {
		return ErrOAuthInvalidClient
	}
	switch client.ClientType {
	case "", OAuthClientTypeConfidential:
		if strings.TrimSpace(clientSecret) == "" || client.ClientSecretHash == "" {
			return ErrOAuthInvalidClient
		}
		if bcrypt.CompareHashAndPassword([]byte(client.ClientSecretHash), []byte(clientSecret)) != nil {
			return ErrOAuthInvalidClient
		}
	case OAuthClientTypePublic:
		if strings.TrimSpace(clientSecret) != "" {
			return ErrOAuthInvalidClient
		}
	default:
		return ErrOAuthInvalidClient
	}
	return nil
}

func isRedirectURIAllowed(client *OAuthClient, redirectURI string) bool {
	if containsExact(client.RedirectURIs, redirectURI) {
		return true
	}
	if !client.AllowLoopbackRedirect {
		return false
	}
	parsed, err := url.Parse(redirectURI)
	if err != nil || parsed.Scheme != "http" || parsed.Path != "/auth/callback" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	if parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost" {
		return false
	}
	return parsed.Port() != ""
}

func (s *OAuthAuthorizationService) hashOAuthSecret(raw string) (string, string, error) {
	key := ""
	if s != nil && s.cfg != nil {
		key = s.cfg.JWT.Secret
	}
	if key == "" {
		return "", "", errors.New("oauth hmac key not configured")
	}
	return "default", HashOAuthSecretWithKey(raw, key), nil
}

func HashOAuthSecretWithKey(value, key string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

func HashOAuthClientSecret(secret string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func (s *OAuthAuthorizationService) createRefreshToken(ctx context.Context, userID int64, apiKeyID *int64, clientID string, scopes []string, familyID, parentTokenHash string) (string, error) {
	rawToken, token, err := s.newRefreshTokenRecord(userID, apiKeyID, clientID, scopes, familyID, parentTokenHash)
	if err != nil {
		return "", err
	}
	if err := s.refreshTokenRepo.Create(ctx, token); err != nil {
		return "", err
	}
	return rawToken, nil
}

func (s *OAuthAuthorizationService) newRefreshTokenRecord(userID int64, apiKeyID *int64, clientID string, scopes []string, familyID, parentTokenHash string) (string, *OAuthRefreshToken, error) {
	rawToken, err := randomTokenHex(48)
	if err != nil {
		return "", nil, fmt.Errorf("generate refresh token: %w", err)
	}
	keyID, tokenHash, err := s.hashOAuthSecret(rawToken)
	if err != nil {
		return "", nil, err
	}
	if familyID == "" {
		familyID, err = randomTokenHex(16)
		if err != nil {
			return "", nil, fmt.Errorf("generate refresh token family: %w", err)
		}
	}
	var parent *string
	if parentTokenHash != "" {
		parent = &parentTokenHash
	}
	token := &OAuthRefreshToken{
		TokenHash:       tokenHash,
		HMACKeyID:       keyID,
		FamilyID:        familyID,
		ParentTokenHash: parent,
		UserID:          userID,
		APIKeyID:        apiKeyID,
		ClientID:        clientID,
		Scopes:          append([]string(nil), scopes...),
		Status:          OAuthRefreshTokenStatusActive,
		ExpiresAt:       time.Now().Add(OAuthRefreshTokenTTL),
	}
	return rawToken, token, nil
}

func randomTokenHex(byteLength int) (string, error) {
	buf := make([]byte, byteLength)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func normalizeScopes(raw string) []string {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(fields))
	out := make([]string, 0, len(fields))
	for _, scope := range fields {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		out = append(out, scope)
	}
	sort.Strings(out)
	return out
}

func containsExact(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func buildOAuthRedirect(rawURL string, values map[string]string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := u.Query()
	for key, value := range values {
		if value == "" {
			continue
		}
		q.Set(key, value)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
