package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// UserHandler handles user-related requests
type UserHandler struct {
	userService  *service.UserService
	phoneService *service.PhoneVerificationService
}

// NewUserHandler creates a new UserHandler
func NewUserHandler(userService *service.UserService, phoneService *service.PhoneVerificationService) *UserHandler {
	return &UserHandler{
		userService:  userService,
		phoneService: phoneService,
	}
}

// ChangePasswordRequest represents the change password request payload
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=6"`
}

// UpdateProfileRequest represents the update profile request payload
type UpdateProfileRequest struct {
	Username *string `json:"username"`
}

// GetProfile handles getting user profile
// GET /api/v1/users/me
func (h *UserHandler) GetProfile(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	userData, err := h.userService.GetByID(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, dto.UserFromService(userData))
}

// ChangePassword handles changing user password
// POST /api/v1/users/me/password
func (h *UserHandler) ChangePassword(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	svcReq := service.ChangePasswordRequest{
		CurrentPassword: req.OldPassword,
		NewPassword:     req.NewPassword,
	}
	err := h.userService.ChangePassword(c.Request.Context(), subject.UserID, svcReq)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, gin.H{"message": "Password changed successfully"})
}

// UpdateProfile handles updating user profile
// PUT /api/v1/users/me
func (h *UserHandler) UpdateProfile(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	var req UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	svcReq := service.UpdateProfileRequest{
		Username: req.Username,
	}
	updatedUser, err := h.userService.UpdateProfile(c.Request.Context(), subject.UserID, svcReq)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, dto.UserFromService(updatedUser))
}

// SendPhoneCodeRequest 发送手机验证码请求
type SendPhoneCodeRequest struct {
	PhoneNumber string `json:"phone_number" binding:"required"`
}

// SendPhoneCode handles sending SMS verification code
// POST /api/v1/user/phone/send-code
func (h *UserHandler) SendPhoneCode(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	var req SendPhoneCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	// 归一化手机号
	phone, err := service.NormalizePhoneNumber(req.PhoneNumber)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// 检查当前用户是否已绑定
	user, err := h.userService.GetByID(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if user.PhoneNumber != nil && *user.PhoneNumber != "" {
		response.ErrorFrom(c, service.ErrPhoneAlreadyBound)
		return
	}

	// 检查手机号是否已被其他用户绑定
	exists, err := h.userService.ExistsByPhoneNumber(c.Request.Context(), phone)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if exists {
		response.ErrorFrom(c, service.ErrPhoneNumberAlreadyBound)
		return
	}

	countdown, err := h.phoneService.SendVerifyCode(c.Request.Context(), phone)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, gin.H{
		"message":   "Verification code sent",
		"countdown": countdown,
	})
}

// BindPhoneRequest 绑定手机号请求
type BindPhoneRequest struct {
	PhoneNumber string `json:"phone_number" binding:"required"`
	VerifyCode  string `json:"verify_code" binding:"required,len=6"`
}

// BindPhone handles phone number binding with SMS verification
// POST /api/v1/user/phone/bind
func (h *UserHandler) BindPhone(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	var req BindPhoneRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	// 归一化手机号
	phone, err := service.NormalizePhoneNumber(req.PhoneNumber)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// 验证短信验证码
	if err := h.phoneService.VerifyCode(c.Request.Context(), phone, req.VerifyCode); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// 绑定手机号并赠送余额
	updatedUser, err := h.userService.BindPhoneAndGrantBonus(c.Request.Context(), subject.UserID, phone)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, dto.UserFromService(updatedUser))
}
