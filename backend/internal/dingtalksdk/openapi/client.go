// Package openapi 提供钉钉 OpenAPI 的 HTTP 客户端封装。
package openapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"sop-chat/internal/dingtalksdk/logger"
)

const apiBase = "https://api.dingtalk.com"

// Client 是钉钉 OpenAPI 的 HTTP 客户端，负责 access_token 的自动获取、缓存和刷新。
type Client struct {
	clientId     string
	clientSecret string
	httpClient   *http.Client

	mu          sync.Mutex
	accessToken string
	expireAt    time.Time
}

// NewClient 创建一个新的钉钉 OpenAPI 客户端。
func NewClient(clientId, clientSecret string) *Client {
	return &Client{
		clientId:     clientId,
		clientSecret: clientSecret,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// accessTokenResponse 是获取 access_token 接口的响应结构。
type accessTokenResponse struct {
	AccessToken string `json:"accessToken"`
	ExpireIn    int64  `json:"expireIn"`
}

// GetAccessToken 获取有效的 access_token，自动缓存并在过期前 5 分钟刷新。
func (c *Client) GetAccessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 缓存有效且距过期超过 5 分钟，直接返回
	if c.accessToken != "" && time.Now().Before(c.expireAt.Add(-5*time.Minute)) {
		return c.accessToken, nil
	}

	reqBody, err := json.Marshal(map[string]string{
		"appKey":    c.clientId,
		"appSecret": c.clientSecret,
	})
	if err != nil {
		return "", fmt.Errorf("序列化 access_token 请求体失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		apiBase+"/v1.0/oauth2/accessToken", bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("创建 access_token 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("获取 access_token 失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取 access_token 响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("获取 access_token 返回 %d: %s", resp.StatusCode, string(body))
	}

	var result accessTokenResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析 access_token 响应失败: %w", err)
	}

	if result.AccessToken == "" {
		return "", fmt.Errorf("获取 access_token 返回空值，响应: %s", string(body))
	}

	c.accessToken = result.AccessToken
	c.expireAt = time.Now().Add(time.Duration(result.ExpireIn) * time.Second)

	logger.GetLogger().Infof("钉钉 OpenAPI access_token 已刷新，有效期 %d 秒", result.ExpireIn)

	return c.accessToken, nil
}

// DoAPI 发送带 access_token 的通用 API 请求，返回响应体原始字节。
// method 为 HTTP 方法，path 为 API 路径（如 /v1.0/card/instances），reqBody 会被序列化为 JSON。
func (c *Client) DoAPI(ctx context.Context, method, path string, reqBody any) ([]byte, error) {
	token, err := c.GetAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	var bodyReader io.Reader
	if reqBody != nil {
		data, err := json.Marshal(reqBody)
		if err != nil {
			return nil, fmt.Errorf("序列化请求体失败: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, apiBase+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("创建 API 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-acs-dingtalk-access-token", token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API 请求失败 [%s %s]: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取 API 响应失败: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API 返回 %d [%s %s]: %s", resp.StatusCode, method, path, string(respBody))
	}

	return respBody, nil
}
