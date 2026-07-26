package service

import (
	"time"
)

// SubscriptionCacheData represents cached subscription data.
// 缓存按 (userID, planID) 维度存取（同一用户 hash 内按 plan 前缀分字段），
// 快照本身不再携带 PlanID —— 归属由存取键决定。
type SubscriptionCacheData struct {
	Status       string
	ExpiresAt    time.Time
	DailyUsage   float64
	WeeklyUsage  float64
	MonthlyUsage float64
	Version      int64
}
