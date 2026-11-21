package middleware

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// MetaskMigrationTopupGuard 迁移开放期间拒绝充值、兑换与订阅购买：
// 冻结只在用户点击的那一刻读余额，之后再进旧站的钱不会被带到新站。
func MetaskMigrationTopupGuard(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		if cfg != nil && cfg.MetaskMigration.Active() {
			response.ErrorFrom(c, service.ErrMetaskMigrationClosed)
			c.Abort()
			return
		}
		c.Next()
	}
}

// MetaskMigrationGatewayStopped 截止后（metask_migration.gateway_stopped）拒绝全部 API 调用，
// 提示用户登录本站完成迁移。未停服时返回 false，调用方继续走原有链路。
func MetaskMigrationGatewayStopped(cfg *config.Config, c *gin.Context) bool {
	if cfg == nil || !cfg.MetaskMigration.GatewayStopped {
		return false
	}
	message := "本站已停止 API 服务。请登录本站完成迁移后，改用新的 base URL"
	if site := strings.TrimRight(cfg.MetaskMigration.GatewayBaseURL, "/"); site != "" {
		message += "（" + site + "）"
	}
	message += "。This service has stopped serving API calls; sign in to this site to migrate your account."
	if strings.Contains(c.Request.URL.Path, "/v1beta") {
		GoogleErrorWriter(c, http.StatusForbidden, message)
	} else {
		AnthropicErrorWriter(c, http.StatusForbidden, message)
	}
	c.Abort()
	return true
}
