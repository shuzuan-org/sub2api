package service

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"
)

// AccountPricingSyncService 定期检测 Account 模型定价与 LiteLLM 的偏差，
// 当价格变动超过阈值时创建 OpsAlertEvent 提醒管理员确认。
type AccountPricingSyncService struct {
	accountRepo    AccountRepository
	pricingService *PricingService
	opsService     *OpsService
	timingWheel    *TimingWheelService
	interval       time.Duration
	threshold      float64 // 价格变动百分比阈值（如 0.05 = 5%）

	// 告警去重：记录已告警的 account:model，避免重复创建 firing 告警
	alerted sync.Map // key: "accountID:model" → value: time.Time (上次告警时间)
}

const (
	pricingSyncTaskName         = "pricing_sync:check"
	pricingSyncDefaultInterval  = 6 * time.Hour
	pricingSyncDefaultThreshold = 0.05 // 5%
)

// NewAccountPricingSyncService 创建价格同步服务
func NewAccountPricingSyncService(
	accountRepo AccountRepository,
	pricingService *PricingService,
	opsService *OpsService,
	timingWheel *TimingWheelService,
) *AccountPricingSyncService {
	return &AccountPricingSyncService{
		accountRepo:    accountRepo,
		pricingService: pricingService,
		opsService:     opsService,
		timingWheel:    timingWheel,
		interval:       pricingSyncDefaultInterval,
		threshold:      pricingSyncDefaultThreshold,
	}
}

// Start 注册定时任务
func (s *AccountPricingSyncService) Start() {
	if s.timingWheel == nil {
		log.Printf("[AccountPricingSync] TimingWheel not available, skipping")
		return
	}
	if s.pricingService == nil {
		log.Printf("[AccountPricingSync] PricingService not available, skipping")
		return
	}
	s.timingWheel.ScheduleRecurring(pricingSyncTaskName, s.interval, s.run)
	log.Printf("[AccountPricingSync] Started (interval: %v, threshold: %.0f%%)", s.interval, s.threshold*100)
}

// Stop 取消定时任务
func (s *AccountPricingSyncService) Stop() {
	if s.timingWheel != nil {
		s.timingWheel.Cancel(pricingSyncTaskName)
	}
}

func (s *AccountPricingSyncService) run() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	accounts, err := s.accountRepo.ListWithModelPricing(ctx)
	if err != nil {
		log.Printf("[AccountPricingSync] ListWithModelPricing failed: %v", err)
		return
	}

	var checked, alerts int
	for i := range accounts {
		acc := &accounts[i]
		raw, ok := acc.Extra["model_pricing"]
		if !ok || raw == nil {
			continue
		}
		allPricing, ok := raw.(map[string]any)
		if !ok || len(allPricing) == 0 {
			continue
		}

		for model := range allPricing {
			checked++
			s.checkModelPricing(ctx, acc, model, &alerts)
		}
	}

	if checked > 0 {
		log.Printf("[AccountPricingSync] Checked %d model-account pairs, %d alerts created", checked, alerts)
	}
}

func (s *AccountPricingSyncService) checkModelPricing(ctx context.Context, account *Account, model string, alerts *int) {
	accountPricing := account.GetModelPricingOverride(model)
	if accountPricing == nil {
		return
	}

	// 从 LiteLLM 获取最新价格
	litellmPricing := s.pricingService.GetModelPricing(strings.ToLower(model))
	if litellmPricing == nil {
		// LiteLLM 查不到 — 不算偏差，Account 覆盖价格就是权威
		return
	}

	// 比较关键价格维度
	inputDiff := priceDiffPct(accountPricing.InputPricePerToken, litellmPricing.InputCostPerToken)
	outputDiff := priceDiffPct(accountPricing.OutputPricePerToken, litellmPricing.OutputCostPerToken)

	dedupKey := fmt.Sprintf("%d:%s", account.ID, model)
	maxDiff := math.Max(inputDiff, outputDiff)
	if maxDiff < s.threshold {
		// 偏差回到阈值内，清除去重标记
		s.alerted.Delete(dedupKey)
		return
	}

	// 去重：同一 account+model 在一个同步周期内不重复告警
	if lastAlert, ok := s.alerted.Load(dedupKey); ok {
		if t, ok := lastAlert.(time.Time); ok && time.Since(t) < s.interval {
			return // 上次告警还在冷却期内
		}
	}

	// 价格偏差超阈值，创建告警
	now := time.Now()
	title := fmt.Sprintf("模型定价偏差: %s (Account #%d)", model, account.ID)
	desc := fmt.Sprintf(
		"Account「%s」(#%d) 的模型 %s 定价与 LiteLLM 偏差 %.1f%%。\n"+
			"Account input: %.8f U/token, LiteLLM input: %.8f U/token\n"+
			"Account output: %.8f U/token, LiteLLM output: %.8f U/token\n"+
			"请确认是否需要更新 Account 定价。",
		account.Name, account.ID, model, maxDiff*100,
		accountPricing.InputPricePerToken, litellmPricing.InputCostPerToken,
		accountPricing.OutputPricePerToken, litellmPricing.OutputCostPerToken,
	)

	event := &OpsAlertEvent{
		RuleID:         0, // 系统自动检测，不关联规则
		Severity:       "warning",
		Status:         OpsAlertStatusFiring,
		Title:          title,
		Description:    desc,
		MetricValue:    float64Ptr(maxDiff * 100),
		ThresholdValue: float64Ptr(s.threshold * 100),
		Dimensions: map[string]any{
			"account_id":   account.ID,
			"account_name": account.Name,
			"model":        model,
			"type":         "model_pricing_drift",
		},
		FiredAt:   now,
		CreatedAt: now,
	}

	if s.opsService != nil {
		if _, err := s.opsService.CreateAlertEvent(ctx, event); err != nil {
			log.Printf("[AccountPricingSync] CreateAlertEvent failed (account=%d model=%s): %v", account.ID, model, err)
			return
		}
		s.alerted.Store(dedupKey, time.Now())
		*alerts++
	}
}

// priceDiffPct 计算两个价格的百分比差异（相对于 reference）
func priceDiffPct(account, reference float64) float64 {
	if reference <= 0 {
		if account <= 0 {
			return 0
		}
		return 1.0 // 100% 差异
	}
	return math.Abs(account-reference) / reference
}
