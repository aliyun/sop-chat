package scheduler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"sop-chat/internal/config"
)

// SendToWebhook 根据 Webhook 类型将消息发送到对应平台（公开，供外部触发测试使用）
// 返回平台原始响应体（即使出错也尽量返回），以及错误信息
func SendToWebhook(cfg config.WebhookConfig, content string) (string, error) {
	return sendToWebhook(cfg, content)
}

// sendToWebhook 根据 Webhook 类型将消息发送到对应平台
func sendToWebhook(cfg config.WebhookConfig, content string) (string, error) {
	if cfg.URL == "" {
		return "", fmt.Errorf("webhook URL 未配置")
	}

	msgType := strings.ToLower(cfg.MsgType)
	if msgType == "" {
		msgType = "text"
	}

	var payload interface{}

	switch strings.ToLower(cfg.Type) {
	case "dingtalk":
		payload = buildDingTalkPayload(msgType, cfg.Title, content)
	case "feishu":
		payload = buildFeishuPayload(msgType, cfg.Title, content)
	case "wecom":
		payload = buildWeComPayload(msgType, content)
	case "email":
		return "", fmt.Errorf("邮件推送暂未开放")
	default:
		return "", fmt.Errorf("不支持的 webhook 类型: %q（支持：dingtalk、feishu、wecom）", cfg.Type)
	}

	raw, err := postJSON(cfg.URL, payload)
	if err != nil {
		return raw, err
	}

	// 校验平台应用层错误码（各平台 HTTP 200 但 body 中携带错误）
	if appErr := checkPlatformError(strings.ToLower(cfg.Type), raw); appErr != nil {
		return raw, appErr
	}

	return raw, nil
}

// checkPlatformError 解析平台响应体中的应用层错误码
func checkPlatformError(platform, raw string) error {
	if raw == "" {
		return nil
	}
	// 钉钉 / 企业微信：{"errcode":0,"errmsg":"ok"}
	// 飞书：{"code":0,"msg":"success"} 或 {"StatusCode":0,"StatusMessage":"success"}
	var resp struct {
		ErrCode    int    `json:"errcode"`
		ErrMsg     string `json:"errmsg"`
		Code       int    `json:"code"`
		Msg        string `json:"msg"`
		StatusCode int    `json:"StatusCode"`
		StatusMsg  string `json:"StatusMessage"`
	}
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil // 无法解析时不视为错误
	}
	switch platform {
	case "dingtalk", "wecom":
		if resp.ErrCode != 0 {
			return fmt.Errorf("errcode=%d errmsg=%q", resp.ErrCode, resp.ErrMsg)
		}
	case "feishu":
		if resp.Code != 0 {
			return fmt.Errorf("code=%d msg=%q", resp.Code, resp.Msg)
		}
		if resp.StatusCode != 0 {
			return fmt.Errorf("StatusCode=%d StatusMessage=%q", resp.StatusCode, resp.StatusMsg)
		}
	}
	return nil
}

// buildDingTalkPayload 构造钉钉机器人 Webhook 消息体
func buildDingTalkPayload(msgType, title, content string) interface{} {
	switch msgType {
	case "markdown":
		if title == "" {
			title = "通知"
		}
		return map[string]interface{}{
			"msgtype": "markdown",
			"markdown": map[string]string{
				"title": title,
				"text":  content,
			},
		}
	default: // text
		return map[string]interface{}{
			"msgtype": "text",
			"text": map[string]string{
				"content": content,
			},
		}
	}
}

// buildFeishuPayload 构造飞书机器人 Webhook 消息体
func buildFeishuPayload(msgType, title, content string) interface{} {
	switch msgType {
	case "markdown", "post":
		if title == "" {
			title = "通知"
		}
		return map[string]interface{}{
			"msg_type": "post",
			"content": map[string]interface{}{
				"post": map[string]interface{}{
					"zh_cn": map[string]interface{}{
						"title": title,
						"content": [][]map[string]string{
							{{"tag": "text", "text": content}},
						},
					},
				},
			},
		}
	default: // text
		return map[string]interface{}{
			"msg_type": "text",
			"content": map[string]string{
				"text": content,
			},
		}
	}
}

// buildWeComPayload 构造企业微信机器人 Webhook 消息体
func buildWeComPayload(msgType, content string) interface{} {
	switch msgType {
	case "markdown":
		return map[string]interface{}{
			"msgtype": "markdown",
			"markdown": map[string]string{
				"content": content,
			},
		}
	default: // text
		return map[string]interface{}{
			"msgtype": "text",
			"text": map[string]string{
				"content": content,
			},
		}
	}
}

// postJSON 向指定 URL 发送 JSON POST 请求，返回原始响应体和错误
func postJSON(url string, payload interface{}) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("序列化消息体失败: %w", err)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("发送 webhook 请求失败: %w", err)
	}
	defer resp.Body.Close()

	rawBytes, _ := io.ReadAll(resp.Body)
	raw := strings.TrimSpace(string(rawBytes))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return raw, fmt.Errorf("webhook 返回非成功状态码: %d, body: %s", resp.StatusCode, raw)
	}
	return raw, nil
}
