package service

import "time"

// APIKeyAuthSnapshot API Key 认证缓存快照（仅包含认证所需字段）
type APIKeyAuthSnapshot struct {
	APIKeyID    int64                    `json:"api_key_id"`
	UserID      int64                    `json:"user_id"`
	GroupID     *int64                   `json:"group_id,omitempty"`
	Status      string                   `json:"status"`
	IPWhitelist []string                 `json:"ip_whitelist,omitempty"`
	IPBlacklist []string                 `json:"ip_blacklist,omitempty"`
	User        APIKeyAuthUserSnapshot   `json:"user"`
	Group       *APIKeyAuthGroupSnapshot `json:"group,omitempty"`

	// Quota fields for API Key independent quota feature
	Quota     float64 `json:"quota"`      // Quota limit in USD (0 = unlimited)
	QuotaUsed float64 `json:"quota_used"` // Used quota amount

	// Expiration field for API Key expiration feature
	ExpiresAt *time.Time `json:"expires_at,omitempty"` // Expiration time (nil = never expires)

	// Rate limit configuration (only limits, not usage - usage read from Redis at check time)
	RateLimit5h float64 `json:"rate_limit_5h"`
	RateLimit1d float64 `json:"rate_limit_1d"`
	RateLimit7d float64 `json:"rate_limit_7d"`
}

// APIKeyAuthUserSnapshot 用户快照
type APIKeyAuthUserSnapshot struct {
	ID            int64   `json:"id"`
	Status        string  `json:"status"`
	Role          string  `json:"role"`
	Balance       float64 `json:"balance"`
	Concurrency   int     `json:"concurrency"`
	AllowedGroups []int64 `json:"allowed_groups,omitempty"`
}

// APIKeyAuthGroupSnapshot 分组快照
type APIKeyAuthGroupSnapshot struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Status   string `json:"status"`
	// Visibility 三档可见性（public/subscriber/private）。鉴权热路径据此判定 subscriber 分组校验，
	// 必须进缓存快照——否则缓存命中时该字段为空，subscriber 过期拒绝会被静默跳过。
	Visibility string `json:"visibility,omitempty"`
	// VisiblePlanIDs subscriber 分组绑定的订阅计划集合（仅 subscriber 档非空）。
	VisiblePlanIDs                  []int64  `json:"visible_plan_ids,omitempty"`
	RateMultiplier                  float64  `json:"rate_multiplier"`
	ImagePrice1K                    *float64 `json:"image_price_1k,omitempty"`
	ImagePrice2K                    *float64 `json:"image_price_2k,omitempty"`
	ImagePrice4K                    *float64 `json:"image_price_4k,omitempty"`
	SoraImagePrice360               *float64 `json:"sora_image_price_360,omitempty"`
	SoraImagePrice540               *float64 `json:"sora_image_price_540,omitempty"`
	SoraVideoPricePerRequest        *float64 `json:"sora_video_price_per_request,omitempty"`
	SoraVideoPricePerRequestHD      *float64 `json:"sora_video_price_per_request_hd,omitempty"`
	ClaudeCodeOnly                  bool     `json:"claude_code_only"`
	FallbackGroupID                 *int64   `json:"fallback_group_id,omitempty"`
	FallbackGroupIDOnInvalidRequest *int64   `json:"fallback_group_id_on_invalid_request,omitempty"`

	// Model routing is used by gateway account selection, so it must be part of auth cache snapshot.
	// Only anthropic groups use these fields; others may leave them empty.
	ModelRouting        map[string][]int64 `json:"model_routing,omitempty"`
	ModelRoutingEnabled bool               `json:"model_routing_enabled"`
	MCPXMLInject        bool               `json:"mcp_xml_inject"`

	// 支持的模型系列（仅 antigravity 平台使用）
	SupportedModelScopes []string `json:"supported_model_scopes,omitempty"`

	// OpenAI Messages 调度配置（仅 openai 平台使用）
	AllowMessagesDispatch bool   `json:"allow_messages_dispatch"`
	DefaultMappedModel    string `json:"default_mapped_model,omitempty"`
}

// APIKeyAuthCacheEntry 缓存条目，支持负缓存
type APIKeyAuthCacheEntry struct {
	NotFound bool                `json:"not_found"`
	Snapshot *APIKeyAuthSnapshot `json:"snapshot,omitempty"`
}
