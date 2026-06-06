package handler

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type oauthAuthorizeQuery struct {
	ClientID            string `form:"client_id" binding:"required"`
	RedirectURI         string `form:"redirect_uri" binding:"required"`
	ResponseType        string `form:"response_type" binding:"required"`
	Scope               string `form:"scope"`
	State               string `form:"state"`
	CodeChallenge       string `form:"code_challenge"`
	CodeChallengeMethod string `form:"code_challenge_method"`
}

type oauthAuthorizeConfirmRequest struct {
	ClientID            string `json:"client_id" binding:"required"`
	RedirectURI         string `json:"redirect_uri" binding:"required"`
	ResponseType        string `json:"response_type" binding:"required"`
	Scope               string `json:"scope"`
	State               string `json:"state"`
	CodeChallenge       string `json:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method"`
}

func (h *AuthHandler) OAuthAuthorizePreview(c *gin.Context) {
	if h.oauthAuthorizationService == nil {
		response.Error(c, http.StatusServiceUnavailable, "OAuth authorization service unavailable")
		return
	}
	var req oauthAuthorizeQuery
	if err := c.ShouldBindQuery(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	out, err := h.oauthAuthorizationService.PreviewAuthorization(c.Request.Context(), service.OAuthAuthorizeInput{
		ClientID:            req.ClientID,
		RedirectURI:         req.RedirectURI,
		ResponseType:        req.ResponseType,
		Scope:               req.Scope,
		State:               req.State,
		CodeChallenge:       req.CodeChallenge,
		CodeChallengeMethod: req.CodeChallengeMethod,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}

func (h *AuthHandler) OAuthAuthorizeConfirm(c *gin.Context) {
	if h.oauthAuthorizationService == nil {
		response.Error(c, http.StatusServiceUnavailable, "OAuth authorization service unavailable")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req oauthAuthorizeConfirmRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	out, err := h.oauthAuthorizationService.ApproveAuthorization(c.Request.Context(), subject.UserID, service.OAuthAuthorizeInput{
		ClientID:            req.ClientID,
		RedirectURI:         req.RedirectURI,
		ResponseType:        req.ResponseType,
		Scope:               req.Scope,
		State:               req.State,
		CodeChallenge:       req.CodeChallenge,
		CodeChallengeMethod: req.CodeChallengeMethod,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}

func (h *AuthHandler) OAuthAuthorizeDeny(c *gin.Context) {
	if h.oauthAuthorizationService == nil {
		response.Error(c, http.StatusServiceUnavailable, "OAuth authorization service unavailable")
		return
	}
	var req oauthAuthorizeConfirmRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	out, err := h.oauthAuthorizationService.DenyAuthorization(c.Request.Context(), service.OAuthAuthorizeInput{
		ClientID:            req.ClientID,
		RedirectURI:         req.RedirectURI,
		ResponseType:        req.ResponseType,
		Scope:               req.Scope,
		State:               req.State,
		CodeChallenge:       req.CodeChallenge,
		CodeChallengeMethod: req.CodeChallengeMethod,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}

func (h *AuthHandler) OAuthToken(c *gin.Context) {
	if h.oauthAuthorizationService == nil {
		response.Error(c, http.StatusServiceUnavailable, "OAuth authorization service unavailable")
		return
	}
	if err := c.Request.ParseForm(); err != nil {
		response.BadRequest(c, "Invalid form body")
		return
	}
	clientID, clientSecret, ok := c.Request.BasicAuth()
	if !ok {
		clientID = strings.TrimSpace(c.PostForm("client_id"))
		clientSecret = c.PostForm("client_secret")
	}
	out, err := h.oauthAuthorizationService.ExchangeAuthorizationCode(c.Request.Context(), service.OAuthTokenInput{
		GrantType:    c.PostForm("grant_type"),
		Code:         c.PostForm("code"),
		RedirectURI:  c.PostForm("redirect_uri"),
		ClientID:     clientID,
		ClientSecret: clientSecret,
		CodeVerifier: c.PostForm("code_verifier"),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}

func (h *AuthHandler) OAuthUserInfo(c *gin.Context) {
	if h.oauthAuthorizationService == nil {
		response.Error(c, http.StatusServiceUnavailable, "OAuth authorization service unavailable")
		return
	}
	authHeader := c.GetHeader("Authorization")
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		response.Unauthorized(c, "OAuth access token is required")
		return
	}
	out, err := h.oauthAuthorizationService.GetUserInfo(c.Request.Context(), strings.TrimSpace(parts[1]))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}
