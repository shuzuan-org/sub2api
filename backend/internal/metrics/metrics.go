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

	// UpstreamErrorShapedTotal 网关把上游错误返回给客户端的次数，按对外 status/type 统计。
	// 注：网关已改为"直接透传"上游错误状态码，此处 status 即透传后的上游原始码。
	UpstreamErrorShapedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sub2api_upstream_error_shaped_total",
		Help: "Upstream errors returned to client by returned (passthrough) status and error type.",
	}, []string{"status", "type"})

	// OpsErrorTotal 与 ops_error_logs 同源的错误聚合计数（低基数）。
	//
	// 在 OpsErrorLoggerMiddleware 完成分类、入队 ops_error_logs 之前同步递增，
	// 保证 Prometheus 聚合与 DB 明细一一对应、永不漂移；且因发生在异步入队之前，
	// 队列丢弃只影响 DB 明细完整性，不影响本指标的正确性。
	//
	// 标签全部为低基数枚举：
	//   - platform:         anthropic|openai|gemini|antigravity|sora|deepseek|""（分组平台）
	//   - model:            NormalizeModel 归一化后的模型名（如 claude-sonnet-4-5），未知模型归入 "__other__"
	//   - phase:            request|auth|routing|upstream|network|internal（classifyOpsPhase）
	//   - type:             normalizeOpsErrorType 的有界词表（10 类）
	//   - severity:         P1|P2|P3（classifyOpsSeverity）
	//   - business_limited: true|false（额度/并发/计费等业务限制，用于 SLO 中排除）
	// 高基数维度（upstream_status/account_id/request_id/message）一律不进指标，只留 ops_error_logs 供 PG 下钻。
	OpsErrorTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sub2api_ops_error_total",
		Help: "Classified errors (same source as ops_error_logs) by platform, model, phase, type, severity and business-limited flag.",
	}, []string{"platform", "model", "phase", "type", "severity", "business_limited"})

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

	// ToolErrorTotal tool-call 处理失败次数（项4）。
	ToolErrorTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sub2api_tool_error_total",
		Help: "Tool-call processing errors by kind.",
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

// init 预初始化 OpsErrorTotal 的关键序列为 0。
//
// Prometheus CounterVec 的带标签序列在首次 Inc 前不存在，会让 Grafana 面板
// 在"尚未出错"时显示 No data 而非 0，也会让 rate()/告警表达式缺基线。这里把
// 告警与 SLO 面板会引用的组合预置为 0（沿用本仓 stream/tool 指标的既有做法）。
// 仅覆盖高价值组合，不做全叉乘（避免制造大量恒零序列）。
//
// 注意：model 维度在 init 时未必已知（需在配置加载后通过 SetAllowedModels 注入），
// 因此 init 先用 placeholder "__init__" 占位。配置加载后由 BootstrapModelMetrics 补
// 注册真实模型维度的零值序列。
func init() {
	platforms := []string{"anthropic", "openai", "gemini", "antigravity", "sora"}
	severities := []string{"P1", "P2", "P3"}
	// 告警/看板主要关注的非业务限制错误类型。
	realErrorTypes := []string{"upstream_error", "rate_limit_error", "api_error", "authentication_error"}
	for _, p := range platforms {
		for _, s := range severities {
			for _, t := range realErrorTypes {
				OpsErrorTotal.WithLabelValues(p, "", "upstream", t, s, "false")
			}
		}
	}
	// 内部错误（我们自己的 bug）与鉴权阶段错误：单独预置，供对应告警读到 0 基线。
	// 这些错误的 model 标签置空（属于网关层面，与模型无关）。
	for _, s := range severities {
		OpsErrorTotal.WithLabelValues("", "", "internal", "api_error", s, "false")
		OpsErrorTotal.WithLabelValues("", "", "auth", "authentication_error", s, "false")
	}

	// 稳定性看板（项1/2/4/5）引用的低频事件指标：全部为有界小词表，预置 0
	// 避免"从未发生"与"没在采集"在面板上不可区分（No data vs 0）。
	for _, cause := range []string{"upstream", "client"} {
		StreamTruncationTotal.WithLabelValues(cause)
	}
	for _, phase := range []string{"slotwait", "stream"} {
		for _, cause := range []string{"client", "shutdown", "deadline"} {
			RequestInterruptedTotal.WithLabelValues(phase, cause)
		}
	}
	for _, kind := range []string{"apply_name_failed", "apply_args_failed"} {
		ToolErrorTotal.WithLabelValues(kind)
	}
	// 上游错误透传：按透传派生规则（ErrTypeForUpstreamStatus）预置常见状态码组合，
	// 另保留规则系统默认参数在用的 502/upstream_error。
	for status, errType := range map[string]string{
		"400": "invalid_request_error",
		"401": "authentication_error",
		"403": "permission_error",
		"404": "not_found_error",
		"429": "rate_limit_error",
		"500": "api_error",
		"502": "api_error",
		"503": "api_error",
		"529": "overloaded_error",
	} {
		UpstreamErrorShapedTotal.WithLabelValues(status, errType)
	}
	UpstreamErrorShapedTotal.WithLabelValues("502", "upstream_error")
}

// BootstrapModelMetrics 在配置加载后调用，用已知的模型白名单补注册 model 维度的零值序列。
//
// 调用时机：setup/router 层在 cfg 可用后、服务 Accept 前调用。
// 幂等：多次调用只补未预置的 (platform, model) 组合。
func BootstrapModelMetrics(models []string) {
	SetAllowedModels(models)

	platforms := []string{"anthropic", "openai", "gemini", "antigravity", "sora", "deepseek"}
	severities := []string{"P1", "P2", "P3"}
	realErrorTypes := []string{"upstream_error", "rate_limit_error", "api_error", "authentication_error"}
	for _, m := range models {
		m = NormalizeModel(m)
		if m == "" || m == "__other__" {
			continue
		}
		for _, p := range platforms {
			for _, s := range severities {
				for _, t := range realErrorTypes {
					OpsErrorTotal.WithLabelValues(p, m, "upstream", t, s, "false")
				}
			}
		}
	}
}

// Handler 返回 /metrics 的 HTTP handler（基于默认 registry）。
func Handler() http.Handler { return promhttp.Handler() }

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

// UpstreamStatusTotal 上游 HTTP 状态码分布，按平台+模型+上游状态码拆分。
//
// 标签全部为低基数枚举：
//   - platform:    anthropic|openai|gemini|antigravity|sora|deepseek|""
//   - model:       NormalizeModel 归一化后的模型名
//   - upstream_status: "200"|"400"|"401"|"403"|"429"|"500"|"502"|"503"|"other"
//
// 记录时机：每次上游 HTTP 往返完成后（无论成功/失败）。
var UpstreamStatusTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "sub2api_upstream_status_total",
	Help: "Upstream HTTP response status codes by platform, model and upstream status.",
}, []string{"platform", "model", "upstream_status"})

// UpstreamLatencySeconds 上游请求延迟分布（Histogram），按平台+模型+阶段拆分。
//
// 标签：
//   - platform:    anthropic|openai|gemini|antigravity|sora|deepseek|""
//   - model:       NormalizeModel 归一化后的模型名
//   - phase:       "ttft"（首 token 时间，仅流式请求）| "total"（完整往返时间）
//
// Buckets 覆盖从 0.1s 到 300s，适配 LLM 请求的典型延迟范围。
var UpstreamLatencySeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "sub2api_upstream_latency_seconds",
	Help:    "Upstream request latency in seconds by platform, model and phase (ttft|total).",
	Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120, 300},
}, []string{"platform", "model", "phase"})

// upstreamStatusBucket 将原始 HTTP 状态码归入有界桶，防止标签基数爆炸。
// 已知状态码原样返回，其余归入 "other"。
func upstreamStatusBucket(code int) string {
	switch code {
	case 200:
		return "200"
	case 400:
		return "400"
	case 401:
		return "401"
	case 403:
		return "403"
	case 429:
		return "429"
	case 500:
		return "500"
	case 502:
		return "502"
	case 503:
		return "503"
	default:
		if code >= 200 && code < 300 {
			return "2xx"
		}
		if code >= 400 && code < 500 {
			return "4xx"
		}
		if code >= 500 {
			return "5xx"
		}
		return "other"
	}
}

// RecordUpstreamStatus 记录一次上游 HTTP 响应的状态码。
// 应在每次上游往返完成后调用（无论成功/失败）。
// model 会经 NormalizeModel 归一化。
func RecordUpstreamStatus(platform, model string, statusCode int) {
	model = NormalizeModel(model)
	if model == "" {
		model = "__other__"
	}
	UpstreamStatusTotal.WithLabelValues(platform, model, upstreamStatusBucket(statusCode)).Inc()
}

// RecordUpstreamLatency 记录一次上游请求的延迟。
// phase 为 "ttft"（首 token 时间）或 "total"（完整往返时间）。
// durationMs <= 0 时跳过记录（如非流式请求的 ttft）。
// model 会经 NormalizeModel 归一化。
func RecordUpstreamLatency(platform, model, phase string, durationMs int64) {
	if durationMs <= 0 {
		return
	}
	model = NormalizeModel(model)
	if model == "" {
		model = "__other__"
	}
	UpstreamLatencySeconds.WithLabelValues(platform, model, phase).Observe(float64(durationMs) / 1000.0)
}

// LLMTokensTotal Token 消耗总量，按模型 + token 类型累加。
//
// 标签（全部低基数）：
//   - model: NormalizeModel 归一化后的模型名（白名单外归入 "__other__"）
//   - type:  input | output | cache_read | cache_creation
//
// 记录时机：每次 usage 落库收口（writeUsageLogBestEffort）时同步累加，
// 与 usage_logs 表同源；在落库前打点，因此不受异步写队列丢弃影响。
// 用 rate()/increase() 在 Grafana 观察每模型输入/输出 token 消耗速率。
var LLMTokensTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "sub2api_llm_tokens_total",
	Help: "LLM token consumption by model and token type (input|output|cache_read|cache_creation).",
}, []string{"model", "type"})

// RecordTokenUsage 累加一次请求的 token 消耗。
// model 经 NormalizeModel 归一化；各类别 <=0 时跳过（不产生无谓的零值序列）。
func RecordTokenUsage(model string, input, output, cacheRead, cacheCreation int) {
	model = NormalizeModel(model)
	if model == "" {
		model = "__other__"
	}
	if input > 0 {
		LLMTokensTotal.WithLabelValues(model, "input").Add(float64(input))
	}
	if output > 0 {
		LLMTokensTotal.WithLabelValues(model, "output").Add(float64(output))
	}
	if cacheRead > 0 {
		LLMTokensTotal.WithLabelValues(model, "cache_read").Add(float64(cacheRead))
	}
	if cacheCreation > 0 {
		LLMTokensTotal.WithLabelValues(model, "cache_creation").Add(float64(cacheCreation))
	}
}

// ---------------------------------------------------------------------------
// 账号池可用性指标（GaugeVec + Collector，每次 scrape 时从 accountRepo 重新计算）
// ---------------------------------------------------------------------------

// AccountPoolStat 描述一个 (platform, model) 组合的账号池状态。
type AccountPoolStat struct {
	Platform    string
	Model       string
	Total       int // 配置的总账号数（active + schedulable）
	Available   int // 当前可调度（IsSchedulable）的账号数
	Unavailable int // 不可调度的账号数（临时封禁/限流/过载等）
}

// AccountPoolStatsFunc is the callback that returns fresh account pool stats on each scrape.
type AccountPoolStatsFunc func() []AccountPoolStat

// accountPoolCollector implements prometheus.Collector for account pool gauges.
type accountPoolCollector struct {
	statsFunc AccountPoolStatsFunc

	availableDesc   *prometheus.Desc
	totalDesc       *prometheus.Desc
	unavailableDesc *prometheus.Desc
}

func newAccountPoolCollector(statsFunc AccountPoolStatsFunc) *accountPoolCollector {
	return &accountPoolCollector{
		statsFunc: statsFunc,
		availableDesc: prometheus.NewDesc(
			"sub2api_account_pool_available",
			"Number of schedulable (available) accounts by platform and model.",
			[]string{"platform", "model"}, nil,
		),
		totalDesc: prometheus.NewDesc(
			"sub2api_account_pool_total",
			"Total number of planned-in accounts (active and manually schedulable) by platform and model.",
			[]string{"platform", "model"}, nil,
		),
		unavailableDesc: prometheus.NewDesc(
			"sub2api_account_pool_unavailable",
			"Number of unavailable (blocked/rate-limited/overloaded) accounts by platform and model.",
			[]string{"platform", "model"}, nil,
		),
	}
}

func (c *accountPoolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.availableDesc
	ch <- c.totalDesc
	ch <- c.unavailableDesc
}

func (c *accountPoolCollector) Collect(ch chan<- prometheus.Metric) {
	stats := c.statsFunc()
	for _, s := range stats {
		model := NormalizeModel(s.Model)
		if model == "" || model == "__other__" {
			continue
		}
		ch <- prometheus.MustNewConstMetric(c.availableDesc, prometheus.GaugeValue, float64(s.Available), s.Platform, model)
		ch <- prometheus.MustNewConstMetric(c.totalDesc, prometheus.GaugeValue, float64(s.Total), s.Platform, model)
		ch <- prometheus.MustNewConstMetric(c.unavailableDesc, prometheus.GaugeValue, float64(s.Unavailable), s.Platform, model)
	}
}

// accountPoolOnce 确保 Collector 只注册一次。
var accountPoolOnce sync.Once

// RegisterAccountPoolGauges registers account pool gauges that call statsFunc on each scrape.
// statsFunc should return a snapshot of account counts grouped by (platform, model).
// Call once during server initialization.
func RegisterAccountPoolGauges(statsFunc AccountPoolStatsFunc) {
	if statsFunc == nil {
		return
	}
	accountPoolOnce.Do(func() {
		prometheus.MustRegister(newAccountPoolCollector(statsFunc))
	})
}

// ---------------------------------------------------------------------------
// HTTP 上游连接池水位（仿 DB 连接池，用 GaugeFunc 实现）
// ---------------------------------------------------------------------------

// UpstreamPoolStat 返回 HTTP 上游连接池的瞬时统计值。
type UpstreamPoolStat struct {
	ClientsCached int // 缓存的上游客户端实例数量
	InFlightTotal int // 总进行中请求数（跨所有客户端）
}

// UpstreamPoolStatsFunc is the callback that returns fresh upstream HTTP pool stats on each scrape.
type UpstreamPoolStatsFunc func() UpstreamPoolStat

var (
	upstreamPoolClientsCached prometheus.Gauge
	upstreamPoolInFlight      prometheus.Gauge
	upstreamPoolOnce          sync.Once
)

// RegisterUpstreamPoolGauges registers HTTP upstream connection pool gauges.
// Call once during server initialization.
func RegisterUpstreamPoolGauges(statsFunc UpstreamPoolStatsFunc) {
	if statsFunc == nil {
		return
	}
	upstreamPoolOnce.Do(func() {
		// Use prometheus.NewGauge (NOT promauto) to avoid auto-registration.
		// The collector below is the sole registration point.
		upstreamPoolClientsCached = prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "sub2api_upstream_pool_clients_cached",
			Help: "Number of cached HTTP upstream client instances.",
		})
		upstreamPoolInFlight = prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "sub2api_upstream_pool_in_flight",
			Help: "Total in-flight upstream HTTP requests across all cached clients.",
		})

		collector := &upstreamPoolCollector{
			statsFunc:   statsFunc,
			cachedGauge: upstreamPoolClientsCached,
			flightGauge: upstreamPoolInFlight,
		}
		prometheus.MustRegister(collector)
	})
}

type upstreamPoolCollector struct {
	statsFunc   UpstreamPoolStatsFunc
	cachedGauge prometheus.Gauge
	flightGauge prometheus.Gauge
}

func (c *upstreamPoolCollector) Describe(ch chan<- *prometheus.Desc) {
	c.cachedGauge.Describe(ch)
	c.flightGauge.Describe(ch)
}

func (c *upstreamPoolCollector) Collect(ch chan<- prometheus.Metric) {
	stats := c.statsFunc()
	c.cachedGauge.Set(float64(stats.ClientsCached))
	c.flightGauge.Set(float64(stats.InFlightTotal))
	c.cachedGauge.Collect(ch)
	c.flightGauge.Collect(ch)
}
