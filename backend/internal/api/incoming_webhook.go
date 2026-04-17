package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"sop-chat/internal/client"
	"sop-chat/internal/config"
	"sop-chat/internal/scheduler"
	"sop-chat/pkg/sopchat"

	cmsclient "github.com/alibabacloud-go/cms-20240330/v6/client"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/gin-gonic/gin"
)

const maxIncomingWebhookBodyBytes = 1 << 20 // 1 MiB

func (s *Server) handleIncomingWebhook(c *gin.Context) {
	ruleID := strings.TrimSpace(c.Param("id"))
	s.mu.RLock()
	globalCfg := s.globalConfig
	cmsCfg := s.config
	s.mu.RUnlock()
	if globalCfg == nil {
		writeIncomingWebhookFail(c, http.StatusServiceUnavailable, "config not loaded")
		return
	}

	webhookCfg := findIncomingWebhookConfig(globalCfg.IncomingWebhooks, ruleID)
	if webhookCfg == nil {
		writeIncomingWebhookFail(c, http.StatusNotFound, "incoming webhook not found")
		return
	}
	if !validateBearerToken(c.GetHeader("Authorization"), webhookCfg.BearerToken) {
		writeIncomingWebhookFail(c, http.StatusUnauthorized, "invalid bearer token")
		return
	}
	effectiveWebhooks := webhookCfg.EffectiveWebhooks()
	if strings.TrimSpace(webhookCfg.EmployeeName) == "" || strings.TrimSpace(webhookCfg.Prompt) == "" {
		writeIncomingWebhookFail(c, http.StatusInternalServerError, "incoming webhook config is incomplete")
		return
	}
	if len(effectiveWebhooks) == 0 && strings.TrimSpace(webhookCfg.CallbackURL) == "" {
		writeIncomingWebhookFail(c, http.StatusInternalServerError, "incoming webhook output target is empty")
		return
	}

	reader := http.MaxBytesReader(c.Writer, c.Request.Body, maxIncomingWebhookBodyBytes)
	bodyBytes, err := io.ReadAll(reader)
	if err != nil {
		writeIncomingWebhookFail(c, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	normalizedBody := strings.TrimSpace(string(bodyBytes))
	promptPayload := normalizedBody
	requestPayload := any(normalizedBody)
	if normalizedBody == "" {
		promptPayload = "{}"
		requestPayload = map[string]any{}
	} else {
		var decoded any
		if json.Unmarshal(bodyBytes, &decoded) == nil {
			if pretty, mErr := json.MarshalIndent(decoded, "", "  "); mErr == nil {
				promptPayload = string(pretty)
				requestPayload = decoded
			}
		}
	}

	taskProduct := config.ResolveScheduledTaskProduct(webhookCfg.Product, webhookCfg.Project, webhookCfg.Workspace, strings.TrimSpace(globalCfg.Global.Product))
	finalPrompt := buildIncomingWebhookPrompt(webhookCfg.Prompt, promptPayload, webhookCfg.ConciseReply)
	promptLog := scheduler.PromptForLog(finalPrompt, 500)
	log.Printf("[incoming-webhook] matched id=%q name=%q product=%q employee=%q payloadLen=%d prompt=%s",
		ruleID, webhookCfg.Name, taskProduct, webhookCfg.EmployeeName, len(bodyBytes), promptLog)

	if cmsCfg == nil || cmsCfg.AccessKeyId == "" {
		writeIncomingWebhookFail(c, http.StatusServiceUnavailable, "CMS credential not configured")
		return
	}

	cfgCopy := *webhookCfg
	language := globalCfg.GetLanguage()
	c.String(http.StatusOK, "success")

	go s.processIncomingWebhookAsync(s.lifecycleCtx, ruleID, &cfgCopy, cmsCfg, language, finalPrompt, taskProduct, requestPayload, effectiveWebhooks)
}

func (s *Server) processIncomingWebhookAsync(ctx context.Context, ruleID string, webhookCfg *config.IncomingWebhookConfig, cmsCfg *client.Config, language, finalPrompt, taskProduct string, requestPayload any, effectiveWebhooks []config.WebhookConfig) {
	reply, err := queryIncomingWebhookEmployee(ctx, cmsCfg, language, webhookCfg, finalPrompt, taskProduct)
	if err != nil {
		log.Printf("[incoming-webhook] query employee failed id=%q name=%q: %v", ruleID, webhookCfg.Name, err)
		return
	}

	webhookSent := false
	var webhookErrors []string
	for _, target := range effectiveWebhooks {
		if strings.TrimSpace(target.URL) == "" {
			continue
		}
		raw, sendErr := scheduler.SendToWebhook(target, reply)
		if sendErr != nil {
			webhookErrors = append(webhookErrors, fmt.Sprintf("[%s] %v", target.Type, sendErr))
			log.Printf("[incoming-webhook] send webhook failed name=%q type=%s: %v raw=%s", webhookCfg.Name, target.Type, sendErr, raw)
			continue
		}
		webhookSent = true
		log.Printf("[incoming-webhook] send webhook success name=%q type=%s raw=%s", webhookCfg.Name, target.Type, raw)
	}
	// 向后兼容旧配置：当未配置新 webhooks 时，继续使用 callbackUrl 作为通用 HTTP 回调。
	if len(effectiveWebhooks) == 0 && strings.TrimSpace(webhookCfg.CallbackURL) != "" {
		callbackPayload := map[string]any{
			"id":           ruleID,
			"name":         webhookCfg.Name,
			"employeeName": webhookCfg.EmployeeName,
			"product":      taskProduct,
			"receivedAt":   time.Now().Format(time.RFC3339),
			"request": map[string]any{
				"body": requestPayload,
			},
			"result": map[string]any{
				"reply": reply,
			},
		}
		rawResp, callbackErr := postWebhookJSON(webhookCfg.CallbackURL, webhookCfg.CallbackBearerToken, callbackPayload)
		if callbackErr != nil {
			webhookErrors = append(webhookErrors, callbackErr.Error())
			log.Printf("[incoming-webhook] legacy callback failed name=%q url=%s: %v raw=%s", webhookCfg.Name, webhookCfg.CallbackURL, callbackErr, rawResp)
		} else {
			webhookSent = true
			log.Printf("[incoming-webhook] legacy callback success name=%q url=%s raw=%s", webhookCfg.Name, webhookCfg.CallbackURL, rawResp)
		}
	}
	if !webhookSent && len(webhookErrors) > 0 {
		log.Printf("[incoming-webhook] all webhook notifications failed id=%q name=%q err=%s", ruleID, webhookCfg.Name, strings.Join(webhookErrors, "; "))
	}
}

func findIncomingWebhookConfig(configs []config.IncomingWebhookConfig, ruleID string) *config.IncomingWebhookConfig {
	id := normalizeIncomingWebhookID(ruleID)
	if id == "" {
		return nil
	}
	for i := range configs {
		cfg := &configs[i]
		if !cfg.Enabled {
			continue
		}
		if normalizeIncomingWebhookID(cfg.Name) == id {
			return cfg
		}
	}
	return nil
}

func normalizeIncomingWebhookID(s string) string {
	return strings.Trim(strings.TrimSpace(s), "/")
}

func writeIncomingWebhookFail(c *gin.Context, status int, message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		c.String(status, "fail")
		return
	}
	c.String(status, "fail: "+message)
}

func validateBearerToken(authHeader, expected string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return false
	}
	if !strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		return false
	}
	return strings.TrimSpace(authHeader[len("bearer "):]) == expected
}

func buildIncomingWebhookPrompt(basePrompt, payload string, concise bool) string {
	basePrompt = strings.TrimSpace(basePrompt)
	payload = strings.TrimSpace(payload)
	if payload == "" {
		payload = "{}"
	}
	if len([]rune(payload)) > 8000 {
		r := []rune(payload)
		payload = string(r[:8000]) + fmt.Sprintf("\n...(payload truncated, total %d chars)", len(r))
	}
	prompt := basePrompt + "\n\n以下是 webhook 请求内容（JSON）：\n" + payload
	if concise {
		prompt += "\n\n简化最终输出 适合聊天工具上阅读"
	}
	return prompt
}

// queryIncomingWebhookEmployee 使用统一 QueryEmployeeWithRetry 接口发起查询。
func queryIncomingWebhookEmployee(ctx context.Context, cmsCfg *client.Config, language string, wh *config.IncomingWebhookConfig, prompt, taskProduct string) (string, error) {
	sopClient, err := client.NewCMSClient(cmsCfg)
	if err != nil {
		return "", fmt.Errorf("create CMS client failed: %w", err)
	}
	threadTitle := fmt.Sprintf("[Webhook] %s @ %s", wh.Name, time.Now().Format("2006-01-02 15:04:05"))
	threadResp, err := sopClient.CreateThread(&sopchat.ThreadConfig{
		EmployeeName: wh.EmployeeName,
		Title:        threadTitle,
	})
	if err != nil {
		return "", fmt.Errorf("create thread failed: %w", err)
	}
	if threadResp.Body == nil || threadResp.Body.ThreadId == nil || *threadResp.Body.ThreadId == "" {
		return "", fmt.Errorf("create thread returned empty thread id")
	}
	threadID := *threadResp.Body.ThreadId
	now := time.Now()
	variables := map[string]interface{}{
		"timeStamp": fmt.Sprintf("%d", now.Unix()),
		"timeZone":  "Asia/Shanghai",
		"language":  language,
	}
	if config.IsSlsProduct(taskProduct) {
		variables["skill"] = "sop"
		if wh.Project != "" {
			variables["project"] = wh.Project
		}
	} else {
		if wh.Workspace != "" {
			variables["workspace"] = wh.Workspace
		}
		if wh.Region != "" {
			variables["region"] = wh.Region
		}
		variables["fromTime"] = now.Add(-15 * time.Minute).Unix()
		variables["toTime"] = now.Unix()
	}
	request := &cmsclient.CreateChatRequest{
		DigitalEmployeeName: tea.String(wh.EmployeeName),
		ThreadId:            tea.String(threadID),
		Action:              tea.String("create"),
		Messages: []*cmsclient.CreateChatRequestMessages{
			{
				Role: tea.String("user"),
				Contents: []*cmsclient.CreateChatRequestMessagesContents{
					{
						Type:  tea.String("text"),
						Value: tea.String(prompt),
					},
				},
			},
		},
		Variables: variables,
	}
	queryCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	result, err := sopchat.QueryEmployeeWithRetry(queryCtx, &sopchat.QueryEmployeeOptions{
		CMSClient: sopClient.CmsClient,
		Request:   request,
	})
	if err != nil {
		return "", err
	}
	return result.Text, nil
}

func postWebhookJSON(url, bearerToken string, payload any) (string, error) {
	if strings.TrimSpace(url) == "" {
		return "", fmt.Errorf("callbackUrl is empty")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload failed: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build callback request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token := strings.TrimSpace(bearerToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	httpClient := &http.Client{Timeout: 15 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("send callback request failed: %w", err)
	}
	defer resp.Body.Close()
	rawBytes, _ := io.ReadAll(resp.Body)
	raw := strings.TrimSpace(string(rawBytes))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return raw, fmt.Errorf("callback webhook returned status %d", resp.StatusCode)
	}
	return raw, nil
}
