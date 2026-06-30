package routes

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/metrics"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// readinessProbeTimeout 限定 readiness 探测依赖的时间，避免依赖卡死时探测请求本身被拖住。
const readinessProbeTimeout = 2 * time.Second

// RegisterCommonRoutes 注册通用路由（存活/就绪检查、/metrics、状态等）。
//
// 健康检查分两种语义：
//   - /health（liveness）：进程是否活着。只要能响应即 200，供 systemd/容器判断是否需要重启。
//   - /ready（readiness）：依赖（DB/Redis）是否可用、能否处理请求。依赖挂掉时返回 503，
//     供 LB/Caddy 把"半死"实例摘掉流量而非继续打流量造成大面积报错。
func RegisterCommonRoutes(r *gin.Engine, sqlDB *sql.DB, redisClient *redis.Client) {
	// 存活检查（liveness）：保持轻量，不探依赖。
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// 就绪检查（readiness）：真实探测 DB 与 Redis 连通性。
	r.GET("/ready", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), readinessProbeTimeout)
		defer cancel()

		result := gin.H{"db": "ok", "redis": "ok"}
		ready := true

		if sqlDB != nil {
			if err := sqlDB.PingContext(ctx); err != nil {
				result["db"] = "down"
				ready = false
			}
		}
		if redisClient != nil {
			if err := redisClient.Ping(ctx).Err(); err != nil {
				result["redis"] = "down"
				ready = false
			}
		}

		if ready {
			result["status"] = "ready"
			c.JSON(http.StatusOK, result)
			return
		}
		result["status"] = "not_ready"
		c.JSON(http.StatusServiceUnavailable, result)
	})

	// Prometheus 指标端点（稳定性建设"效果可观测"的抓取入口）。
	r.GET("/metrics", gin.WrapH(metrics.Handler()))

	// Claude Code 遥测日志（忽略，直接返回200）
	r.POST("/api/event_logging/batch", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// Setup status endpoint (always returns needs_setup: false in normal mode)
	// This is used by the frontend to detect when the service has restarted after setup
	r.GET("/setup/status", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"data": gin.H{
				"needs_setup": false,
				"step":        "completed",
			},
		})
	})
}
