package service

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"math/big"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrPhoneSMSNotConfigured   = infraerrors.ServiceUnavailable("SMS_NOT_CONFIGURED", "SMS service not configured")
	ErrPhoneSMSSendFailed      = infraerrors.ServiceUnavailable("SMS_SEND_FAILED", "failed to send sms verification code")
	ErrPhoneSMSSendRateLimit   = infraerrors.TooManyRequests("SMS_SEND_RATE_LIMITED", "sms sending limit exceeded, please try again later")
	ErrPhoneVerifyCodeTooFreq  = infraerrors.TooManyRequests("PHONE_CODE_TOO_FREQUENT", "please wait before requesting a new code")
	ErrPhoneVerifyCodeMaxAttms = infraerrors.TooManyRequests("PHONE_CODE_MAX_ATTEMPTS", "too many failed attempts, please request a new code")
	ErrInvalidPhoneVerifyCode  = infraerrors.BadRequest("INVALID_PHONE_VERIFY_CODE", "invalid or expired verification code")
)

const (
	phoneVerifyCodeTTL      = 15 * time.Minute
	phoneVerifyCodeCooldown = 60 * time.Second
	phoneMaxVerifyAttempts  = 5
)

// SMSSender 短信发送接口，方便测试替换。
type SMSSender interface {
	SendSMS(ctx context.Context, phone, code, signName, templateID, sdkAppID string) error
}

// PhoneVerificationService 手机号验证码服务。
type PhoneVerificationService struct {
	cache  SmsCache
	sender SMSSender
	ss     SettingRepository
}

// NewPhoneVerificationService 创建手机验证码服务实例。
func NewPhoneVerificationService(cache SmsCache, sender SMSSender, ss SettingRepository) *PhoneVerificationService {
	return &PhoneVerificationService{
		cache:  cache,
		sender: sender,
		ss:     ss,
	}
}

// getSMSConfig 从数据库获取腾讯云短信配置。
func (s *PhoneVerificationService) getSMSConfig(ctx context.Context) (enabled bool, signName, templateID, sdkAppID string, err error) {
	keys := []string{
		SettingKeySMSTencentEnabled,
		SettingKeySMSTencentSignName,
		SettingKeySMSTencentTemplateID,
		SettingKeySMSTencentSdkAppID,
	}
	settings, err := s.ss.GetMultiple(ctx, keys)
	if err != nil {
		return false, "", "", "", fmt.Errorf("get sms settings: %w", err)
	}

	enabled = settings[SettingKeySMSTencentEnabled] == "true"
	signName = strings.TrimSpace(settings[SettingKeySMSTencentSignName])
	templateID = strings.TrimSpace(settings[SettingKeySMSTencentTemplateID])
	sdkAppID = strings.TrimSpace(settings[SettingKeySMSTencentSdkAppID])

	if !enabled || signName == "" || templateID == "" || sdkAppID == "" {
		return false, "", "", "", nil
	}

	return true, signName, templateID, sdkAppID, nil
}

// GenerateVerifyCode 生成 6 位数字验证码。
func (s *PhoneVerificationService) GenerateVerifyCode() (string, error) {
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

// SendVerifyCode 发送短信验证码到指定手机号。
func (s *PhoneVerificationService) SendVerifyCode(ctx context.Context, phone string) (int, error) {
	// 检查是否在冷却期内
	existing, err := s.cache.GetSmsCode(ctx, phone)
	if err == nil && existing != nil {
		if time.Since(existing.CreatedAt) < phoneVerifyCodeCooldown {
			return phoneVerifyCodeCooldownSecs, ErrPhoneVerifyCodeTooFreq
		}
	}

	// 检查短信配置
	enabled, signName, templateID, sdkAppID, cfgErr := s.getSMSConfig(ctx)
	if cfgErr != nil {
		return phoneVerifyCodeCooldownSecs, cfgErr
	}
	if !enabled {
		return phoneVerifyCodeCooldownSecs, ErrPhoneSMSNotConfigured
	}

	// 生成验证码
	code, err := s.GenerateVerifyCode()
	if err != nil {
		return phoneVerifyCodeCooldownSecs, fmt.Errorf("generate code: %w", err)
	}

	// 发送真实短信
	if err := s.sender.SendSMS(ctx, phone, code, signName, templateID, sdkAppID); err != nil {
		return phoneVerifyCodeCooldownSecs, fmt.Errorf("send sms: %w", err)
	}

	// 保存到 Redis
	data := &SmsCodeData{
		Code:      code,
		Attempts:  0,
		CreatedAt: time.Now(),
	}
	if err := s.cache.SetSmsCode(ctx, phone, data, phoneVerifyCodeTTL); err != nil {
		return phoneVerifyCodeCooldownSecs, fmt.Errorf("save sms code: %w", err)
	}

	return phoneVerifyCodeCooldownSecs, nil
}

const phoneVerifyCodeCooldownSecs = 60

// VerifyCode 验证短信验证码。
func (s *PhoneVerificationService) VerifyCode(ctx context.Context, phone, code string) error {
	data, err := s.cache.GetSmsCode(ctx, phone)
	if err != nil || data == nil {
		return ErrInvalidPhoneVerifyCode
	}

	if data.Attempts >= phoneMaxVerifyAttempts {
		return ErrPhoneVerifyCodeMaxAttms
	}

	if subtle.ConstantTimeCompare([]byte(data.Code), []byte(code)) != 1 {
		data.Attempts++
		if err := s.cache.SetSmsCode(ctx, phone, data, phoneVerifyCodeTTL); err != nil {
			_ = err
		}
		if data.Attempts >= phoneMaxVerifyAttempts {
			return ErrPhoneVerifyCodeMaxAttms
		}
		return ErrInvalidPhoneVerifyCode
	}

	if err := s.cache.DeleteSmsCode(ctx, phone); err != nil {
		_ = err
	}
	return nil
}
