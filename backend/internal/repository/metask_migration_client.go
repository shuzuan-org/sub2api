package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type metaskMigrationClient struct {
	httpClient *http.Client
}

// NewMetaskMigrationClient 调 metask-ai 入账接口的 HTTP 客户端。地址来自运维配置（可以是内网地址），不做解析地址校验。
func NewMetaskMigrationClient() service.MetaskMigrationClient {
	return &metaskMigrationClient{httpClient: &http.Client{Timeout: 20 * time.Second}}
}

func (c *metaskMigrationClient) Apply(ctx context.Context, baseURL, token string, payload *service.MetaskMigrationPayload) (json.RawMessage, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode payload: %w", err)
	}
	url := strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/api/internal/migrations/sub2api"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Metask-Migration-Token", token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		snippet := string(data)
		if len(snippet) > 300 {
			snippet = snippet[:300]
		}
		return nil, fmt.Errorf("metask-ai returned %d: %s", resp.StatusCode, snippet)
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("metask-ai returned invalid JSON")
	}
	return json.RawMessage(data), nil
}
