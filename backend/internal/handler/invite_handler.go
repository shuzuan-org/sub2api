package handler

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// InviteHandler 处理「邀请好友」相关请求。
type InviteHandler struct {
	inviteService  *service.InviteService
	settingService *service.SettingService
}

// NewInviteHandler 创建 InviteHandler。
func NewInviteHandler(
	inviteService *service.InviteService,
	settingService *service.SettingService,
) *InviteHandler {
	return &InviteHandler{
		inviteService:  inviteService,
		settingService: settingService,
	}
}

type inviteStats struct {
	InvitedCount    int     `json:"invited_count"`
	RechargedCount  int     `json:"recharged_count"` // 占位：本期恒 0
	TotalCommission float64 `json:"total_commission"` // 占位：本期恒 0
	Withdrawable    float64 `json:"withdrawable"`     // 占位：本期恒 0
}

type inviteRecord struct {
	Email         string  `json:"email"`
	Nickname      string  `json:"nickname"`
	RegisteredAt  string  `json:"registered_at"`
	TotalRecharge float64 `json:"total_recharge"` // 占位：本期恒 0
	Status        string  `json:"status"`         // 占位：恒 "registered"
}

type inviteSummaryResponse struct {
	Code     string         `json:"code"`
	Link     string         `json:"link"`
	Stats    inviteStats    `json:"stats"`
	Records  []inviteRecord `json:"records"`
	Total    int            `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
}

// GetSummary 返回当前用户的邀请码、邀请链接、统计与邀请明细（分页）。
// GET /api/v1/invite/summary?page=&page_size=&search=
func (h *InviteHandler) GetSummary(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	ctx := c.Request.Context()

	code, err := h.inviteService.GetOrCreateCode(ctx, subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	page, pageSize := response.ParsePagination(c)
	search := strings.TrimSpace(c.Query("search"))

	records, total, err := h.inviteService.ListInvitees(ctx, subject.UserID, page, pageSize, search)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	base := strings.TrimRight(h.settingService.GetFrontendURL(ctx), "/")
	link := ""
	if base != "" {
		link = base + "/register?invite=" + code
	}

	out := inviteSummaryResponse{
		Code: code,
		Link: link,
		Stats: inviteStats{
			InvitedCount:    total,
			RechargedCount:  0,
			TotalCommission: 0,
			Withdrawable:    0,
		},
		Records:  make([]inviteRecord, 0, len(records)),
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}
	for _, r := range records {
		out.Records = append(out.Records, inviteRecord{
			Email:         r.Email,
			Nickname:      r.Username,
			RegisteredAt:  r.RegisteredAt.Format("2006-01-02 15:04:05"),
			TotalRecharge: r.TotalRecharge,
			Status:        r.Status,
		})
	}

	response.Success(c, out)
}
