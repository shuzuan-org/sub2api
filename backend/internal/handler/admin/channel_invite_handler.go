package admin

import (
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// ChannelInviteHandler handles admin channel invite code management
type ChannelInviteHandler struct {
	channelInviteSvc *service.ChannelInviteService
}

// NewChannelInviteHandler creates a new admin channel invite handler
func NewChannelInviteHandler(channelInviteSvc *service.ChannelInviteService) *ChannelInviteHandler {
	return &ChannelInviteHandler{
		channelInviteSvc: channelInviteSvc,
	}
}

// ======================== 批次管理 ========================

// CreateBatchRequest creates a channel invite batch
type CreateBatchRequest struct {
	Name           string  `json:"name" binding:"required"`
	BonusAmount    float64 `json:"bonus_amount" binding:"required,min=0"`
	MaxUsesPerCode int     `json:"max_uses_per_code"`
	StartTime      *int64  `json:"start_time"` // unix timestamp seconds
	EndTime        *int64  `json:"end_time"`   // unix timestamp seconds
	Notes          string  `json:"notes"`
	CreatedBy      int64   `json:"created_by" binding:"required"`
	GroupIDs       []int64 `json:"group_ids"`
}

// UpdateBatchRequest updates a channel invite batch
type UpdateBatchRequest struct {
	Name           *string  `json:"name"`
	BonusAmount    *float64 `json:"bonus_amount" binding:"omitempty,min=0"`
	MaxUsesPerCode *int     `json:"max_uses_per_code" binding:"omitempty,min=0"`
	StartTime      *int64   `json:"start_time"`
	EndTime        *int64   `json:"end_time"`
	Status         *string  `json:"status" binding:"omitempty,oneof=active disabled"`
	Notes          *string  `json:"notes"`
	GroupIDs       []int64  `json:"group_ids"`
}

// GenerateCodesRequest generates invite codes in a batch
type GenerateCodesRequest struct {
	Count int `json:"count" binding:"required,min=1,max=500"`
}

// ListBatches GET /api/v1/admin/channel-invite/batches
func (h *ChannelInviteHandler) ListBatches(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	status := c.Query("status")
	search := strings.TrimSpace(c.Query("search"))
	if len(search) > 100 {
		search = search[:100]
	}

	params := pagination.PaginationParams{
		Page:     page,
		PageSize: pageSize,
	}

	batches, paginationResult, err := h.channelInviteSvc.ListBatches(c.Request.Context(), params, status, search)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := make([]dto.ChannelInviteBatch, 0, len(batches))
	for i := range batches {
		out = append(out, *dto.ChannelInviteBatchFromService(&batches[i]))
	}
	response.Paginated(c, out, paginationResult.Total, page, pageSize)
}

// GetBatch GET /api/v1/admin/channel-invite/batches/:id
func (h *ChannelInviteHandler) GetBatch(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid batch ID")
		return
	}

	batch, err := h.channelInviteSvc.GetBatch(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, dto.ChannelInviteBatchFromService(batch))
}

// CreateBatch POST /api/v1/admin/channel-invite/batches
func (h *ChannelInviteHandler) CreateBatch(c *gin.Context) {
	var req CreateBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	input := &service.CreateChannelInviteBatchInput{
		Name:           req.Name,
		BonusAmount:    req.BonusAmount,
		MaxUsesPerCode: req.MaxUsesPerCode,
		Notes:          req.Notes,
		CreatedBy:      req.CreatedBy,
		GroupIDs:       req.GroupIDs,
	}

	if req.StartTime != nil {
		t := time.Unix(*req.StartTime, 0)
		input.StartTime = &t
	}
	if req.EndTime != nil {
		t := time.Unix(*req.EndTime, 0)
		input.EndTime = &t
	}

	if input.MaxUsesPerCode <= 0 {
		input.MaxUsesPerCode = 1
	}

	batch, err := h.channelInviteSvc.CreateBatch(c.Request.Context(), input)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, dto.ChannelInviteBatchFromService(batch))
}

// UpdateBatch PUT /api/v1/admin/channel-invite/batches/:id
func (h *ChannelInviteHandler) UpdateBatch(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid batch ID")
		return
	}

	var req UpdateBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	input := &service.UpdateChannelInviteBatchInput{
		Name:           req.Name,
		BonusAmount:    req.BonusAmount,
		MaxUsesPerCode: req.MaxUsesPerCode,
		Status:         req.Status,
		Notes:          req.Notes,
		GroupIDs:       req.GroupIDs,
	}

	if req.StartTime != nil {
		t := time.Unix(*req.StartTime, 0)
		input.StartTime = &t
	}
	if req.EndTime != nil {
		t := time.Unix(*req.EndTime, 0)
		input.EndTime = &t
	}

	batch, err := h.channelInviteSvc.UpdateBatch(c.Request.Context(), id, input)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, dto.ChannelInviteBatchFromService(batch))
}

// DeleteBatch DELETE /api/v1/admin/channel-invite/batches/:id
func (h *ChannelInviteHandler) DeleteBatch(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid batch ID")
		return
	}

	if err := h.channelInviteSvc.DeleteBatch(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, gin.H{"message": "Batch deleted successfully"})
}

// GenerateCodes POST /api/v1/admin/channel-invite/batches/:id/generate-codes
func (h *ChannelInviteHandler) GenerateCodes(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid batch ID")
		return
	}

	var req GenerateCodesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	codes, err := h.channelInviteSvc.GenerateCodes(c.Request.Context(), id, req.Count)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := make([]dto.ChannelInviteCode, 0, len(codes))
	for i := range codes {
		out = append(out, *dto.ChannelInviteCodeFromService(&codes[i]))
	}
	response.Success(c, out)
}

// ListCodes GET /api/v1/admin/channel-invite/batches/:id/codes
func (h *ChannelInviteHandler) ListCodes(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid batch ID")
		return
	}

	page, pageSize := response.ParsePagination(c)
	status := c.Query("status")
	search := strings.TrimSpace(c.Query("search"))

	params := pagination.PaginationParams{
		Page:     page,
		PageSize: pageSize,
	}

	codes, paginationResult, err := h.channelInviteSvc.ListCodes(c.Request.Context(), id, params, status, search)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := make([]dto.ChannelInviteCode, 0, len(codes))
	for i := range codes {
		out = append(out, *dto.ChannelInviteCodeFromService(&codes[i]))
	}
	response.Paginated(c, out, paginationResult.Total, page, pageSize)
}

// ListUsages GET /api/v1/admin/channel-invite/batches/:id/usages
func (h *ChannelInviteHandler) ListUsages(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid batch ID")
		return
	}

	page, pageSize := response.ParsePagination(c)
	params := pagination.PaginationParams{
		Page:     page,
		PageSize: pageSize,
	}

	usages, paginationResult, err := h.channelInviteSvc.ListUsages(c.Request.Context(), id, params)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := make([]dto.ChannelInviteCodeUsage, 0, len(usages))
	for i := range usages {
		out = append(out, *dto.ChannelInviteCodeUsageFromService(&usages[i]))
	}
	response.Paginated(c, out, paginationResult.Total, page, pageSize)
}
