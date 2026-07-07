package admin

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// userOperationsReporter 供 handler 依赖的窄接口（便于测试 stub）。
type userOperationsReporter interface {
	GetReport(ctx context.Context, account string, userID int64) (*service.UserOperationsLookupResult, error)
}

// UserOperationsHandler 管理端用户运营查询：按 email/username/user_id 输出
// 用户的邀请关系、充值金额与 token 消耗聚合报告。
// 注意与运维语义的 Ops* 系列 handler 无关。
type UserOperationsHandler struct {
	svc userOperationsReporter
}

// NewUserOperationsHandler 创建用户运营查询 handler。
func NewUserOperationsHandler(svc *service.UserOperationsService) *UserOperationsHandler {
	return &UserOperationsHandler{svc: svc}
}

// --- 端点专用响应 DTO ---

type operationsUserBrief struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

type operationsUserDetail struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	Balance   float64   `json:"balance"`
	CreatedAt time.Time `json:"created_at"`
}

type operationsInvitation struct {
	ReferralCode      *string               `json:"referral_code"` // null = 从未生成（懒创建）
	InviterID         *int64                `json:"inviter_id"`    // 原始 referred_by，邀请人已删除时仍保留
	Inviter           *operationsUserBrief  `json:"inviter"`       // null = 无邀请人或邀请人已删除
	InvitedCount      int64                 `json:"invited_count"`
	Invitees          []operationsUserBrief `json:"invitees"`
	InviteesTruncated bool                  `json:"invitees_truncated"`
}

type operationsAlipayRecharge struct {
	PaidOrderCount int64   `json:"paid_order_count"`
	CnyFeeTotal    int64   `json:"cny_fee_total"`    // 实付人民币合计，单位：分
	UsdAmountTotal float64 `json:"usd_amount_total"` // 到账 U 代币合计
}

type operationsRecharge struct {
	Alipay             operationsAlipayRecharge `json:"alipay"`
	RedeemBalanceTotal float64                  `json:"redeem_balance_total"`
	// CombinedUsdTotal = 支付宝到账 U + 兑换码余额（同为 U 代币；cny_fee 单位是分，不参与混算）
	CombinedUsdTotal float64 `json:"combined_usd_total"`
}

type operationsUsage struct {
	TotalRequests     int64   `json:"total_requests"`
	TotalInputTokens  int64   `json:"total_input_tokens"`
	TotalOutputTokens int64   `json:"total_output_tokens"`
	TotalCacheTokens  int64   `json:"total_cache_tokens"`
	TotalTokens       int64   `json:"total_tokens"`
	TotalCost         float64 `json:"total_cost"`
	TotalActualCost   float64 `json:"total_actual_cost"`
}

type operationsReport struct {
	User       operationsUserDetail `json:"user"`
	Invitation operationsInvitation `json:"invitation"`
	Recharge   operationsRecharge   `json:"recharge"`
	Usage      operationsUsage      `json:"usage"`
}

type operationsReportResponse struct {
	Matched    bool                  `json:"matched"`
	Report     *operationsReport     `json:"report,omitempty"`
	Candidates []operationsUserBrief `json:"candidates,omitempty"`
}

// GetReport handles the per-user operations report lookup
// GET /api/v1/admin/users/operations-report
// Query params:
//   - account: email 或 username（email 精确匹配优先；username 多命中时返回 candidates）
//   - user_id: 可选，直接按用户 ID 查询（提供时忽略 account）
func (h *UserOperationsHandler) GetReport(c *gin.Context) {
	var userID int64
	if raw := strings.TrimSpace(c.Query("user_id")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 {
			response.BadRequest(c, "Invalid user_id")
			return
		}
		userID = parsed
	}

	// 标准化和验证 account 参数（镜像 UserHandler.List 的 search 清洗）
	account := strings.TrimSpace(c.Query("account"))
	if runes := []rune(account); len(runes) > 100 {
		account = string(runes[:100])
	}

	if account == "" && userID == 0 {
		response.BadRequest(c, "account or user_id is required")
		return
	}

	result, err := h.svc.GetReport(c.Request.Context(), account, userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	if result.Report == nil {
		response.Success(c, operationsReportResponse{
			Matched:    false,
			Candidates: operationsBriefsFromService(result.Candidates),
		})
		return
	}

	response.Success(c, operationsReportResponse{
		Matched: true,
		Report:  operationsReportFromService(result.Report),
	})
}

func operationsBriefsFromService(in []service.UserOperationsBrief) []operationsUserBrief {
	out := make([]operationsUserBrief, 0, len(in))
	for _, b := range in {
		out = append(out, operationsUserBrief{
			ID:        b.ID,
			Email:     b.Email,
			Username:  b.Username,
			CreatedAt: b.CreatedAt,
		})
	}
	return out
}

func operationsReportFromService(r *service.UserOperationsReport) *operationsReport {
	report := &operationsReport{
		User: operationsUserDetail{
			ID:        r.User.ID,
			Email:     r.User.Email,
			Username:  r.User.Username,
			Role:      r.User.Role,
			Status:    r.User.Status,
			Balance:   r.User.Balance,
			CreatedAt: r.User.CreatedAt,
		},
		Invitation: operationsInvitation{
			ReferralCode:      r.ReferralCode,
			InviterID:         r.InviterID,
			InvitedCount:      r.InvitedCount,
			Invitees:          operationsBriefsFromService(r.Invitees),
			InviteesTruncated: r.InviteesTruncated,
		},
		Recharge: operationsRecharge{
			Alipay: operationsAlipayRecharge{
				PaidOrderCount: r.AlipayPaidCount,
				CnyFeeTotal:    r.AlipayCnyFeeTotal,
				UsdAmountTotal: r.AlipayUsdTotal,
			},
			RedeemBalanceTotal: r.RedeemBalanceTotal,
			CombinedUsdTotal:   r.AlipayUsdTotal + r.RedeemBalanceTotal,
		},
	}
	if r.Inviter != nil {
		report.Invitation.Inviter = &operationsUserBrief{
			ID:        r.Inviter.ID,
			Email:     r.Inviter.Email,
			Username:  r.Inviter.Username,
			CreatedAt: r.Inviter.CreatedAt,
		}
	}
	if r.Usage != nil {
		report.Usage = operationsUsage{
			TotalRequests:     r.Usage.TotalRequests,
			TotalInputTokens:  r.Usage.TotalInputTokens,
			TotalOutputTokens: r.Usage.TotalOutputTokens,
			TotalCacheTokens:  r.Usage.TotalCacheTokens,
			TotalTokens:       r.Usage.TotalTokens,
			TotalCost:         r.Usage.TotalCost,
			TotalActualCost:   r.Usage.TotalActualCost,
		}
	}
	return report
}
