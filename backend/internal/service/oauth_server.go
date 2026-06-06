package service

import (
	"context"
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
	OAuthAuthorizationCodeTTL = 5 * time.Minute
	OAuthAccessTokenTTL       = time.Hour
	OAuthTokenTypeBearer      = "Bearer"
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
	ID               int64
	ClientID         string
	ClientSecretHash string
	Name             string
	RedirectURIs     []string
	Scopes           []string
	Status           string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (c *OAuthClient) IsActive() bool {
	return c != nil && c.Status == StatusActive
}

type OAuthAuthorizationCode struct {
	ID                  int64
	CodeHash            string
	ClientID            string
	UserID              int64
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
	GetByCodeHash(ctx context.Context, codeHash string) (*OAuthAuthorizationCode, error)
	MarkUsed(ctx context.Context, id int64, usedAt time.Time) error
}

type OAuthAuthorizationService struct {
	clientRepo OAuthClientRepository
	codeRepo   OAuthAuthorizationCodeRepository
	userRepo   UserRepository
	cfg        *config.Config
}

func NewOAuthAuthorizationService(
	clientRepo OAuthClientRepository,
	codeRepo OAuthAuthorizationCodeRepository,
	userRepo UserRepository,
	cfg *config.Config,
) *OAuthAuthorizationService {
	return &OAuthAuthorizationService{clientRepo: clientRepo, codeRepo: codeRepo, userRepo: userRepo, cfg: cfg}
}

type OAuthAuthorizeInput struct {
	ClientID            string
	RedirectURI         string
	ResponseType        string
	Scope               string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
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
}

type OAuthTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope,omitempty"`
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

	rawCode, err := randomTokenHex(32)
	if err != nil {
		return nil, fmt.Errorf("generate authorization code: %w", err)
	}
	now := time.Now()
	if err := s.codeRepo.Create(ctx, &OAuthAuthorizationCode{
		CodeHash:            HashOAuthSecret(rawCode),
		ClientID:            client.ClientID,
		UserID:              userID,
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

func (s *OAuthAuthorizationService) ExchangeAuthorizationCode(ctx context.Context, input OAuthTokenInput) (*OAuthTokenResponse, error) {
	if strings.TrimSpace(input.GrantType) != "authorization_code" {
		return nil, ErrOAuthUnsupportedGrantType
	}
	clientID := strings.TrimSpace(input.ClientID)
	clientSecret := input.ClientSecret
	if clientID == "" || clientSecret == "" {
		return nil, ErrOAuthInvalidClient
	}
	client, err := s.clientRepo.GetByClientID(ctx, clientID)
	if err != nil {
		return nil, ErrOAuthInvalidClient
	}
	if !client.IsActive() || bcrypt.CompareHashAndPassword([]byte(client.ClientSecretHash), []byte(clientSecret)) != nil {
		return nil, ErrOAuthInvalidClient
	}

	code := strings.TrimSpace(input.Code)
	if code == "" || len(code) > maxTokenLength {
		return nil, ErrOAuthInvalidCode
	}
	authCode, err := s.codeRepo.GetByCodeHash(ctx, HashOAuthSecret(code))
	if err != nil {
		return nil, ErrOAuthInvalidCode
	}
	if subtle.ConstantTimeCompare([]byte(authCode.ClientID), []byte(client.ClientID)) != 1 {
		return nil, ErrOAuthInvalidCode
	}
	if authCode.RedirectURI != strings.TrimSpace(input.RedirectURI) {
		return nil, ErrOAuthInvalidRedirectURI
	}
	if authCode.UsedAt != nil {
		return nil, ErrOAuthCodeUsed
	}
	if err := validatePKCE(authCode.CodeChallenge, authCode.CodeChallengeMethod, input.CodeVerifier); err != nil {
		return nil, err
	}
	if time.Now().After(authCode.ExpiresAt) {
		return nil, ErrOAuthCodeExpired
	}
	if err := s.codeRepo.MarkUsed(ctx, authCode.ID, time.Now()); err != nil {
		return nil, err
	}

	accessToken, err := s.generateOAuthAccessToken(authCode.UserID, client.ClientID, authCode.Scopes)
	if err != nil {
		return nil, err
	}
	return &OAuthTokenResponse{
		AccessToken: accessToken,
		TokenType:   OAuthTokenTypeBearer,
		ExpiresIn:   int(OAuthAccessTokenTTL.Seconds()),
		Scope:       strings.Join(authCode.Scopes, " "),
	}, nil
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
	if !containsExact(client.RedirectURIs, redirectURI) {
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
	return client, scopes, nil
}

func (s *OAuthAuthorizationService) generateOAuthAccessToken(userID int64, clientID string, scopes []string) (string, error) {
	if s.cfg == nil || s.cfg.JWT.Secret == "" {
		return "", errors.New("jwt secret not configured")
	}
	now := time.Now()
	claims := &OAuthAccessTokenClaims{
		UserID:   userID,
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
		return nil
	}
	if len(challenge) < 43 || len(challenge) > 128 {
		return ErrOAuthInvalidPKCE
	}
	if method != "S256" && method != "plain" {
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
		return nil
	}
	verifier = strings.TrimSpace(verifier)
	if err := validateCodeChallenge(verifier, "plain"); err != nil {
		return ErrOAuthInvalidPKCE
	}
	method = normalizedCodeChallengeMethod(method)
	var expected string
	switch method {
	case "plain":
		expected = verifier
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

func normalizedCodeChallengeMethod(method string) string {
	method = strings.TrimSpace(method)
	if method == "" {
		return "plain"
	}
	return method
}

func HashOAuthSecret(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func HashOAuthClientSecret(secret string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
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
