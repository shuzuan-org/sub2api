package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/metrics"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

// SyntheticProbeService 端到端合成探针（对应 metask-ai 的 probe 作业）。
//
// 每轮对配置里的每个 target 并行发一次最小请求，路径与真实用户完全一致：
// API key 鉴权 → 分组 / 调度 → 账号 → 上游 → usage_logs 计量。请求结束后等
// metering_delay_seconds，再按探针 key 查本轮的 usage_logs，确认计量确实落库。
// 结果写成 sub2api_probe_* 指标，119 上的 Sub2apiProbe* 规则据此告警。
//
// 请求打 base_url（默认 127.0.0.1:8080），不经 nginx：公网入口由 blackbox 覆盖，
// 这里只回答"有没有一条能用的上游链路"。
type SyntheticProbeService struct {
	cfg        config.ProbeConfig
	apiKeys    probeAPIKeyLookup
	usageLogs  probeUsageReader
	httpClient *http.Client
	now        func() time.Time
	sleep      func(ctx context.Context, d time.Duration) bool

	keyIDMu sync.Mutex
	keyIDs  map[string]int64

	stopCh    chan struct{}
	stopOnce  sync.Once
	startOnce sync.Once
	wg        sync.WaitGroup
}

type probeAPIKeyLookup interface {
	GetByKey(ctx context.Context, key string) (*APIKey, error)
}

type probeUsageReader interface {
	ListByAPIKeyAndTimeRange(ctx context.Context, apiKeyID int64, startTime, endTime time.Time) ([]UsageLog, *pagination.PaginationResult, error)
}

const (
	probeUserAgent       = "sub2api-synthetic-probe/1.0"
	probeMaxResponseRead = 64 << 10
)

// NewSyntheticProbeService 创建探针；cfg 未启用时返回的实例 Start 为空操作。
func NewSyntheticProbeService(cfg config.ProbeConfig, apiKeys probeAPIKeyLookup, usageLogs probeUsageReader) *SyntheticProbeService {
	return &SyntheticProbeService{
		cfg:        cfg,
		apiKeys:    apiKeys,
		usageLogs:  usageLogs,
		httpClient: &http.Client{Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second},
		now:        time.Now,
		sleep:      sleepCtx,
		keyIDs:     map[string]int64{},
		stopCh:     make(chan struct{}),
	}
}

func (s *SyntheticProbeService) enabled() bool {
	return s != nil && s.cfg.Enabled && len(s.cfg.Targets) > 0
}

// Start 启动探测循环。首轮延迟一个 metering_delay，让进程先完成 listener 交接。
func (s *SyntheticProbeService) Start() {
	if !s.enabled() {
		return
	}
	s.startOnce.Do(func() {
		models := make([]string, 0, len(s.cfg.Targets))
		for _, t := range s.cfg.Targets {
			models = append(models, t.Model)
		}
		metrics.ResetProbeModels(models)

		ctx, cancel := context.WithCancel(context.Background())
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer cancel()
			go func() {
				<-s.stopCh
				cancel()
			}()
			if !s.sleep(ctx, 30*time.Second) {
				return
			}
			ticker := time.NewTicker(time.Duration(s.cfg.IntervalSeconds) * time.Second)
			defer ticker.Stop()
			for {
				s.RunOnce(ctx)
				select {
				case <-ticker.C:
				case <-ctx.Done():
					return
				}
			}
		}()
		logger.LegacyPrintf("service.synthetic_probe", "[SyntheticProbe] started: %d targets, every %ds", len(s.cfg.Targets), s.cfg.IntervalSeconds)
	})
}

// Stop 停止探测循环并等待在途一轮结束。
func (s *SyntheticProbeService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() { close(s.stopCh) })
	s.wg.Wait()
}

type probeAttempt struct {
	target config.ProbeTarget
	key    string
	result metrics.ProbeResult
	errMsg string
}

// RunOnce 跑一轮并写指标，返回各 target 的结果（测试用）。
func (s *SyntheticProbeService) RunOnce(ctx context.Context) []metrics.ProbeResult {
	if !s.enabled() {
		return nil
	}
	roundStart := s.now()

	attempts := make([]*probeAttempt, len(s.cfg.Targets))
	var wg sync.WaitGroup
	for i, t := range s.cfg.Targets {
		key := strings.TrimSpace(t.APIKey)
		if key == "" {
			key = strings.TrimSpace(s.cfg.APIKey)
		}
		a := &probeAttempt{target: t, key: key, result: metrics.ProbeResult{Model: t.Model}}
		attempts[i] = a
		wg.Add(1)
		go func() {
			defer wg.Done()
			if a.target.Disabled || s.targetDisabled(ctx, a.key) {
				a.result.Skipped = true
				logger.LegacyPrintf("service.synthetic_probe", "[SyntheticProbe] model=%s skipped: probe API key or group is disabled", a.target.Model)
				return
			}
			s.call(ctx, a)
		}()
	}
	wg.Wait()

	// 计量是异步写入（usage record worker pool），等一会儿再查。
	anyHTTPOK := false
	for _, a := range attempts {
		anyHTTPOK = anyHTTPOK || a.result.HTTPOK
	}
	if anyHTTPOK && s.cfg.MeteringDelaySeconds > 0 {
		if !s.sleep(ctx, time.Duration(s.cfg.MeteringDelaySeconds)*time.Second) {
			return nil
		}
	}
	s.checkMetering(ctx, attempts, roundStart.Add(-time.Second), s.now().Add(time.Second))

	results := make([]metrics.ProbeResult, len(attempts))
	for i, a := range attempts {
		results[i] = a.result
		if !a.result.Skipped && (!a.result.HTTPOK || !a.result.Metered) {
			logger.LegacyPrintf("service.synthetic_probe", "[SyntheticProbe] model=%s status=%d metered=%v latency=%s err=%s",
				a.target.Model, a.result.HTTPStatus, a.result.Metered, a.result.Latency.Round(time.Millisecond), a.errMsg)
		}
	}
	metrics.RecordProbeRound(results, s.now())
	return results
}

// targetDisabled prevents an intentionally disabled probe group from becoming a
// false production alert. A disabled target is exported as skipped=1 so it is
// visible in Prometheus while excluded from the failing alert.
func (s *SyntheticProbeService) targetDisabled(ctx context.Context, key string) bool {
	if s.apiKeys == nil || strings.TrimSpace(key) == "" {
		return false
	}
	k, err := s.apiKeys.GetByKey(ctx, key)
	if err != nil || k == nil {
		return false
	}
	if k.Status != "" && k.Status != StatusAPIKeyActive {
		return true
	}
	return k.Group != nil && !k.Group.IsActive()
}

func (s *SyntheticProbeService) call(ctx context.Context, a *probeAttempt) {
	path, body := probeRequest(a.target, s.cfg.MaxTokens)
	url := strings.TrimRight(s.cfg.BaseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		a.errMsg = err.Error()
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.key)
	req.Header.Set("User-Agent", probeUserAgent)
	if path == "/v1/messages" {
		req.Header.Set("anthropic-version", "2023-06-01")
	}

	start := s.now()
	resp, err := s.httpClient.Do(req)
	a.result.Latency = s.now().Sub(start)
	if err != nil {
		a.errMsg = err.Error()
		return
	}
	defer func() { _ = resp.Body.Close() }()
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, probeMaxResponseRead))
	a.result.HTTPStatus = resp.StatusCode
	a.result.HTTPOK = resp.StatusCode >= 200 && resp.StatusCode < 300
	if !a.result.HTTPOK {
		a.errMsg = truncateProbeBody(snippet)
	}
}

func (s *SyntheticProbeService) checkMetering(ctx context.Context, attempts []*probeAttempt, from, to time.Time) {
	if s.apiKeys == nil || s.usageLogs == nil {
		return
	}
	logsByKey := map[string][]UsageLog{}
	for _, a := range attempts {
		if !a.result.HTTPOK {
			continue
		}
		logs, ok := logsByKey[a.key]
		if !ok {
			id, err := s.keyID(ctx, a.key)
			if err != nil {
				a.errMsg = "lookup probe api key: " + err.Error()
				continue
			}
			logs, err = func() ([]UsageLog, error) {
				l, _, err := s.usageLogs.ListByAPIKeyAndTimeRange(ctx, id, from, to)
				return l, err
			}()
			if err != nil {
				a.errMsg = "query usage_logs: " + err.Error()
				continue
			}
			logsByKey[a.key] = logs
		}
		for _, l := range logs {
			if strings.EqualFold(l.RequestedModel, a.target.Model) || strings.EqualFold(l.Model, a.target.Model) {
				a.result.Metered = true
				break
			}
		}
		if !a.result.Metered {
			a.errMsg = "gateway returned 2xx but no usage_logs row was written"
		}
	}
}

func (s *SyntheticProbeService) keyID(ctx context.Context, key string) (int64, error) {
	s.keyIDMu.Lock()
	id, ok := s.keyIDs[key]
	s.keyIDMu.Unlock()
	if ok {
		return id, nil
	}
	k, err := s.apiKeys.GetByKey(ctx, key)
	if err != nil {
		return 0, err
	}
	if k == nil {
		return 0, fmt.Errorf("api key not found")
	}
	s.keyIDMu.Lock()
	s.keyIDs[key] = k.ID
	s.keyIDMu.Unlock()
	return k.ID, nil
}

func probeRequest(t config.ProbeTarget, maxTokens int) (string, []byte) {
	msg := []map[string]string{{"role": "user", "content": "ping"}}
	if t.Protocol == "openai" {
		b, _ := json.Marshal(map[string]any{"model": t.Model, "max_tokens": maxTokens, "messages": msg, "stream": false})
		return "/v1/chat/completions", b
	}
	b, _ := json.Marshal(map[string]any{"model": t.Model, "max_tokens": maxTokens, "messages": msg})
	return "/v1/messages", b
}

func truncateProbeBody(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
