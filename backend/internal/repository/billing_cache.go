package repository

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	billingBalanceKeyPrefix   = "billing:balance:"
	billingSubKeyPrefix       = "billing:sub:"
	billingRateLimitKeyPrefix = "apikey:rate:"
	billingCacheTTL           = 5 * time.Minute
	billingCacheJitter        = 30 * time.Second
	rateLimitCacheTTL         = 7 * 24 * time.Hour // 7 days matches the longest window

	// Rate limit window durations — must match service.RateLimitWindow* constants.
	rateLimitWindow5h = 5 * time.Hour
	rateLimitWindow1d = 24 * time.Hour
	rateLimitWindow7d = 7 * 24 * time.Hour
)

// jitteredTTL 返回带随机抖动的 TTL，防止缓存雪崩
func jitteredTTL() time.Duration {
	// 只做“减法抖动”，确保实际 TTL 不会超过 billingCacheTTL（避免上界预期被打破）。
	if billingCacheJitter <= 0 {
		return billingCacheTTL
	}
	jitter := time.Duration(rand.IntN(int(billingCacheJitter)))
	return billingCacheTTL - jitter
}

// billingBalanceKey generates the Redis key for user balance cache.
func billingBalanceKey(userID int64) string {
	return fmt.Sprintf("%s%d", billingBalanceKeyPrefix, userID)
}

// billingSubKey generates the Redis key for subscription cache.
func billingSubKey(userID int64) string {
	return fmt.Sprintf("%s%d", billingSubKeyPrefix, userID)
}

// 订阅缓存布局：每用户一个 hash key（billing:sub:{userID}），字段按 plan 前缀
// 命名（p{planID}:status 等）。多订阅 FIFO 轮换时各 plan 的快照互不污染，
// usage 增量按 plan 精确记账，而用户级失效仍是一次 DEL。
const (
	subFieldStatus       = "status"
	subFieldExpiresAt    = "expires_at"
	subFieldDailyUsage   = "daily_usage"
	subFieldWeeklyUsage  = "weekly_usage"
	subFieldMonthlyUsage = "monthly_usage"
	subFieldVersion      = "version"
)

// subField 生成 plan 前缀字段名，如 subField(9, "status") → "p9:status"。
func subField(planID int64, name string) string {
	return fmt.Sprintf("p%d:%s", planID, name)
}

// legacySubFields 是 plan 前缀化之前的扁平字段，Set 时顺带清理，避免旧格式
// 字段随 TTL 刷新无限存活。全量迁移完成后可删除。
var legacySubFields = []string{"plan_id", "status", "expires_at", "daily_usage", "weekly_usage", "monthly_usage", "version"}

// billingRateLimitKey generates the Redis key for API key rate limit cache.
func billingRateLimitKey(keyID int64) string {
	return fmt.Sprintf("%s%d", billingRateLimitKeyPrefix, keyID)
}

const (
	rateLimitFieldUsage5h  = "usage_5h"
	rateLimitFieldUsage1d  = "usage_1d"
	rateLimitFieldUsage7d  = "usage_7d"
	rateLimitFieldWindow5h = "window_5h"
	rateLimitFieldWindow1d = "window_1d"
	rateLimitFieldWindow7d = "window_7d"
)

var (
	deductBalanceScript = redis.NewScript(`
		local current = redis.call('GET', KEYS[1])
		if current == false then
			return 0
		end
		local newVal = tonumber(current) - tonumber(ARGV[1])
		redis.call('SET', KEYS[1], newVal)
		redis.call('EXPIRE', KEYS[1], ARGV[2])
		return 1
	`)

	// ARGV: [1]=cost, [2]=ttl_seconds, [3]=plan 字段前缀（如 "p9:"）。
	// 仅当该 plan 的快照存在时才累加，防止把用量记到不存在/别的 plan 头上。
	updateSubUsageScript = redis.NewScript(`
		if redis.call('HEXISTS', KEYS[1], ARGV[3] .. 'status') == 0 then
			return 0
		end
		local cost = tonumber(ARGV[1])
		redis.call('HINCRBYFLOAT', KEYS[1], ARGV[3] .. 'daily_usage', cost)
		redis.call('HINCRBYFLOAT', KEYS[1], ARGV[3] .. 'weekly_usage', cost)
		redis.call('HINCRBYFLOAT', KEYS[1], ARGV[3] .. 'monthly_usage', cost)
		redis.call('EXPIRE', KEYS[1], ARGV[2])
		return 1
	`)

	// updateRateLimitUsageScript atomically increments all three rate limit usage counters
	// with window expiration checking. If a window has expired, its usage is reset to cost
	// (instead of accumulated) and the window timestamp is updated, matching the DB-side
	// IncrementRateLimitUsage semantics.
	//
	// ARGV: [1]=cost, [2]=ttl_seconds, [3]=now_unix, [4]=window_5h_seconds, [5]=window_1d_seconds, [6]=window_7d_seconds
	updateRateLimitUsageScript = redis.NewScript(`
		local exists = redis.call('EXISTS', KEYS[1])
		if exists == 0 then
			return 0
		end
		local cost = tonumber(ARGV[1])
		local now = tonumber(ARGV[3])
		local win5h = tonumber(ARGV[4])
		local win1d = tonumber(ARGV[5])
		local win7d = tonumber(ARGV[6])

		-- Helper: check if window is expired and update usage + window accordingly
		-- Returns nothing, modifies the hash in-place.
		local function update_window(usage_field, window_field, window_duration)
			local w = tonumber(redis.call('HGET', KEYS[1], window_field) or 0)
			if w == 0 or (now - w) >= window_duration then
				-- Window expired or never started: reset usage to cost, start new window
				redis.call('HSET', KEYS[1], usage_field, tostring(cost))
				redis.call('HSET', KEYS[1], window_field, tostring(now))
			else
				-- Window still valid: accumulate
				redis.call('HINCRBYFLOAT', KEYS[1], usage_field, cost)
			end
		end

		update_window('usage_5h', 'window_5h', win5h)
		update_window('usage_1d', 'window_1d', win1d)
		update_window('usage_7d', 'window_7d', win7d)
		redis.call('EXPIRE', KEYS[1], ARGV[2])
		return 1
	`)
)

type billingCache struct {
	rdb *redis.Client
}

func NewBillingCache(rdb *redis.Client) service.BillingCache {
	return &billingCache{rdb: rdb}
}

// FlushBillingKeys 使用 SCAN 批量清除所有计费缓存 key。
// 仅供一次性迁移场景调用（如金额单位变更后清脏数据），不要在每次启动时执行。
func FlushBillingKeys(rdb *redis.Client) {
	ctx := context.Background()
	prefixes := []string{billingBalanceKeyPrefix, billingSubKeyPrefix, billingRateLimitKeyPrefix}
	total := 0
	for _, prefix := range prefixes {
		var cursor uint64
		for {
			keys, nextCursor, err := rdb.Scan(ctx, cursor, prefix+"*", 200).Result()
			if err != nil {
				log.Printf("[BillingCache] flush scan error (prefix=%s): %v", prefix, err)
				break
			}
			if len(keys) > 0 {
				if err := rdb.Del(ctx, keys...).Err(); err != nil {
					log.Printf("[BillingCache] flush del error: %v", err)
				}
				total += len(keys)
			}
			cursor = nextCursor
			if cursor == 0 {
				break
			}
		}
	}
	if total > 0 {
		log.Printf("[BillingCache] flushed %d cached keys", total)
	}
}

func (c *billingCache) GetUserBalance(ctx context.Context, userID int64) (float64, error) {
	key := billingBalanceKey(userID)
	val, err := c.rdb.Get(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	return strconv.ParseFloat(val, 64)
}

func (c *billingCache) SetUserBalance(ctx context.Context, userID int64, balance float64) error {
	key := billingBalanceKey(userID)
	return c.rdb.Set(ctx, key, balance, jitteredTTL()).Err()
}

func (c *billingCache) DeductUserBalance(ctx context.Context, userID int64, amount float64) error {
	key := billingBalanceKey(userID)
	_, err := deductBalanceScript.Run(ctx, c.rdb, []string{key}, amount, int(jitteredTTL().Seconds())).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		log.Printf("Warning: deduct balance cache failed for user %d: %v", userID, err)
		return err
	}
	return nil
}

func (c *billingCache) InvalidateUserBalance(ctx context.Context, userID int64) error {
	key := billingBalanceKey(userID)
	return c.rdb.Del(ctx, key).Err()
}

func (c *billingCache) GetSubscriptionCache(ctx context.Context, userID, planID int64) (*service.SubscriptionCacheData, error) {
	key := billingSubKey(userID)
	fields := []string{
		subField(planID, subFieldStatus),
		subField(planID, subFieldExpiresAt),
		subField(planID, subFieldDailyUsage),
		subField(planID, subFieldWeeklyUsage),
		subField(planID, subFieldMonthlyUsage),
		subField(planID, subFieldVersion),
	}
	vals, err := c.rdb.HMGet(ctx, key, fields...).Result()
	if err != nil {
		return nil, err
	}
	return parseSubscriptionCache(vals)
}

// parseSubscriptionCache 按 GetSubscriptionCache 中 fields 的顺序解析 HMGET 结果。
// status 缺失（该 plan 无快照）视为 cache miss，返回 redis.Nil。
func parseSubscriptionCache(vals []any) (*service.SubscriptionCacheData, error) {
	str := func(i int) string {
		if i < len(vals) {
			if s, ok := vals[i].(string); ok {
				return s
			}
		}
		return ""
	}

	result := &service.SubscriptionCacheData{}
	result.Status = str(0)
	if result.Status == "" {
		return nil, redis.Nil
	}
	if unix, err := strconv.ParseInt(str(1), 10, 64); err == nil {
		result.ExpiresAt = time.Unix(unix, 0)
	} else {
		return nil, errors.New("invalid cache: bad expires_at")
	}
	result.DailyUsage, _ = strconv.ParseFloat(str(2), 64)
	result.WeeklyUsage, _ = strconv.ParseFloat(str(3), 64)
	result.MonthlyUsage, _ = strconv.ParseFloat(str(4), 64)
	result.Version, _ = strconv.ParseInt(str(5), 10, 64)
	return result, nil
}

func (c *billingCache) SetSubscriptionCache(ctx context.Context, userID, planID int64, data *service.SubscriptionCacheData) error {
	if data == nil {
		return nil
	}

	key := billingSubKey(userID)

	fields := map[string]any{
		subField(planID, subFieldStatus):       data.Status,
		subField(planID, subFieldExpiresAt):    data.ExpiresAt.Unix(),
		subField(planID, subFieldDailyUsage):   data.DailyUsage,
		subField(planID, subFieldWeeklyUsage):  data.WeeklyUsage,
		subField(planID, subFieldMonthlyUsage): data.MonthlyUsage,
		subField(planID, subFieldVersion):      data.Version,
	}

	pipe := c.rdb.Pipeline()
	pipe.HSet(ctx, key, fields)
	pipe.HDel(ctx, key, legacySubFields...)
	pipe.Expire(ctx, key, jitteredTTL())
	_, err := pipe.Exec(ctx)
	return err
}

func (c *billingCache) UpdateSubscriptionUsage(ctx context.Context, userID, planID int64, cost float64) error {
	key := billingSubKey(userID)
	prefix := fmt.Sprintf("p%d:", planID)
	_, err := updateSubUsageScript.Run(ctx, c.rdb, []string{key}, cost, int(jitteredTTL().Seconds()), prefix).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		log.Printf("Warning: update subscription usage cache failed for user %d plan %d: %v", userID, planID, err)
		return err
	}
	return nil
}

func (c *billingCache) InvalidateSubscriptionCache(ctx context.Context, userID int64) error {
	key := billingSubKey(userID)
	return c.rdb.Del(ctx, key).Err()
}

func (c *billingCache) GetAPIKeyRateLimit(ctx context.Context, keyID int64) (*service.APIKeyRateLimitCacheData, error) {
	key := billingRateLimitKey(keyID)
	result, err := c.rdb.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, redis.Nil
	}
	data := &service.APIKeyRateLimitCacheData{}
	if v, ok := result[rateLimitFieldUsage5h]; ok {
		data.Usage5h, _ = strconv.ParseFloat(v, 64)
	}
	if v, ok := result[rateLimitFieldUsage1d]; ok {
		data.Usage1d, _ = strconv.ParseFloat(v, 64)
	}
	if v, ok := result[rateLimitFieldUsage7d]; ok {
		data.Usage7d, _ = strconv.ParseFloat(v, 64)
	}
	if v, ok := result[rateLimitFieldWindow5h]; ok {
		data.Window5h, _ = strconv.ParseInt(v, 10, 64)
	}
	if v, ok := result[rateLimitFieldWindow1d]; ok {
		data.Window1d, _ = strconv.ParseInt(v, 10, 64)
	}
	if v, ok := result[rateLimitFieldWindow7d]; ok {
		data.Window7d, _ = strconv.ParseInt(v, 10, 64)
	}
	return data, nil
}

func (c *billingCache) SetAPIKeyRateLimit(ctx context.Context, keyID int64, data *service.APIKeyRateLimitCacheData) error {
	if data == nil {
		return nil
	}
	key := billingRateLimitKey(keyID)
	fields := map[string]any{
		rateLimitFieldUsage5h:  data.Usage5h,
		rateLimitFieldUsage1d:  data.Usage1d,
		rateLimitFieldUsage7d:  data.Usage7d,
		rateLimitFieldWindow5h: data.Window5h,
		rateLimitFieldWindow1d: data.Window1d,
		rateLimitFieldWindow7d: data.Window7d,
	}
	pipe := c.rdb.Pipeline()
	pipe.HSet(ctx, key, fields)
	pipe.Expire(ctx, key, rateLimitCacheTTL)
	_, err := pipe.Exec(ctx)
	return err
}

func (c *billingCache) UpdateAPIKeyRateLimitUsage(ctx context.Context, keyID int64, cost float64) error {
	key := billingRateLimitKey(keyID)
	now := time.Now().Unix()
	_, err := updateRateLimitUsageScript.Run(ctx, c.rdb, []string{key},
		cost,
		int(rateLimitCacheTTL.Seconds()),
		now,
		int(rateLimitWindow5h.Seconds()),
		int(rateLimitWindow1d.Seconds()),
		int(rateLimitWindow7d.Seconds()),
	).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		log.Printf("Warning: update rate limit usage cache failed for api key %d: %v", keyID, err)
		return err
	}
	return nil
}

func (c *billingCache) InvalidateAPIKeyRateLimit(ctx context.Context, keyID int64) error {
	key := billingRateLimitKey(keyID)
	return c.rdb.Del(ctx, key).Err()
}
