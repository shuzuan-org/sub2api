package service

import (
	"net/http"
	"strings"
)

// 上游错误"直接透传"支持函数。
//
// 历史上网关把上游错误状态码翻译（塑形）成对外码（如上游 401/403→502、5xx→502）。
// 现改为"直接透传"：上游返回什么错误状态码，就原样返回给客户端，并带上从上游响应体
// 提取的 message。errType 按状态码派生，保证对外错误信封仍规范。
//
// 注意：只对"真·上游错误状态"透传；我们自己的内部错误（解析失败、客户端断连、
// 无上游状态的网络失败）仍保留各自原码，不走这里。

// ErrTypeForUpstreamStatus 按上游状态码派生对外 error.type。
func ErrTypeForUpstreamStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request_error"
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusForbidden:
		return "permission_error"
	case http.StatusNotFound:
		return "not_found_error"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	case 529:
		return "overloaded_error"
	default:
		return "api_error"
	}
}

// GenericUpstreamMsg 上游无可提取 message 时的通用兜底文案（按状态码）。
func GenericUpstreamMsg(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "Upstream authentication failed"
	case http.StatusForbidden:
		return "Upstream access forbidden"
	case http.StatusNotFound:
		return "Upstream resource not found"
	case http.StatusTooManyRequests:
		return "Upstream rate limit exceeded"
	case 529:
		return "Upstream service overloaded"
	default:
		if status >= 500 {
			return "Upstream service error"
		}
		return "Upstream request failed"
	}
}

// upstreamPassthroughDefaults 计算"无匹配透传规则时"的默认响应：
// 透传上游状态码 + 上游 message（提取失败则用通用文案）。
func upstreamPassthroughDefaults(upstreamStatus int, body []byte) (status int, errType, msg string) {
	status = upstreamStatus
	errType = ErrTypeForUpstreamStatus(upstreamStatus)
	msg = sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(body)))
	if msg == "" {
		msg = GenericUpstreamMsg(upstreamStatus)
	}
	return
}
