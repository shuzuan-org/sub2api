package handler

import (
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// ValidateCodeResponse 公开校验邀请码响应
type ValidateCodeResponse struct {
	Valid         bool   `json:"valid"`
	Type          string `json:"type,omitempty"`           // "channel" | "friend"
	RemainingUses int    `json:"remaining_uses,omitempty"` // 渠道码剩余次数
	BatchStatus   string `json:"batch_status,omitempty"`   // 渠道活动状态
	Reason        string `json:"reason,omitempty"`         // 无效原因
}

// ChannelInviteHandler handles user-facing channel invite code endpoints
type ChannelInviteHandler struct {
	channelInviteSvc *service.ChannelInviteService
}

// NewChannelInviteHandler creates a new channel invite handler (user-facing)
func NewChannelInviteHandler(channelInviteSvc *service.ChannelInviteService) *ChannelInviteHandler {
	return &ChannelInviteHandler{
		channelInviteSvc: channelInviteSvc,
	}
}

// ClaimRequest is the request for claiming a channel invite code
type ClaimRequest struct {
	Code string `json:"code" binding:"required"`
}

// Claim POST /api/v1/channel-invite/claim
func (h *ChannelInviteHandler) Claim(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	var req ClaimRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	req.Code = strings.TrimSpace(req.Code)
	if req.Code == "" {
		response.BadRequest(c, "Code cannot be empty")
		return
	}

	if err := h.channelInviteSvc.ClaimCode(c.Request.Context(), subject.UserID, req.Code); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, gin.H{"message": "Invite code claimed successfully"})
}

// ---- 渠道合作方（码主）视角：/invite 页渠道邀请区块 ----
// 响应刻意精简，不暴露 notes/created_by/creator/groups 等 admin 专属字段。

type ownerBatchCode struct {
	Code      string `json:"code"`
	Status    string `json:"status"`
	MaxUses   int    `json:"max_uses"`
	UsedCount int    `json:"used_count"`
}

type ownerBatchSummary struct {
	ID               int64            `json:"id"`
	Name             string           `json:"name"`
	Status           string           `json:"status"`
	IsActive         bool             `json:"is_active"` // 综合 status 与时间窗
	BonusAmount      float64          `json:"bonus_amount"`
	StartTime        *string          `json:"start_time"`
	EndTime          *string          `json:"end_time"`
	ActivityCopyText string           `json:"activity_copy_text"`
	CodeCount        int              `json:"code_count"`
	UsedCount        int              `json:"used_count"`
	Codes            []ownerBatchCode `json:"codes"`
}

type ownerSummaryResponse struct {
	Batches []ownerBatchSummary `json:"batches"`
}

type ownerUsageRecord struct {
	ID           int64  `json:"id"`
	UserEmail    string `json:"user_email"`
	UserUsername string `json:"user_username"`
	ClaimedAt    string `json:"claimed_at"`
	BonusGranted bool   `json:"bonus_granted"`
}

const ownerTimeLayout = "2006-01-02 15:04:05"

func formatOwnerTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(ownerTimeLayout)
	return &s
}

// GetOwnerSummary 返回当前用户作为渠道合作方名下的全部渠道活动批次。
// GET /api/v1/channel-invite/summary
func (h *ChannelInviteHandler) GetOwnerSummary(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	batches, err := h.channelInviteSvc.GetOwnerSummary(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := ownerSummaryResponse{Batches: make([]ownerBatchSummary, 0, len(batches))}
	for i := range batches {
		b := &batches[i]
		codes := make([]ownerBatchCode, 0, len(b.Codes))
		for _, code := range b.Codes {
			codes = append(codes, ownerBatchCode{
				Code:      code.Code,
				Status:    code.Status,
				MaxUses:   code.MaxUses,
				UsedCount: code.UsedCount,
			})
		}
		out.Batches = append(out.Batches, ownerBatchSummary{
			ID:               b.ID,
			Name:             b.Name,
			Status:           b.Status,
			IsActive:         b.IsActive(),
			BonusAmount:      b.BonusAmount,
			StartTime:        formatOwnerTime(b.StartTime),
			EndTime:          formatOwnerTime(b.EndTime),
			ActivityCopyText: b.ActivityCopyText,
			CodeCount:        b.CodeCount,
			UsedCount:        b.UsedCount,
			Codes:            codes,
		})
	}

	response.Success(c, out)
}

// ListOwnerBatchUsages 渠道合作方分页查询自己批次的兑换（被邀请）记录。
// GET /api/v1/channel-invite/batches/:id/usages?page=&page_size=
func (h *ChannelInviteHandler) ListOwnerBatchUsages(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	batchID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || batchID <= 0 {
		response.BadRequest(c, "Invalid batch ID")
		return
	}

	page, pageSize := response.ParsePagination(c)
	params := pagination.PaginationParams{Page: page, PageSize: pageSize}

	usages, result, err := h.channelInviteSvc.ListOwnerBatchUsages(c.Request.Context(), subject.UserID, batchID, params)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := make([]ownerUsageRecord, 0, len(usages))
	for _, u := range usages {
		rec := ownerUsageRecord{
			ID:           u.ID,
			ClaimedAt:    u.ClaimedAt.Format(ownerTimeLayout),
			BonusGranted: u.BonusGranted,
		}
		if u.User != nil {
			rec.UserEmail = u.User.Email
			rec.UserUsername = u.User.Username
		}
		out = append(out, rec)
	}

	response.Paginated(c, out, result.Total, page, pageSize)
}

// ValidateCode GET /api/v1/invite/validate?code=XXXXXX
func (h *ChannelInviteHandler) ValidateCode(c *gin.Context) {
	code := strings.TrimSpace(c.Query("code"))
	if code == "" {
		response.BadRequest(c, "code is required")
		return
	}

	result := h.channelInviteSvc.ValidateCode(c.Request.Context(), code)
	response.Success(c, result)
}
