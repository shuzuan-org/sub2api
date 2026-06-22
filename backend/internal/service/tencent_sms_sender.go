package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// TencentSMSSender 腾讯云短信发送器（API v3 签名）。
type TencentSMSSender struct {
	httpClient *http.Client
	ss         SettingRepository
}

// NewTencentSMSSender 创建腾讯云短信发送器。
func NewTencentSMSSender(ss SettingRepository) *TencentSMSSender {
	return &TencentSMSSender{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		ss:         ss,
	}
}

// SendSMS 发送短信验证码。
func (s *TencentSMSSender) SendSMS(ctx context.Context, phone, code, signName, templateID, sdkAppID string) error {
	keys := []string{
		SettingKeySMSTencentSecretID,
		SettingKeySMSTencentSecretKey,
		SettingKeySMSTencentRegion,
	}
	settings, err := s.ss.GetMultiple(ctx, keys)
	if err != nil {
		return fmt.Errorf("get sms credentials: %w", err)
	}

	secretID := strings.TrimSpace(settings[SettingKeySMSTencentSecretID])
	secretKey := strings.TrimSpace(settings[SettingKeySMSTencentSecretKey])
	region := strings.TrimSpace(settings[SettingKeySMSTencentRegion])
	if region == "" {
		region = "ap-guangzhou"
	}
	if secretID == "" || secretKey == "" {
		return fmt.Errorf("tencent sms secret id/key not configured")
	}

	// API v3 请求体
	body := map[string]interface{}{
		"PhoneNumberSet":   []string{phone},
		"SmsSdkAppId":      sdkAppID,
		"SignName":         signName,
		"TemplateId":       templateID,
		"TemplateParamSet": []string{code, "15"},
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal sms request: %w", err)
	}

	service := "sms"
	host := "sms.tencentcloudapi.com"
	algorithm := "TC3-HMAC-SHA256"
	timestamp := time.Now().Unix()
	date := time.Unix(timestamp, 0).UTC().Format("2006-01-02")

	// 1. Canonical Request
	httpRequestMethod := "POST"
	canonicalURI := "/"
	canonicalQueryString := ""
	action := "SendSms"
	canonicalHeaders := fmt.Sprintf("content-type:application/json; charset=utf-8\nhost:%s\nx-tc-action:%s\n", host, strings.ToLower(action))
	signedHeaders := "content-type;host;x-tc-action"
	hashedRequestPayload := sha256Hex(payload)
	canonicalRequest := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		httpRequestMethod, canonicalURI, canonicalQueryString,
		canonicalHeaders, signedHeaders, hashedRequestPayload)

	// 2. String to Sign
	credentialScope := fmt.Sprintf("%s/%s/tc3_request", date, service)
	hashedCanonicalRequest := sha256Hex([]byte(canonicalRequest))
	stringToSign := fmt.Sprintf("%s\n%d\n%s\n%s", algorithm, timestamp, credentialScope, hashedCanonicalRequest)

	// 3. Signature
	secretDate := hmacSHA256([]byte("TC3"+secretKey), []byte(date))
	secretService := hmacSHA256(secretDate, []byte(service))
	secretSigning := hmacSHA256(secretService, []byte("tc3_request"))
	signature := hex.EncodeToString(hmacSHA256(secretSigning, []byte(stringToSign)))

	// 4. Authorization
	authorization := fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		algorithm, secretID, credentialScope, signedHeaders, signature)

	url := "https://" + host

	req, err := http.NewRequestWithContext(ctx, httpRequestMethod, url, strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("create sms request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Host", host)
	req.Header.Set("X-TC-Action", action)
	req.Header.Set("X-TC-Version", "2021-01-11")
	req.Header.Set("X-TC-Timestamp", fmt.Sprintf("%d", timestamp))
	req.Header.Set("X-TC-Region", region)
	req.Header.Set("Authorization", authorization)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send sms request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	if resp.StatusCode != http.StatusOK {
		return tencentSMSError("sms api returned non-200 status", fmt.Sprintf("HTTP_%d", resp.StatusCode), string(respBody))
	}

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
		} `json:"Response"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("parse sms response: %w", err)
	}

	if result.Response.Error.Code != "" {
		return tencentSMSError("sms api error", result.Response.Error.Code, result.Response.Error.Message)
	}
	for _, status := range result.Response.SendStatusSet {
		if status.Code != "Ok" {
			return tencentSMSError("sms send status error", status.Code, status.Message)
		}
	}

	return nil
}

func tencentSMSError(prefix, code, message string) error {
	cause := fmt.Errorf("%s: %s - %s", prefix, code, message)
	metadata := map[string]string{"provider_code": code}
	if strings.Contains(code, "LimitExceeded") {
		return ErrPhoneSMSSendRateLimit.WithMetadata(metadata).WithCause(cause)
	}
	return ErrPhoneSMSSendFailed.WithMetadata(metadata).WithCause(cause)
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}
