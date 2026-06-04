package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	smsVerifyCodeTTL         = 15 * time.Minute
	smsVerifyCodeCooldown    = 60 * time.Second
	maxSmsVerifyCodeAttempts = 5

	// Tencent Cloud SMS API endpoint (inner endpoint)
	tencentSmsEndpoint = "sms.internal.tencentcloudapi.com"
)

// SMS configuration errors
var (
	ErrSmsNotConfigured    = infraerrors.ServiceUnavailable("SMS_NOT_CONFIGURED", "SMS service not configured")
	ErrInvalidSmsCode      = infraerrors.BadRequest("INVALID_SMS_CODE", "invalid or expired verification code")
	ErrSmsCodeTooFrequent  = infraerrors.TooManyRequests("SMS_CODE_TOO_FREQUENT", "please wait before requesting a new code")
	ErrSmsCodeMaxAttempts  = infraerrors.TooManyRequests("SMS_CODE_MAX_ATTEMPTS", "too many failed attempts, please request a new code")
	ErrSmsSendFailed       = infraerrors.ServiceUnavailable("SMS_SEND_FAILED", "failed to send SMS verification code")
	ErrPhoneLoginDisabled  = infraerrors.Forbidden("PHONE_LOGIN_DISABLED", "phone login is currently disabled")
	ErrPhoneNotBound       = infraerrors.BadRequest("PHONE_NOT_BOUND", "this phone number is not bound to any account")
)

// PhoneCodeCache defines cache operations for SMS verification codes.
type PhoneCodeCache interface {
	GetVerificationCode(ctx context.Context, purpose, phone string) (*VerificationCodeData, error)
	SetVerificationCode(ctx context.Context, purpose, phone string, data *VerificationCodeData, ttl time.Duration) error
	DeleteVerificationCode(ctx context.Context, purpose, phone string) error
	IsInCooldown(ctx context.Context, phone string) bool
	SetCooldown(ctx context.Context, phone string, ttl time.Duration) error
}

// SmsService handles SMS verification code sending and validation.
type SmsService struct {
	settingRepo SettingRepository
	cache       PhoneCodeCache
	httpClient  *http.Client
}

// NewSmsService creates a new SmsService.
func NewSmsService(settingRepo SettingRepository, cache PhoneCodeCache) *SmsService {
	return &SmsService{
		settingRepo: settingRepo,
		cache:       cache,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
	}
}

// SetHTTPClient allows injection of a custom HTTP client (e.g. with proxy support).
func (s *SmsService) SetHTTPClient(client *http.Client) {
	if client != nil {
		s.httpClient = client
	}
}

// IsConfigured checks whether Tencent Cloud SMS is properly configured.
func (s *SmsService) IsConfigured(ctx context.Context) bool {
	if s.settingRepo == nil {
		return false
	}
	keys := []string{
		SettingKeyTencentSmsSecretID,
		SettingKeyTencentSmsSecretKey,
		SettingKeyTencentSmsSdkAppID,
		SettingKeyTencentSmsSignName,
		SettingKeyTencentSmsTemplateID,
	}
	settings, err := s.settingRepo.GetMultiple(ctx, keys)
	if err != nil {
		return false
	}
	secretID := strings.TrimSpace(settings[SettingKeyTencentSmsSecretID])
	secretKey := strings.TrimSpace(settings[SettingKeyTencentSmsSecretKey])
	appID := strings.TrimSpace(settings[SettingKeyTencentSmsSdkAppID])
	signName := strings.TrimSpace(settings[SettingKeyTencentSmsSignName])
	templateID := strings.TrimSpace(settings[SettingKeyTencentSmsTemplateID])
	return secretID != "" && secretKey != "" && appID != "" && signName != "" && templateID != ""
}

// GenerateVerifyCode generates a 6-digit numeric verification code.
func (s *SmsService) GenerateVerifyCode() (string, error) {
	const digits = "0123456789"
	code := make([]byte, 6)
	for i := range code {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(digits))))
		if err != nil {
			return "", err
		}
		code[i] = digits[num.Int64()]
	}
	return string(code), nil
}

// SendVerifyCode sends an SMS verification code for the given purpose and phone number.
// "login" - phone code login; "bind" - binding phone to account.
func (s *SmsService) SendVerifyCode(ctx context.Context, purpose, phone string) error {
	if !s.IsConfigured(ctx) {
		return ErrSmsNotConfigured
	}

	// Check cooldown
	if s.cache.IsInCooldown(ctx, phone) {
		return ErrSmsCodeTooFrequent
	}

	// Generate code
	code, err := s.GenerateVerifyCode()
	if err != nil {
		return fmt.Errorf("generate code: %w", err)
	}

	// Save to Redis
	data := &VerificationCodeData{
		Code:      code,
		Attempts:  0,
		CreatedAt: time.Now(),
	}
	if err := s.cache.SetVerificationCode(ctx, purpose, phone, data, smsVerifyCodeTTL); err != nil {
		return fmt.Errorf("save sms code: %w", err)
	}

	// Send SMS via Tencent Cloud
	if err := s.sendTencentSMS(ctx, phone, code); err != nil {
		// Remove saved code on send failure to prevent stuck state
		_ = s.cache.DeleteVerificationCode(ctx, purpose, phone)
		return err
	}

	// Set cooldown after successful send
	_ = s.cache.SetCooldown(ctx, phone, smsVerifyCodeCooldown)

	return nil
}

// VerifyCode validates a verification code for the given purpose and phone number.
func (s *SmsService) VerifyCode(ctx context.Context, purpose, phone, code string) error {
	data, err := s.cache.GetVerificationCode(ctx, purpose, phone)
	if err != nil || data == nil {
		return ErrInvalidSmsCode
	}

	// Check max attempts
	if data.Attempts >= maxSmsVerifyCodeAttempts {
		return ErrSmsCodeMaxAttempts
	}

	// Constant-time comparison to prevent timing attacks
	if subtle.ConstantTimeCompare([]byte(data.Code), []byte(code)) != 1 {
		data.Attempts++
		if err := s.cache.SetVerificationCode(ctx, purpose, phone, data, smsVerifyCodeTTL); err != nil {
			// Non-fatal: just log the failure in production
		}
		if data.Attempts >= maxSmsVerifyCodeAttempts {
			return ErrSmsCodeMaxAttempts
		}
		return ErrInvalidSmsCode
	}

	// Success: delete the code
	if err := s.cache.DeleteVerificationCode(ctx, purpose, phone); err != nil {
		// Non-fatal: code will expire naturally
	}
	return nil
}

// tencentCloudConfig holds Tencent Cloud SMS configuration from settings.
type tencentCloudConfig struct {
	SecretID   string
	SecretKey  string
	AppID      string
	SignName   string
	TemplateID string
	Region     string
}

func (s *SmsService) getTencentConfig(ctx context.Context) (*tencentCloudConfig, error) {
	keys := []string{
		SettingKeyTencentSmsSecretID,
		SettingKeyTencentSmsSecretKey,
		SettingKeyTencentSmsSdkAppID,
		SettingKeyTencentSmsSignName,
		SettingKeyTencentSmsTemplateID,
		SettingKeyTencentSmsRegion,
	}
	settings, err := s.settingRepo.GetMultiple(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("get sms settings: %w", err)
	}

	cfg := &tencentCloudConfig{
		SecretID:   strings.TrimSpace(settings[SettingKeyTencentSmsSecretID]),
		SecretKey:  strings.TrimSpace(settings[SettingKeyTencentSmsSecretKey]),
		AppID:      strings.TrimSpace(settings[SettingKeyTencentSmsSdkAppID]),
		SignName:   strings.TrimSpace(settings[SettingKeyTencentSmsSignName]),
		TemplateID: strings.TrimSpace(settings[SettingKeyTencentSmsTemplateID]),
		Region:     strings.TrimSpace(settings[SettingKeyTencentSmsRegion]),
	}
	if cfg.Region == "" {
		cfg.Region = "ap-guangzhou"
	}

	return cfg, nil
}

// sendTencentSMS sends an SMS via Tencent Cloud API v3 (TC3-HMAC-SHA256 signing).
func (s *SmsService) sendTencentSMS(ctx context.Context, phone, code string) error {
	cfg, err := s.getTencentConfig(ctx)
	if err != nil {
		return ErrSmsNotConfigured
	}

	// Build request body
	reqBody := map[string]interface{}{
		"PhoneNumberSet":   []string{"+86" + phone},
		"SmsSdkAppId":      cfg.AppID,
		"SignName":         cfg.SignName,
		"TemplateId":       cfg.TemplateID,
		"TemplateParamSet": []string{code},
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	return s.tencentCloudAPI(ctx, cfg, "SendSms", string(payload))
}

// tencentCloudAPI performs a Tencent Cloud API v3 signed request.
func (s *SmsService) tencentCloudAPI(ctx context.Context, cfg *tencentCloudConfig, action, payload string) error {
	service := "sms"
	endpoint := tencentSmsEndpoint
	algorithm := "TC3-HMAC-SHA256"
	timestamp := time.Now().UTC().Unix()
	date := time.Unix(timestamp, 0).UTC().Format("2006-01-02")

	// Step 1: Build canonical request
	httpRequestMethod := "POST"
	canonicalURI := "/"
	canonicalQueryString := ""
	canonicalHeaders := fmt.Sprintf("content-type:application/json; charset=utf-8\nhost:%s\nx-tc-action:%s\n", endpoint, strings.ToLower(action))
	signedHeaders := "content-type;host;x-tc-action"
	hashedRequestPayload := sha256Hex(payload)
	canonicalRequest := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		httpRequestMethod, canonicalURI, canonicalQueryString,
		canonicalHeaders, signedHeaders, hashedRequestPayload)

	// Step 2: Build string to sign
	credentialScope := fmt.Sprintf("%s/%s/tc3_request", date, service)
	hashedCanonicalRequest := sha256Hex(canonicalRequest)
	stringToSign := fmt.Sprintf("%s\n%d\n%s\n%s",
		algorithm, timestamp, credentialScope, hashedCanonicalRequest)

	// Step 3: Calculate signature
	secretDate := hmacSHA256([]byte("TC3"+cfg.SecretKey), date)
	secretService := hmacSHA256(secretDate, service)
	secretSigning := hmacSHA256(secretService, "tc3_request")
	signature := hex.EncodeToString(hmacSHA256(secretSigning, stringToSign))

	// Step 4: Build authorization header
	authorization := fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		algorithm, cfg.SecretID, credentialScope, signedHeaders, signature)

	// Step 5: Send request
	u := url.URL{Scheme: "https", Host: endpoint, Path: "/"}
	req, err := http.NewRequestWithContext(ctx, httpRequestMethod, u.String(), strings.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Host", endpoint)
	req.Header.Set("X-TC-Action", action)
	req.Header.Set("X-TC-Timestamp", fmt.Sprintf("%d", timestamp))
	req.Header.Set("X-TC-Version", "2021-01-11")
	req.Header.Set("X-TC-Region", cfg.Region)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", ErrSmsSendFailed)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))

	var result struct {
		Response struct {
			SendStatusSet []struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"SendStatusSet"`
			Error struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"Error"`
			RequestId string `json:"RequestId"`
		} `json:"Response"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("parse response: %w", ErrSmsSendFailed)
	}

	if result.Response.Error.Code != "" {
		return fmt.Errorf("%w: %s", ErrSmsSendFailed, result.Response.Error.Message)
	}

	if len(result.Response.SendStatusSet) == 0 || result.Response.SendStatusSet[0].Code != "Ok" {
		msg := "unknown error"
		if len(result.Response.SendStatusSet) > 0 {
			msg = result.Response.SendStatusSet[0].Message
		}
		return fmt.Errorf("%w: %s", ErrSmsSendFailed, msg)
	}

	return nil
}

// hmacSHA256 computes HMAC-SHA256.
func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

// sha256Hex computes SHA-256 hash and returns hex string.
func sha256Hex(s string) string {
	h := sha256.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}
