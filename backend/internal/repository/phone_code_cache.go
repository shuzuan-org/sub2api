package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	smsVerifyCodeKeyPrefix  = "sms_verify_code:"
	smsCooldownKeyPrefix    = "sms_cooldown:"
)

// smsVerifyCodeKey generates the Redis key for SMS verification code storage.
func smsVerifyCodeKey(purpose, recipient string) string {
	return smsVerifyCodeKeyPrefix + purpose + ":" + recipient
}

// smsCooldownKey generates the Redis key for SMS cooldown tracking.
func smsCooldownKey(phone string) string {
	return smsCooldownKeyPrefix + phone
}

type phoneCodeCache struct {
	rdb *redis.Client
}

// NewPhoneCodeCache creates a new PhoneCodeCache backed by Redis.
func NewPhoneCodeCache(rdb *redis.Client) service.PhoneCodeCache {
	return &phoneCodeCache{rdb: rdb}
}

func (c *phoneCodeCache) GetVerificationCode(ctx context.Context, purpose, phone string) (*service.VerificationCodeData, error) {
	key := smsVerifyCodeKey(purpose, phone)
	val, err := c.rdb.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	var data service.VerificationCodeData
	if err := json.Unmarshal([]byte(val), &data); err != nil {
		return nil, err
	}
	return &data, nil
}

func (c *phoneCodeCache) SetVerificationCode(ctx context.Context, purpose, phone string, data *service.VerificationCodeData, ttl time.Duration) error {
	key := smsVerifyCodeKey(purpose, phone)
	val, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return c.rdb.Set(ctx, key, val, ttl).Err()
}

func (c *phoneCodeCache) DeleteVerificationCode(ctx context.Context, purpose, phone string) error {
	key := smsVerifyCodeKey(purpose, phone)
	return c.rdb.Del(ctx, key).Err()
}

func (c *phoneCodeCache) IsInCooldown(ctx context.Context, phone string) bool {
	key := smsCooldownKey(phone)
	exists, err := c.rdb.Exists(ctx, key).Result()
	return err == nil && exists > 0
}

func (c *phoneCodeCache) SetCooldown(ctx context.Context, phone string, ttl time.Duration) error {
	key := smsCooldownKey(phone)
	return c.rdb.Set(ctx, key, "1", ttl).Err()
}
