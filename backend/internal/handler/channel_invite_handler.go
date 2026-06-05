package handler

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

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
