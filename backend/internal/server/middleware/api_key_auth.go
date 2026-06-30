package middleware

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// NewAPIKeyAuthMiddleware 创建 API Key 认证中间件
func NewAPIKeyAuthMiddleware(apiKeyService *service.APIKeyService, subscriptionService *service.SubscriptionService, cfg *config.Config) APIKeyAuthMiddleware {
	return APIKeyAuthMiddleware(apiKeyAuthWithSubscription(apiKeyService, subscriptionService, nil, cfg))
}

func NewAPIKeyAuthMiddlewareWithOAuth(apiKeyService *service.APIKeyService, subscriptionService *service.SubscriptionService, oauthService *service.OAuthAuthorizationService, cfg *config.Config) APIKeyAuthMiddleware {
	return APIKeyAuthMiddleware(apiKeyAuthWithSubscription(apiKeyService, subscriptionService, oauthService, cfg))
}

// apiKeyAuthWithSubscription API Key认证中间件（支持订阅验证）
//
// 中间件职责分为两层：
//   - 鉴权（Authentication）：验证 Key 有效性、用户状态、IP 限制 —— 始终执行
//   - 计费执行（Billing Enforcement）：过期/配额/订阅/余额检查 —— skipBilling 时整块跳过
//
// /v1/usage 端点只需鉴权，不需要计费执行（允许过期/配额耗尽的 Key 查询自身用量）。
func apiKeyAuthWithSubscription(apiKeyService *service.APIKeyService, subscriptionService *service.SubscriptionService, oauthService *service.OAuthAuthorizationService, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// ── 1. 提取 API Key ──────────────────────────────────────────

		queryKey := strings.TrimSpace(c.Query("key"))
		queryApiKey := strings.TrimSpace(c.Query("api_key"))
		if queryKey != "" || queryApiKey != "" {
			AbortWithError(c, 400, "api_key_in_query_deprecated", "API key in query parameter is deprecated. Please use Authorization header instead.")
			return
		}

		// 尝试从Authorization header中提取API key (Bearer scheme)
		authHeader := c.GetHeader("Authorization")
		var apiKeyString string

		if authHeader != "" {
			// 验证Bearer scheme
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
				apiKeyString = strings.TrimSpace(parts[1])
			}
		}

		// 如果Authorization header中没有，尝试从x-api-key header中提取
		if apiKeyString == "" {
			apiKeyString = c.GetHeader("x-api-key")
		}

		// 如果x-api-key header中没有，尝试从x-goog-api-key header中提取（Gemini CLI兼容）
		if apiKeyString == "" {
			apiKeyString = c.GetHeader("x-goog-api-key")
		}

		// 如果所有header都没有API key
		if apiKeyString == "" {
			AbortWithError(c, 401, "API_KEY_REQUIRED", "API key is required in Authorization header (Bearer scheme), x-api-key header, or x-goog-api-key header")
			return
		}

		// ── 2. 验证 Bearer 凭证 ───────────────────────────────────────

		apiKey, claims, err := resolveBearerCredential(c, apiKeyService, oauthService, apiKeyString)
		if err != nil {
			if errors.Is(err, service.ErrOAuthProfileRequired) {
				AbortWithError(c, 400, "PROFILE_REQUIRED", "OAuth profile is required")
				return
			}
			if errors.Is(err, service.ErrOAuthProfileForbidden) {
				AbortWithError(c, 403, "PROFILE_FORBIDDEN", "OAuth profile is not available")
				return
			}
			if errors.Is(err, service.ErrAPIKeyNotFound) || errors.Is(err, service.ErrOAuthInvalidToken) {
				AbortWithError(c, 401, "INVALID_API_KEY", "Invalid API key")
				return
			}
			AbortWithError(c, 500, "INTERNAL_ERROR", "Failed to validate API key")
			return
		}
		if claims != nil {
			c.Set(string(ContextKeyOAuthClaims), claims)
		}

		if c.Request.URL.Path == "/v1/profiles" {
			if claims == nil {
				AbortWithError(c, 403, "PROFILES_REQUIRE_OAUTH", "Profiles are only available for OAuth credentials")
				return
			}
			c.Next()
			return
		}

		// ── 3. 基础鉴权（始终执行） ─────────────────────────────────

		// disabled / 未知状态 → 无条件拦截（expired 和 quota_exhausted 留给计费阶段）
		if !apiKey.IsActive() &&
			apiKey.Status != service.StatusAPIKeyExpired &&
			apiKey.Status != service.StatusAPIKeyQuotaExhausted {
			AbortWithError(c, 401, "API_KEY_DISABLED", "API key is disabled")
			return
		}

		// 检查 IP 限制（白名单/黑名单）
		// 注意：错误信息故意模糊，避免暴露具体的 IP 限制机制
		if len(apiKey.IPWhitelist) > 0 || len(apiKey.IPBlacklist) > 0 {
			clientIP := ip.GetTrustedClientIP(c)
			allowed, _ := ip.CheckIPRestrictionWithCompiledRules(clientIP, apiKey.CompiledIPWhitelist, apiKey.CompiledIPBlacklist)
			if !allowed {
				AbortWithError(c, 403, "ACCESS_DENIED", "Access denied")
				return
			}
		}

		// 检查关联的用户
		if apiKey.User == nil {
			AbortWithError(c, 401, "USER_NOT_FOUND", "User associated with API key not found")
			return
		}

		// 检查用户状态
		if !apiKey.User.IsActive() {
			AbortWithError(c, 401, "USER_INACTIVE", "User account is not active")
			return
		}

		// ── 4. SimpleMode → early return ─────────────────────────────

		if cfg.RunMode == config.RunModeSimple {
			c.Set(string(ContextKeyAPIKey), apiKey)
			c.Set(string(ContextKeyUser), AuthSubject{
				UserID:      apiKey.User.ID,
				Concurrency: apiKey.User.Concurrency,
			})
			c.Set(string(ContextKeyUserRole), apiKey.User.Role)
			setGroupContext(c, apiKey.Group)
			_ = apiKeyService.TouchLastUsed(c.Request.Context(), apiKey.ID)
			c.Next()
			return
		}

		// ── 5. 加载订阅（始终尝试加载用户的活跃订阅） ───────────────────────

		// skipBilling: 信息查询端点只需鉴权，不需要计费执行
		// （允许过期/配额耗尽的 Key 查询自身用量和分组信息）。
		skipBilling := c.Request.URL.Path == "/v1/usage" || c.Request.URL.Path == "/v1/group"

		// skipBalanceReject: 模型列表是只读发现端点，不消耗额度，余额不足也应可访问。
		// 与 skipBilling 不同，这里仍执行 Key 状态/过期/配额/分组可见性检查与订阅状态加载，
		// 只豁免末尾「无订阅且余额<=0」的硬拒绝，避免影响 callerRemaining 的订阅剩余展示。
		skipBalanceReject := c.Request.URL.Path == "/v1/models"

		var mergedState *service.MergedSubscriptionState

		if subscriptionService != nil {
			mergedState, _ = subscriptionService.GetMergedSubscriptionState(
				c.Request.Context(),
				apiKey.User.ID,
			)
		}

		// ── 6. 计费执行（skipBilling 时整块跳过） ────────────────────

		hasSubscription := false
		if !skipBilling {
			// Key 状态检查
			switch apiKey.Status {
			case service.StatusAPIKeyQuotaExhausted:
				AbortWithError(c, 429, "API_KEY_QUOTA_EXHAUSTED", "API key 额度已用完")
				return
			case service.StatusAPIKeyExpired:
				AbortWithError(c, 403, "API_KEY_EXPIRED", "API key 已过期")
				return
			}

			// 运行时过期/配额检查（即使状态是 active，也要检查时间和用量）
			if apiKey.IsExpired() {
				AbortWithError(c, 403, "API_KEY_EXPIRED", "API key 已过期")
				return
			}
			if apiKey.IsQuotaExhausted() {
				AbortWithError(c, 429, "API_KEY_QUOTA_EXHAUSTED", "API key 额度已用完")
				return
			}

			// 分组可见性校验（与计费解耦）：复用 CanBindGroup 的权威三档逻辑——
			// AllowedGroups（管理员指定）覆盖任何 visibility，其次 public，再次 subscriber
			// （需持有匹配 plan 的有效订阅）。可见性只关心订阅是否有效（未过期），不关心是否超限。
			// 必须在下方 ValidateMergedState 可能把 mergedState 置 nil 之前取活跃 plan 集合。
			if apiKey.Group != nil && apiKey.User != nil {
				if !apiKey.User.CanBindGroup(apiKey.Group.ID, apiKey.Group.Visibility, apiKey.Group.VisiblePlanIDs, mergedState.ActivePlanIDs()) {
					AbortWithError(c, 403, "SUBSCRIPTION_REQUIRED", "此分组仅对持有指定订阅的用户开放")
					return
				}
			}

			// 订阅模式：验证合并限额
			if mergedState != nil && mergedState.FIFOTarget() != nil {
				needsMaintenance, validateErr := subscriptionService.ValidateMergedState(mergedState)
				if validateErr != nil {
					// 订阅超限或其他错误（过期/暂停）→ 清除订阅让后续走余额扣费
					mergedState = nil
				} else {
					hasSubscription = true
					// 窗口维护异步化（不阻塞请求）
					if needsMaintenance {
						for i := range mergedState.FIFOQueue {
							subscriptionService.DoWindowMaintenance(&mergedState.FIFOQueue[i])
						}
					}
				}
			}

			// 无活跃订阅（或订阅超限 fallback）：检查余额
			// skipBalanceReject（/v1/models 等只读发现端点）豁免此硬拒绝。
			if !hasSubscription && !skipBalanceReject {
				if apiKey.User.Balance <= 0 {
					AbortWithError(c, 403, "INSUFFICIENT_BALANCE", "Insufficient account balance")
					return
				}
			}
		}

		// ── 7. 设置上下文 → Next ─────────────────────────────────────

		if hasSubscription && mergedState != nil {
			c.Set(string(ContextKeyMergedSubscription), mergedState)
			c.Header("X-Billing-Type", "subscription")
		} else if !skipBilling {
			c.Header("X-Billing-Type", "balance")
		}
		c.Set(string(ContextKeyAPIKey), apiKey)
		c.Set(string(ContextKeyUser), AuthSubject{
			UserID:      apiKey.User.ID,
			Concurrency: apiKey.User.Concurrency,
		})
		c.Set(string(ContextKeyUserRole), apiKey.User.Role)
		setGroupContext(c, apiKey.Group)
		_ = apiKeyService.TouchLastUsed(c.Request.Context(), apiKey.ID)

		c.Next()
	}
}

func resolveBearerCredential(c *gin.Context, apiKeyService *service.APIKeyService, oauthService *service.OAuthAuthorizationService, bearer string) (*service.APIKey, *service.OAuthAccessTokenClaims, error) {
	ctx := c.Request.Context()
	if strings.Count(bearer, ".") == 2 {
		if oauthService == nil {
			return nil, nil, service.ErrOAuthInvalidToken
		}
		claims, err := oauthService.ValidateOAuthAccessTokenContext(ctx, bearer)
		if err != nil {
			return nil, nil, err
		}
		if !containsScope(claims.Scope, service.MetacodeOAuthScope) {
			return nil, nil, service.ErrOAuthInvalidToken
		}
		if c.Request.URL.Path == "/v1/profiles" {
			return nil, claims, nil
		}
		profileID := strings.TrimSpace(c.GetHeader("X-Metacode-Profile-ID"))
		if profileID == "" {
			return nil, nil, service.ErrOAuthProfileRequired
		}
		apiKeyID, err := strconv.ParseInt(profileID, 10, 64)
		if err != nil || apiKeyID <= 0 {
			return nil, nil, service.ErrOAuthProfileForbidden
		}
		apiKey, err := apiKeyService.GetByID(ctx, apiKeyID)
		if err != nil {
			if errors.Is(err, service.ErrAPIKeyNotFound) {
				return nil, nil, service.ErrOAuthProfileForbidden
			}
			return nil, nil, err
		}
		if apiKey.UserID != claims.UserID {
			return nil, nil, service.ErrOAuthProfileForbidden
		}
		if apiKey.Group == nil || apiKey.GroupID == nil || !apiKey.Group.IsActive() {
			return nil, nil, service.ErrOAuthProfileForbidden
		}
		return apiKey, claims, nil
	}
	apiKey, err := apiKeyService.GetByKey(ctx, bearer)
	return apiKey, nil, err
}

func containsScope(scopes []string, target string) bool {
	for _, scope := range scopes {
		if scope == target {
			return true
		}
	}
	return false
}

func GetOAuthAccessTokenClaimsFromContext(c *gin.Context) (*service.OAuthAccessTokenClaims, bool) {
	value, exists := c.Get(string(ContextKeyOAuthClaims))
	if !exists {
		return nil, false
	}
	claims, ok := value.(*service.OAuthAccessTokenClaims)
	return claims, ok
}

// GetAPIKeyFromContext 从上下文中获取API key
func GetAPIKeyFromContext(c *gin.Context) (*service.APIKey, bool) {
	value, exists := c.Get(string(ContextKeyAPIKey))
	if !exists {
		return nil, false
	}
	apiKey, ok := value.(*service.APIKey)
	return apiKey, ok
}

// GetSubscriptionFromContext 从上下文中获取合并订阅的队首（最早过期的）订阅。
// 用于只需要单个"代表性订阅"的场景（状态展示、CheckBillingEligibility）。
// 如需完整 FIFO 队列（分账），请用 GetMergedStateFromContext。
func GetSubscriptionFromContext(c *gin.Context) (*service.UserSubscription, bool) {
	state, ok := GetMergedStateFromContext(c)
	if !ok || state == nil {
		return nil, false
	}
	sub := state.FIFOTarget()
	return sub, sub != nil
}

// GetMergedStateFromContext 从上下文中获取完整的合并订阅状态（含 FIFO 队列）。
func GetMergedStateFromContext(c *gin.Context) (*service.MergedSubscriptionState, bool) {
	value, exists := c.Get(string(ContextKeyMergedSubscription))
	if !exists {
		return nil, false
	}
	state, ok := value.(*service.MergedSubscriptionState)
	return state, ok
}

func setGroupContext(c *gin.Context, group *service.Group) {
	if !service.IsGroupContextValid(group) {
		return
	}
	if existing, ok := c.Request.Context().Value(ctxkey.Group).(*service.Group); ok && existing != nil && existing.ID == group.ID && service.IsGroupContextValid(existing) {
		return
	}
	ctx := context.WithValue(c.Request.Context(), ctxkey.Group, group)
	c.Request = c.Request.WithContext(ctx)
}
