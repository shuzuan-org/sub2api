package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// DeepSeekDefaultBaseURL is the upstream API endpoint used when the account
// does not configure base_url.
const DeepSeekDefaultBaseURL = "https://api.deepseek.com"

// DeepSeekDefaultModels lists the public DeepSeek models exposed by the
// upstream API. Kept in sync with the frontend whitelist in
// useModelWhitelist.ts so that the UI and backend agree on what is
// available out-of-the-box.
var DeepSeekDefaultModels = []string{
	"deepseek-chat",
	"deepseek-coder",
	"deepseek-reasoner",
	"deepseek-v3",
	"deepseek-v3-0324",
	"deepseek-r1",
	"deepseek-r1-0528",
}

// ForwardDeepSeekChatCompletions performs a passthrough of an OpenAI-compatible
// Chat Completions request to a DeepSeek upstream. The account must be a
// DeepSeek API-key account (Type=apikey, Platform=deepseek). Streaming and
// non-streaming requests are both supported — usage tokens are parsed from
// the response so that billing can run through the standard usage pipeline.
func (s *GatewayService) ForwardDeepSeekChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
) (*ForwardResult, error) {
	if account == nil || !account.IsDeepSeek() {
		return nil, fmt.Errorf("deepseek forward: account must be deepseek platform")
	}
	if account.Type != AccountTypeAPIKey {
		return nil, fmt.Errorf("deepseek forward: only apikey accounts are supported (got %s)", account.Type)
	}

	apiKey := account.GetDeepSeekAPIKey()
	if apiKey == "" {
		return nil, fmt.Errorf("deepseek forward: account missing api_key credential")
	}

	startTime := time.Now()

	originalModel := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	mappedModel := originalModel
	if mapped := account.GetMappedModel(originalModel); mapped != "" {
		mappedModel = mapped
	}
	if mappedModel != originalModel {
		body = s.replaceModelInBody(body, mappedModel)
	}
	reqStream := gjson.GetBytes(body, "stream").Bool()

	// Force usage reporting on streaming requests so we can bill accurately.
	if reqStream {
		body = ensureDeepSeekStreamIncludeUsage(body)
	}

	baseURL := account.GetDeepSeekBaseURL()
	targetURL := strings.TrimRight(baseURL, "/") + "/v1/chat/completions"

	upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("deepseek forward: build request: %w", err)
	}
	upstreamReq.Header.Set("Authorization", "Bearer "+apiKey)
	upstreamReq.Header.Set("Content-Type", "application/json")
	if reqStream {
		upstreamReq.Header.Set("Accept", "text/event-stream")
	} else {
		upstreamReq.Header.Set("Accept", "application/json")
	}

	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	resp, err := s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		return nil, fmt.Errorf("deepseek forward: send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		logger.L().Warn("deepseek forward: upstream error",
			zap.Int("status", resp.StatusCode),
			zap.Int64("account_id", account.ID),
			zap.String("model", originalModel),
			zap.ByteString("body", errBody),
		)
		c.Status(resp.StatusCode)
		copyDeepSeekResponseHeaders(c.Writer.Header(), resp.Header)
		_, _ = c.Writer.Write(errBody)
		return nil, &UpstreamFailoverError{
			StatusCode:      resp.StatusCode,
			ResponseBody:    errBody,
			ResponseHeaders: resp.Header,
		}
	}

	usage := ClaudeUsage{}
	var firstTokenMs *int

	c.Status(resp.StatusCode)
	copyDeepSeekResponseHeaders(c.Writer.Header(), resp.Header)

	if reqStream {
		// Stream pass-through with usage extraction from the final chunks.
		flusher, _ := c.Writer.(http.Flusher)
		reader := bufio.NewReader(resp.Body)
		firstByte := true
		buffer := make([]byte, 0, 64)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				if firstByte {
					ms := int(time.Since(startTime).Milliseconds())
					firstTokenMs = &ms
					firstByte = false
				}
				_, _ = c.Writer.Write(line)
				if flusher != nil {
					flusher.Flush()
				}
				if bytes.HasPrefix(line, []byte("data:")) {
					payload := bytes.TrimSpace(line[len("data:"):])
					if len(payload) > 0 && !bytes.Equal(payload, []byte("[DONE]")) {
						buffer = append(buffer[:0], payload...)
						if u := parseDeepSeekUsage(buffer); u != nil {
							usage = *u
						}
					}
				}
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					logger.L().Warn("deepseek forward: stream read error", zap.Error(err))
				}
				break
			}
		}
	} else {
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, fmt.Errorf("deepseek forward: read response: %w", readErr)
		}
		_, _ = c.Writer.Write(respBody)
		if u := parseDeepSeekUsage(respBody); u != nil {
			usage = *u
		}
		ms := int(time.Since(startTime).Milliseconds())
		firstTokenMs = &ms
	}

	upstreamModel := ""
	if mappedModel != originalModel {
		upstreamModel = mappedModel
	}

	return &ForwardResult{
		RequestID:     strings.TrimSpace(resp.Header.Get("x-request-id")),
		Usage:         usage,
		Model:         originalModel,
		UpstreamModel: upstreamModel,
		Stream:        reqStream,
		Duration:      time.Since(startTime),
		FirstTokenMs:  firstTokenMs,
	}, nil
}

// ensureDeepSeekStreamIncludeUsage adds `stream_options.include_usage=true`
// when the client requested streaming but did not opt-in to usage. This is
// the documented DeepSeek (and OpenAI) flag that causes the upstream to
// emit a final chunk containing token usage, which we need for billing.
func ensureDeepSeekStreamIncludeUsage(body []byte) []byte {
	if !gjson.ValidBytes(body) {
		return body
	}
	if gjson.GetBytes(body, "stream_options.include_usage").Bool() {
		return body
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	opts, _ := payload["stream_options"].(map[string]any)
	if opts == nil {
		opts = map[string]any{}
	}
	opts["include_usage"] = true
	payload["stream_options"] = opts
	updated, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return updated
}

// parseDeepSeekUsage extracts the `usage` block from a DeepSeek/OpenAI
// chat-completions response (either a non-streaming body or a single SSE
// data payload). Returns nil if usage isn't present.
func parseDeepSeekUsage(data []byte) *ClaudeUsage {
	usageBlock := gjson.GetBytes(data, "usage")
	if !usageBlock.Exists() {
		return nil
	}
	prompt := int(usageBlock.Get("prompt_tokens").Int())
	completion := int(usageBlock.Get("completion_tokens").Int())
	if prompt == 0 && completion == 0 {
		return nil
	}
	cacheRead := int(usageBlock.Get("prompt_cache_hit_tokens").Int())
	if cacheRead == 0 {
		// OpenAI-shape fallback
		cacheRead = int(usageBlock.Get("prompt_tokens_details.cached_tokens").Int())
	}
	return &ClaudeUsage{
		InputTokens:          prompt,
		OutputTokens:         completion,
		CacheReadInputTokens: cacheRead,
	}
}

// copyDeepSeekResponseHeaders mirrors safe upstream headers back to the
// client. Hop-by-hop headers and ones the gateway must control itself
// (e.g. content-length) are skipped.
func copyDeepSeekResponseHeaders(dst, src http.Header) {
	skip := map[string]struct{}{
		"connection":          {},
		"keep-alive":          {},
		"proxy-authenticate":  {},
		"proxy-authorization": {},
		"te":                  {},
		"trailers":            {},
		"transfer-encoding":   {},
		"upgrade":             {},
		"content-length":      {},
		"content-encoding":    {},
	}
	for k, values := range src {
		if _, ok := skip[strings.ToLower(k)]; ok {
			continue
		}
		for _, v := range values {
			dst.Add(k, v)
		}
	}
}
