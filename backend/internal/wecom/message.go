package wecom

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	baseURL          = "https://qyapi.weixin.qq.com"
	tokenExpireEarly = 5 * time.Minute
)

// MessageManager 企业微信消息管理器
type MessageManager struct {
	corpID  string
	secret  string
	agentID int

	token       string
	tokenExpire time.Time
	tokenMu     sync.RWMutex

	client *http.Client
}

// NewMessageManager 创建消息管理器
func NewMessageManager(corpID, secret string, agentID int) *MessageManager {
	return &MessageManager{
		corpID:  corpID,
		secret:  secret,
		agentID: agentID,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// GetAccessToken 获取 access_token（带缓存）
func (m *MessageManager) GetAccessToken(ctx context.Context) (string, error) {
	m.tokenMu.RLock()
	if m.token != "" && time.Now().Before(m.tokenExpire) {
		token := m.token
		m.tokenMu.RUnlock()
		return token, nil
	}
	m.tokenMu.RUnlock()

	m.tokenMu.Lock()
	defer m.tokenMu.Unlock()

	if m.token != "" && time.Now().Before(m.tokenExpire) {
		return m.token, nil
	}

	url := fmt.Sprintf("%s/cgi-bin/gettoken?corpid=%s&corpsecret=%s", baseURL, m.corpID, m.secret)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %w", err)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求 access_token 失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %w", err)
	}

	var result struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}

	if result.ErrCode != 0 {
		return "", fmt.Errorf("获取 access_token 失败: code=%d, msg=%s", result.ErrCode, result.ErrMsg)
	}

	m.token = result.AccessToken
	m.tokenExpire = time.Now().Add(time.Duration(result.ExpiresIn)*time.Second - tokenExpireEarly)

	return m.token, nil
}

// SendTextToUser 发送文本消息给用户
func (m *MessageManager) SendTextToUser(ctx context.Context, userID, content string) (string, error) {
	return m.sendMessage(ctx, &sendMessageRequest{
		ToUser:  userID,
		MsgType: "text",
		AgentID: m.agentID,
		Text: &textContent{
			Content: content,
		},
	})
}

// SendMarkdownToUser 发送 Markdown 消息给用户
func (m *MessageManager) SendMarkdownToUser(ctx context.Context, userID, content string) (string, error) {
	return m.sendMessage(ctx, &sendMessageRequest{
		ToUser:  userID,
		MsgType: "markdown",
		AgentID: m.agentID,
		Markdown: &markdownContent{
			Content: content,
		},
	})
}
// SendWebhookText 通过群机器人 Webhook 发送文本消息
func SendWebhookText(ctx context.Context, webhookURL, content string) error {
	return sendWebhookMessage(ctx, webhookURL, &webhookMessageRequest{
		MsgType: "text",
		Text:    &textContent{Content: content},
	})
}

// SendWebhookMarkdown 通过群机器人 Webhook 发送 Markdown 消息
func SendWebhookMarkdown(ctx context.Context, webhookURL, content string) error {
	return sendWebhookMessage(ctx, webhookURL, &webhookMessageRequest{
		MsgType:  "markdown",
		Markdown: &markdownContent{Content: content},
	})
}

// sendWebhookMessage 发送 Webhook 消息（包级私有）
func sendWebhookMessage(ctx context.Context, webhookURL string, msg *webhookMessageRequest) error {
	bodyBytes, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("序列化 Webhook 消息失败: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("创建 Webhook 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("发送 Webhook 请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取 Webhook 响应失败: %w", err)
	}

	var result baseResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("解析 Webhook 响应失败: %w", err)
	}

	if result.ErrCode != 0 {
		return fmt.Errorf("Webhook 发送失败: code=%d, msg=%s", result.ErrCode, result.ErrMsg)
	}

	return nil
}

// sendMessage 发送应用消息
func (m *MessageManager) sendMessage(ctx context.Context, msg *sendMessageRequest) (string, error) {
	token, err := m.GetAccessToken(ctx)
	if err != nil {
		return "", fmt.Errorf("获取 access_token 失败: %w", err)
	}

	bodyBytes, err := json.Marshal(msg)
	if err != nil {
		return "", fmt.Errorf("序列化消息失败: %w", err)
	}

	url := fmt.Sprintf("%s/cgi-bin/message/send?access_token=%s", baseURL, token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %w", err)
	}

	var result sendMessageResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}

	if result.ErrCode != 0 {
		return "", fmt.Errorf("发送消息失败: code=%d, msg=%s", result.ErrCode, result.ErrMsg)
	}

	return result.MsgID, nil
}

// --- 请求/响应结构体 ---

type baseResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

type sendMessageRequest struct {
	ToUser   string           `json:"touser,omitempty"`
	ToParty  string           `json:"toparty,omitempty"`
	ToTag    string           `json:"totag,omitempty"`
	MsgType  string           `json:"msgtype"`
	AgentID  int              `json:"agentid"`
	Text     *textContent     `json:"text,omitempty"`
	Markdown *markdownContent `json:"markdown,omitempty"`
}

type sendMessageResponse struct {
	baseResponse
	InvalidUser  string `json:"invaliduser,omitempty"`
	InvalidParty string `json:"invalidparty,omitempty"`
	InvalidTag   string `json:"invalidtag,omitempty"`
	MsgID        string `json:"msgid,omitempty"`
}
type webhookMessageRequest struct {
	MsgType  string           `json:"msgtype"`
	Text     *textContent     `json:"text,omitempty"`
	Markdown *markdownContent `json:"markdown,omitempty"`
}

type textContent struct {
	Content string `json:"content"`
}

type markdownContent struct {
	Content string `json:"content"`
}

// ConvertMarkdownToWecom 将标准 Markdown 转换为企业微信支持的格式
func ConvertMarkdownToWecom(md string) string {
	return convertCodeBlocks(md)
}

// convertCodeBlocks 将代码块转换为引用格式（企业微信不支持代码块语法）
func convertCodeBlocks(md string) string {
	var result strings.Builder
	lines := strings.Split(md, "\n")
	inCodeBlock := false

	for _, line := range lines {
		if strings.HasPrefix(line, "```") {
			inCodeBlock = !inCodeBlock
			if inCodeBlock {
				result.WriteString("> 代码:\n")
			}
			continue
		}

		if inCodeBlock {
			result.WriteString("> ")
			result.WriteString(line)
			result.WriteString("\n")
		} else {
			result.WriteString(line)
			result.WriteString("\n")
		}
	}

	return strings.TrimSuffix(result.String(), "\n")
}

// truncateContent 截断内容到指定长度
func truncateContent(content string, maxLen int) string {
	runes := []rune(content)
	if len(runes) <= maxLen {
		return content
	}
	return string(runes[:maxLen-3]) + "..."
}

// TruncateContent 导出版本供外部使用
func TruncateContent(content string, maxLen int) string {
	return truncateContent(content, maxLen)
}
