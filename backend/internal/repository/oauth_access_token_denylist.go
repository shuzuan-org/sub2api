package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const oauthAccessTokenRevokedPrefix = "oauth:access_token:revoked:"

type oauthAccessTokenDenylist struct {
	rdb *redis.Client
}

func NewOAuthAccessTokenDenylist(rdb *redis.Client) service.OAuthAccessTokenDenylist {
	return &oauthAccessTokenDenylist{rdb: rdb}
}

func oauthAccessTokenRevokedKey(jti string) string {
	return oauthAccessTokenRevokedPrefix + strings.TrimSpace(jti)
}

func (d *oauthAccessTokenDenylist) Revoke(ctx context.Context, jti string, expiresAt time.Time) error {
	jti = strings.TrimSpace(jti)
	if d == nil || d.rdb == nil || jti == "" {
		return fmt.Errorf("oauth access token denylist unavailable")
	}
	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		return nil
	}
	return d.rdb.Set(ctx, oauthAccessTokenRevokedKey(jti), "1", ttl).Err()
}

func (d *oauthAccessTokenDenylist) IsRevoked(ctx context.Context, jti string) (bool, error) {
	jti = strings.TrimSpace(jti)
	if d == nil || d.rdb == nil || jti == "" {
		return false, fmt.Errorf("oauth access token denylist unavailable")
	}
	exists, err := d.rdb.Exists(ctx, oauthAccessTokenRevokedKey(jti)).Result()
	if err != nil {
		return false, err
	}
	return exists > 0, nil
}
