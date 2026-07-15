package middleware

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/metrics"

	"github.com/gin-gonic/gin"
)

// Metrics 记录每个请求的计数与耗时到 Prometheus。
// 按路由模板（c.FullPath()）打标签以控制基数；/metrics 自身不计入，避免自测量噪声。
func Metrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.Path == "/metrics" {
			c.Next()
			return
		}
		start := time.Now()
		c.Next()
		path := c.FullPath()
		if path == "" {
			// 未命中任何路由（404 等）：用固定占位符，避免按真实 URL 打标签。
			path = "unmatched"
		}
		metrics.ObserveHTTP(path, c.Request.Method, c.Writer.Status(), time.Since(start))
	}
}
