package handler

import (
	"net/http"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// AlphaSearch handles codex CLI's web.run search execution requests.
// POST /v1/alpha/search
//
// codex 的 web-search 扩展在模型调用 web.run 工具后，把 SearchRequest POST
// 到 {base_url}/alpha/search 同步取回搜索结果。本 handler 按字节透传到
// OpenAI 平台账号的上游（细节见 service.ForwardAlphaSearch 的注释），上游
// 状态码/响应体原样回传——搜索失败由 codex 渲染为工具错误，网关不改写语义。
func (h *OpenAIGatewayHandler) AlphaSearch(c *gin.Context) {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.openai_gateway.alpha_search",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)

	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}
	if len(body) == 0 || !gjson.ValidBytes(body) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}

	// SearchRequest.model 是所属会话的模型，用于账号选择；
	// SearchRequest.id 是 codex 会话 id，作为 sticky session 锚，让搜索
	// 子请求尽量落在其会话主请求所在的账号上。
	reqModel := gjson.GetBytes(body, "model").String()
	sessionSeed := gjson.GetBytes(body, "id").String()
	sessionHash := h.gatewayService.GenerateSessionHashWithFallback(c, body, sessionSeed)
	reqLog = reqLog.With(zap.String("model", reqModel))

	account, err := h.gatewayService.SelectAccountForModel(c.Request.Context(), apiKey.GroupID, sessionHash, reqModel)
	if err != nil || account == nil {
		reqLog.Warn("alpha_search.account_select_failed", zap.Error(err))
		h.errorResponse(c, http.StatusServiceUnavailable, "overloaded_error", "No available accounts")
		return
	}
	reqLog = reqLog.With(zap.Int64("account_id", account.ID))

	result, err := h.gatewayService.ForwardAlphaSearch(c.Request.Context(), c, account, body)
	if err != nil {
		reqLog.Warn("alpha_search.forward_failed", zap.Error(err))
		h.errorResponse(c, http.StatusBadGateway, "api_error", "Upstream request failed")
		return
	}

	reqLog.Info("alpha_search.completed",
		zap.Int("upstream_status", result.StatusCode),
		zap.Int("response_bytes", len(result.Body)),
	)

	contentType := result.ContentType
	if contentType == "" {
		contentType = "application/json"
	}
	c.Data(result.StatusCode, contentType, result.Body)
}
