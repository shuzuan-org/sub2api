// Package metrics 提供进程级 Prometheus 指标，是稳定性建设"效果可观测"的基座。
//
// 设计原则：
//   - 自包含、零 wiring：其它包直接 import 本包并递增计数器即可，无需经过 wire。
//   - 默认 registry：go_goroutines / process_* 等运行时指标自动可得（DefaultRegisterer
//     已注册 Go + Process collector），promhttp.Handler() 直接暴露。
//   - 低基数：HTTP 指标按路由模板（gin FullPath）而非真实 URL 打标签，避免标签爆炸。
//
// 各业务项（流式截断 / 错误塑形 / tool 矫正 / 请求中断 / 零停机升级）通过本包定义的
// 计数器把"被容错默默吸收的事件"显式化，使改造前后可在 Grafana 对比。
package metrics

import (
	"database/sql"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"net/http"
)

var (
	// HTTPRequestsTotal 按路由模板 + 方法 + 状态码统计请求总数。
	HTTPRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sub2api_http_requests_total",
		Help: "Total HTTP requests by route template, method and status code.",
	}, []string{"path", "method", "code"})

	// HTTPRequestDuration 请求耗时分布（秒），用于算 P50/P95/P99。
	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "sub2api_http_request_duration_seconds",
		Help:    "HTTP request latency in seconds by route template and method.",
		Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300},
	}, []string{"path", "method"})

	// UpstreamErrorShapedTotal 网关把上游错误塑形后返回给客户端的次数（项3）。
	UpstreamErrorShapedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sub2api_upstream_error_shaped_total",
		Help: "Upstream errors shaped to client by returned status and error type.",
	}, []string{"status", "type"})

	// StreamTruncationTotal 流式中途截断次数，按成因区分（项2/5）。
	// cause: "upstream"（上游静默截断，补发 SSE error）| "client"（客户端主动断）。
	StreamTruncationTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sub2api_stream_truncation_total",
		Help: "Mid-stream truncations by cause (upstream|client).",
	}, []string{"cause"})

	// RequestInterruptedTotal 长任务在各阶段被中断的次数（项5）。
	// phase: "slotwait"|"stream"；cause: "client"|"shutdown"|"deadline"。
	RequestInterruptedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sub2api_request_interrupted_total",
		Help: "Long requests interrupted, by phase and cause.",
	}, []string{"phase", "cause"})

	// ToolCorrectionTotal tool-call 被容错矫正的次数（项4），让"容错消化盲区"可见。
	ToolCorrectionTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sub2api_tool_correction_total",
		Help: "Tool-call corrections applied, by correction kind (from->to).",
	}, []string{"kind"})

	// ToolErrorTotal tool-call 处理失败/被丢弃次数（项4）。
	ToolErrorTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sub2api_tool_error_total",
		Help: "Tool-call processing errors/drops by kind.",
	}, []string{"kind"})

	// DeployUpgradeTotal 零停机热升级（tableflip handoff）发生次数（项1）。
	DeployUpgradeTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "sub2api_deploy_upgrade_total",
		Help: "Number of zero-downtime upgrade handoffs (tableflip).",
	})

	// DrainDurationSeconds 优雅停机/升级交接时排空在途请求的耗时（项1）。
	DrainDurationSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "sub2api_drain_duration_seconds",
		Help:    "Time spent draining in-flight requests during shutdown/upgrade.",
		Buckets: []float64{0.1, 0.5, 1, 5, 15, 30, 60, 300, 1800, 7200},
	})
)

// tool_error_total 的已知 kind 常量（供产生点与预初始化共用，避免字符串漂移）。
const (
	ToolErrorEmptyName = "empty_tool_name" // 上游工具声明缺少名称，被跳过
)

// init 预初始化"固定 label 集"的 CounterVec series 为 0，使 Grafana 在尚未发生该类事件时
// 显示明确的 0 而非有歧义的 "No data"（带 label 的 CounterVec 在首次 Inc 前不会出现该 series）。
// 仅对 label 取值为有限常量集的指标这么做；status/type 等动态 label 不预置。
func init() {
	// 流式中途截断（项2/5）：上游静默截断 / 客户端主动断连。
	StreamTruncationTotal.WithLabelValues("upstream").Add(0)
	StreamTruncationTotal.WithLabelValues("client").Add(0)
	// 长任务在 slotwait 阶段被中断（项5）：客户端断连 / 等待超时。
	for _, cause := range []string{"client", "timeout"} {
		RequestInterruptedTotal.WithLabelValues("slotwait", cause).Add(0)
	}
	// 上游错误塑形（项3）：mapUpstreamError 的输出 (status,type) 是固定小集合，全部预置。
	for _, st := range [][2]string{
		{"502", "upstream_error"},
		{"429", "rate_limit_error"},
		{"503", "overloaded_error"},
		{"503", "upstream_error"},
	} {
		UpstreamErrorShapedTotal.WithLabelValues(st[0], st[1]).Add(0)
	}
}

// Handler 返回 /metrics 的 HTTP handler（基于默认 registry）。
func Handler() http.Handler { return promhttp.Handler() }

// PreInitToolErrorKinds 把已知的 tool_error kind 预置为 0，
// 使 Grafana 在"尚未发生任何 tool 错误"时显示明确的 0，而非有歧义的 No data。
func PreInitToolErrorKinds() {
	ToolErrorTotal.WithLabelValues(ToolErrorEmptyName).Add(0)
}

// dbStatsOnce 确保连接池 Gauge 只注册一次，避免测试反复构建 router 时在默认 registry 上重复注册 panic。
var dbStatsOnce sync.Once

// RegisterDBStats 注册一组随抓取动态读取 db.Stats() 的 Gauge，
// 暴露连接池水位（项0：发现连接池打满 / goroutine 泄漏的关键信号）。
// 幂等：多次调用只生效一次（以首个非空 db 为准）。
func RegisterDBStats(db *sql.DB) {
	if db == nil {
		return
	}
	dbStatsOnce.Do(func() { registerDBStats(db) })
}

func registerDBStats(db *sql.DB) {
	promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "sub2api_db_pool_open_connections",
		Help: "Current number of established DB connections (in use + idle).",
	}, func() float64 { return float64(db.Stats().OpenConnections) })
	promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "sub2api_db_pool_in_use",
		Help: "Number of DB connections currently in use.",
	}, func() float64 { return float64(db.Stats().InUse) })
	promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "sub2api_db_pool_idle",
		Help: "Number of idle DB connections.",
	}, func() float64 { return float64(db.Stats().Idle) })
	promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "sub2api_db_pool_wait_count",
		Help: "Total number of connections waited for (cumulative).",
	}, func() float64 { return float64(db.Stats().WaitCount) })
}

// ObserveHTTP 记录一次 HTTP 请求的计数与耗时。path 应为路由模板（gin FullPath），
// 为空时调用方应传入一个固定占位符以避免按真实 URL 打标签造成基数爆炸。
func ObserveHTTP(path, method string, status int, elapsed time.Duration) {
	code := strconv.Itoa(status)
	HTTPRequestsTotal.WithLabelValues(path, method, code).Inc()
	HTTPRequestDuration.WithLabelValues(path, method).Observe(elapsed.Seconds())
}
