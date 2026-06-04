package service

import (
	"context"
	"time"
)

// SmsCache defines cache operations for phone SMS verification code.
type SmsCache interface {
	GetSmsCode(ctx context.Context, phone string) (*SmsCodeData, error)
	SetSmsCode(ctx context.Context, phone string, data *SmsCodeData, ttl time.Duration) error
	DeleteSmsCode(ctx context.Context, phone string) error
}

// SmsCodeData represents SMS verification code data stored in cache.
type SmsCodeData struct {
	Code      string    `json:"code"`
	Attempts  int       `json:"attempts"`
	CreatedAt time.Time `json:"created_at"`
}
