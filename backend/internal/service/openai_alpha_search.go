package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// alpha/search — codex CLI 的 web.run 搜索执行端点。
//
// 新版 codex 不再把 hosted {type:web_search} 工具放进主 Responses 请求
// (gpt-5.6 全系模型元数据 use_responses_lite=true 时 hosted 路径被关闭)，
// 改为注册 web.run 函数工具；模型调用后 codex 客户端把 SearchRequest
// POST 到 {base_url}/alpha/search 并把 JSON 结果作为工具输出喂回模型
// (openai/codex codex-rs/codex-api/src/endpoint/search.rs, path="alpha/search")。
// 网关若缺这条路由，codex 的 web.run 调用会 404 无声失败。
//
// 请求体是演进中的非 Responses 协议 (id/model/input/commands/settings/...)，
// 按字节透传，不解析重建。响应为同步 JSON {encrypted_output,output,results[]}，
// 无 SSE。搜索响应不携带 token usage，故不进计费流水线；也不占用用户并发
// 槽（搜索子请求短促，且与其所属会话的主请求并发语义不同）。
const (
	chatgptAlphaSearchURL = "https://chatgpt.com/backend-api/codex/alpha/search"
	openaiAlphaSearchURL  = "https://api.openai.com/v1/alpha/search"

	alphaSearchMaxResponseBytes = 20 << 20
)

// buildOpenAIAlphaSearchURL 组装 alpha/search 端点，规则与
// buildOpenAIResponsesURL 对齐:
// - base 以 /alpha/search 结尾：原样返回
// - base 以 /responses 结尾（配置写了完整 responses 路径）：换成 /alpha/search
// - base 以 /v1 结尾：追加 /alpha/search
// - 其他情况：追加 /v1/alpha/search
func buildOpenAIAlphaSearchURL(base string) string {
	normalized := strings.TrimRight(strings.TrimSpace(base), "/")
	if strings.HasSuffix(normalized, "/alpha/search") {
		return normalized
	}
	if strings.HasSuffix(normalized, "/responses") {
		return strings.TrimSuffix(normalized, "/responses") + "/alpha/search"
	}
	if strings.HasSuffix(normalized, "/v1") {
		return normalized + "/alpha/search"
	}
	return normalized + "/v1/alpha/search"
}

// AlphaSearchResult 是上游 alpha/search 的原样响应。
type AlphaSearchResult struct {
	StatusCode  int
	Body        []byte
	ContentType string
}

// ForwardAlphaSearch 把 alpha/search 请求转发到账号对应的上游，字节透传。
// 上游非 200 也原样返回给客户端（codex 把失败渲染为工具错误，语义正确），
// 但 failover 级错误 (429/5xx 等) 仍喂给 rateLimitService 维护账号健康。
func (s *OpenAIGatewayService) ForwardAlphaSearch(ctx context.Context, c *gin.Context, account *Account, body []byte) (*AlphaSearchResult, error) {
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return nil, err
	}

	targetURL := openaiAlphaSearchURL
	switch account.Type {
	case AccountTypeOAuth:
		targetURL = chatgptAlphaSearchURL
	case AccountTypeAPIKey:
		if baseURL := account.GetOpenAIBaseURL(); baseURL != "" {
			validatedURL, err := s.validateUpstreamBaseURL(baseURL)
			if err != nil {
				return nil, err
			}
			targetURL = buildOpenAIAlphaSearchURL(validatedURL)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	// 透传客户端请求头（与 Responses passthrough 同一白名单），codex 会带
	// originator / x-codex-turn-metadata 等。
	allowTimeoutHeaders := s.isOpenAIPassthroughTimeoutHeadersAllowed()
	if c != nil && c.Request != nil {
		for key, values := range c.Request.Header {
			lower := strings.ToLower(strings.TrimSpace(key))
			if !isOpenAIPassthroughAllowedRequestHeader(lower, allowTimeoutHeaders) {
				continue
			}
			for _, v := range values {
				req.Header.Add(key, v)
			}
		}
	}

	// 覆盖入站鉴权残留，注入上游认证。
	req.Header.Del("authorization")
	req.Header.Del("x-api-key")
	req.Header.Set("authorization", "Bearer "+token)
	req.Header.Set("content-type", "application/json")

	if account.Type == AccountTypeOAuth {
		req.Host = "chatgpt.com"
		if chatgptAccountID := account.GetChatGPTAccountID(); chatgptAccountID != "" {
			req.Header.Set("chatgpt-account-id", chatgptAccountID)
		}
		if req.Header.Get("originator") == "" {
			req.Header.Set("originator", "codex_cli_rs")
		}
	}

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, alphaSearchMaxResponseBytes))
	if err != nil {
		return nil, err
	}

	if s.shouldFailoverUpstreamError(resp.StatusCode) {
		s.rateLimitService.HandleUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody)
	}

	return &AlphaSearchResult{
		StatusCode:  resp.StatusCode,
		Body:        respBody,
		ContentType: resp.Header.Get("Content-Type"),
	}, nil
}
