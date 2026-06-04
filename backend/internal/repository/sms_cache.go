package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const smsCodeKeyPrefixRepo = "sms_code:"

func smsCodeKeyRepo(phone string) string {
	return smsCodeKeyPrefixRepo + phone
}

type smsCacheImpl struct {
	rdb *redis.Client
}

// NewSmsCache creates a new SMS code cache backed by Redis.
func NewSmsCache(rdb *redis.Client) service.SmsCache {
	return &smsCacheImpl{rdb: rdb}
}

func (c *smsCacheImpl) GetSmsCode(ctx context.Context, phone string) (*service.SmsCodeData, error) {
	key := smsCodeKeyRepo(phone)
	val, err := c.rdb.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	var data service.SmsCodeData
	if err := json.Unmarshal([]byte(val), &data); err != nil {
		return nil, err
	}
	return &data, nil
}

func (c *smsCacheImpl) SetSmsCode(ctx context.Context, phone string, data *service.SmsCodeData, ttl time.Duration) error {
	key := smsCodeKeyRepo(phone)
	val, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return c.rdb.Set(ctx, key, val, ttl).Err()
}

func (c *smsCacheImpl) DeleteSmsCode(ctx context.Context, phone string) error {
	key := smsCodeKeyRepo(phone)
	return c.rdb.Del(ctx, key).Err()
}
