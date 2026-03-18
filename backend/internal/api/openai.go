package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	cmsclient "github.com/alibabacloud-go/cms-20240330/v6/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/gin-gonic/gin"

	"sop-chat/pkg/sopchat"
)

// contentPart OpenAI content 数组中的单个部分
type contentPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ── OpenAI 兼容请求/响应结构 ──────────────────────────────────────────────────

// OpenAIMessage OpenAI 消息格式
// content 兼容字符串和数组两种格式：
//   - string: "Hello"
//   - array:  [{"type":"text","text":"Hello"}]
type OpenAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"-"` // 由 UnmarshalJSON 填充
}

func (m *OpenAIMessage) UnmarshalJSON(data []byte) error {
	// 用 alias 避免递归
	type alias struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	m.Role = a.Role

	if len(a.Content) == 0 {
		return nil
	}

	// 尝试解析为字符串
	var s string
	if err := json.Unmarshal(a.Content, &s); err == nil {
		m.Content = s
		return nil
	}

	// 尝试解析为数组（[{"type":"text","text":"..."}]）
	var parts []contentPart
	if err := json.Unmarshal(a.Content, &parts); err == nil {
		var sb strings.Builder
		for _, p := range parts {
			if p.Type == "text" {
				sb.WriteString(p.Text)
			}
		}
		m.Content = sb.String()
		return nil
	}

	return fmt.Errorf("unsupported content format: %s", string(a.Content))
}

func (m OpenAIMessage) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{Role: m.Role, Content: m.Content})
}

// OpenAIChatRequest OpenAI /v1/chat/completions 请求体
type OpenAIChatRequest struct {
	Model    string          `json:"model" binding:"required"`
	Messages []OpenAIMessage `json:"messages" binding:"required"`
	Stream   bool            `json:"stream"`
	// 扩展字段：指定现有会话线程，用于多轮对话
	ThreadID string `json:"thread_id"`
}

// OpenAIChoice 非流式响应中的 choice
type OpenAIChoice struct {
	Index        int           `json:"index"`
	Message      OpenAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

// OpenAIUsage token 用量（占位，实际不统计）
type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// OpenAIChatResponse 非流式响应体
type OpenAIChatResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []OpenAIChoice `json:"choices"`
	Usage   OpenAIUsage    `json:"usage"`
	// 扩展字段：返回实际使用的线程 ID，方便客户端复用
	ThreadID string `json:"thread_id,omitempty"`
}

// OpenAIStreamDelta 流式响应中的增量内容
type OpenAIStreamDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// OpenAIStreamChoice 流式响应中的 choice
type OpenAIStreamChoice struct {
	Index        int               `json:"index"`
	Delta        OpenAIStreamDelta `json:"delta"`
	FinishReason *string           `json:"finish_reason"`
}

// OpenAIStreamChunk 流式响应块
type OpenAIStreamChunk struct {
	ID       string               `json:"id"`
	Object   string               `json:"object"`
	Created  int64                `json:"created"`
	Model    string               `json:"model"`
	Choices  []OpenAIStreamChoice `json:"choices"`
	ThreadID string               `json:"thread_id,omitempty"` // 扩展字段，随最后一个 chunk 返回
}

// OpenAIModel /v1/models 返回的单个模型对象
type OpenAIModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// OpenAIModelList /v1/models 返回的模型列表
type OpenAIModelList struct {
	Object string        `json:"object"`
	Data   []OpenAIModel `json:"data"`
}

// ── 认证中间件 ────────────────────────────────────────────────────────────────

// openAIAuthMiddleware 验证 Authorization: Bearer <key> 格式的 API 密钥
func (s *Server) openAIAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 未配置或未启用时跳过认证（方便开发调试）
		if s.globalConfig == nil || s.globalConfig.OpenAI == nil || !s.globalConfig.OpenAI.Enabled || len(s.globalConfig.OpenAI.APIKeys) == 0 {
			c.Next()
			return
		}

		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"message": "Missing Authorization header",
					"type":    "invalid_request_error",
					"code":    "missing_api_key",
				},
			})
			return
		}

		const prefix = "Bearer "
		if !strings.HasPrefix(authHeader, prefix) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"message": "Authorization header must use Bearer scheme",
					"type":    "invalid_request_error",
					"code":    "invalid_api_key",
				},
			})
			return
		}

		token := authHeader[len(prefix):]
		for _, key := range s.globalConfig.OpenAI.APIKeys {
			if token == key {
				c.Next()
				return
			}
		}

		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{
				"message": "Invalid API key",
				"type":    "invalid_request_error",
				"code":    "invalid_api_key",
			},
		})
	}
}

// ── 路由处理器 ────────────────────────────────────────────────────────────────

// handleOpenAIListModels GET /v1/models
// 将每个数字员工映射为一个"模型"返回
func (s *Server) handleOpenAIListModels(c *gin.Context) {
	cmsClient, err := s.createClient()
	if err != nil {
		log.Printf("[OpenAI] 创建客户端失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"message": err.Error(), "type": "server_error"},
		})
		return
	}

	// 根据全局 product 配置决定列举哪类员工：SLS 用带 domain=sop 过滤的接口，CMS 列举全部
	var employeeNames []string
	if s.isSlsProduct() {
		list, err := cmsClient.ListEmployees()
		if err != nil {
			log.Printf("[OpenAI] 列举数字员工失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": gin.H{"message": err.Error(), "type": "server_error"},
			})
			return
		}
		for _, e := range list {
			if e.Name != nil {
				employeeNames = append(employeeNames, *e.Name)
			}
		}
	} else {
		list, err := cmsClient.ListAllEmployees()
		if err != nil {
			log.Printf("[OpenAI] 列举数字员工失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": gin.H{"message": err.Error(), "type": "server_error"},
			})
			return
		}
		for _, e := range list {
			if e.Name != nil {
				employeeNames = append(employeeNames, *e.Name)
			}
		}
	}

	models := make([]OpenAIModel, 0, len(employeeNames))
	for _, name := range employeeNames {
		models = append(models, OpenAIModel{
			ID:      name,
			Object:  "model",
			Created: time.Now().Unix(),
			OwnedBy: "organization",
		})
	}

	c.JSON(http.StatusOK, OpenAIModelList{
		Object: "list",
		Data:   models,
	})
}

// handleOpenAIChatCompletions POST /v1/chat/completions
// 将最后一条 user 消息发送给对应的数字员工，支持流式与非流式返回
func (s *Server) handleOpenAIChatCompletions(c *gin.Context) {
	var req OpenAIChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": err.Error(),
				"type":    "invalid_request_error",
				"code":    "invalid_request",
			},
		})
		return
	}

	// 提取最后一条用户消息
	userMessage := lastUserMessage(req.Messages)
	if userMessage == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "No user message found in messages array",
				"type":    "invalid_request_error",
				"code":    "invalid_request",
			},
		})
		return
	}

	employeeName := req.Model
	threadID := req.ThreadID

	// 如果没有提供 thread_id，自动创建新线程
	if threadID == "" {
		newThreadID, err := s.createOpenAIThread(employeeName)
		if err != nil {
			log.Printf("[OpenAI] 创建线程失败 employee=%s: %v", employeeName, err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": gin.H{"message": "Failed to create thread: " + err.Error(), "type": "server_error"},
			})
			return
		}
		threadID = newThreadID
	}

	// 根据全局 product 配置判断是否为 SLS 产品（需传 skill=sop）
	isSLS := s.isSlsProduct()

	if req.Stream {
		s.handleOpenAIStreamResponse(c, employeeName, threadID, userMessage, isSLS)
	} else {
		s.handleOpenAINonStreamResponse(c, employeeName, threadID, userMessage, isSLS)
	}
}

// createOpenAIThread 为 OpenAI 请求创建新线程
func (s *Server) createOpenAIThread(employeeName string) (string, error) {
	cmsClient, err := s.createClient()
	if err != nil {
		return "", fmt.Errorf("创建客户端失败: %w", err)
	}

	resp, err := cmsClient.CreateThread(&sopchat.ThreadConfig{
		EmployeeName: employeeName,
		Title:        fmt.Sprintf("OpenAI API: %s", time.Now().Format("2006-01-02 15:04:05")),
	})
	if err != nil {
		return "", fmt.Errorf("调用 CreateThread 失败: %w", err)
	}
	if resp.Body == nil || resp.Body.ThreadId == nil || *resp.Body.ThreadId == "" {
		return "", fmt.Errorf("CreateThread 返回了空的 ThreadId")
	}
	return *resp.Body.ThreadId, nil
}

// buildChatRequest 构造 CMS CreateChatRequest
// isSLS 为 true 时表示对接 SLS 产品的数字员工，需在 Variables 中附加 skill=sop
func (s *Server) buildChatRequest(employeeName, threadID, message string, isSLS bool) *cmsclient.CreateChatRequest {
	timeZone := "Asia/Shanghai"
	language := "zh"
	if s.globalConfig != nil {
		timeZone = s.globalConfig.GetTimeZone()
		language = s.globalConfig.GetLanguage()
	}

	variables := map[string]interface{}{
		"timeStamp": fmt.Sprintf("%d", time.Now().Unix()),
		"timeZone":  timeZone,
		"language":  language,
	}
	if isSLS {
		variables["skill"] = "sop"
	}

	return &cmsclient.CreateChatRequest{
		DigitalEmployeeName: tea.String(employeeName),
		ThreadId:            tea.String(threadID),
		Action:              tea.String("create"),
		Messages: []*cmsclient.CreateChatRequestMessages{
			{
				Role: tea.String("user"),
				Contents: []*cmsclient.CreateChatRequestMessagesContents{
					{
						Type:  tea.String("text"),
						Value: tea.String(message),
					},
				},
			},
		},
		Variables: variables,
	}
}

// handleOpenAIStreamResponse 流式模式：以 SSE 格式返回 OpenAI 兼容的 chunks
func (s *Server) handleOpenAIStreamResponse(c *gin.Context, employeeName, threadID, message string, isSLS bool) {
	cmsClient, err := s.createCMSClient()
	if err != nil {
		log.Printf("[OpenAI] 创建 CMS 客户端失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"message": err.Error(), "type": "server_error"},
		})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Transfer-Encoding", "chunked")
	// 允许 CORS 访问
	c.Header("X-Accel-Buffering", "no")

	clientGone := c.Writer.CloseNotify()

	completionID := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	created := time.Now().Unix()

	sendChunk := func(content string, finishReason *string, extThreadID string) {
		chunk := OpenAIStreamChunk{
			ID:       completionID,
			Object:   "chat.completion.chunk",
			Created:  created,
			Model:    employeeName,
			ThreadID: extThreadID,
			Choices: []OpenAIStreamChoice{
				{
					Index:        0,
					Delta:        OpenAIStreamDelta{Content: content},
					FinishReason: finishReason,
				},
			},
		}
		data, err := json.Marshal(chunk)
		if err != nil {
			return
		}
		c.Writer.Write([]byte("data: " + string(data) + "\n\n"))
		c.Writer.Flush()
	}

	// 发送角色 delta
	sendChunk("", nil, "")

	request := s.buildChatRequest(employeeName, threadID, message, isSLS)
	responseChan := make(chan *cmsclient.CreateChatResponse)
	errorChan := make(chan error)

	go cmsClient.CreateChatWithSSE(request, make(map[string]*string), &dara.RuntimeOptions{}, responseChan, errorChan)

	done := false
	for !done {
		select {
		case <-clientGone:
			log.Println("[OpenAI] 客户端断开连接")
			return

		case response, ok := <-responseChan:
			if !ok {
				done = true
				break
			}
			if response.Body == nil {
				continue
			}
			for _, msg := range response.Body.Messages {
				if msg == nil {
					continue
				}
				for _, content := range msg.Contents {
					if content == nil {
						continue
					}
					if t, ok := content["type"]; ok && t == "text" {
						if v, ok := content["value"]; ok {
							if s, ok := v.(string); ok && s != "" {
								sendChunk(s, nil, "")
							}
						}
					}
				}
			}

		case err, ok := <-errorChan:
			if ok && err != nil {
				log.Printf("[OpenAI] SSE 错误: %v", err)
				// 发送错误信息（尽力而为）
				errData, _ := json.Marshal(map[string]interface{}{
					"error": gin.H{"message": err.Error(), "type": "server_error"},
				})
				c.Writer.Write([]byte("data: " + string(errData) + "\n\n"))
				c.Writer.Flush()
				return
			}
			done = true
		}
	}

	// 发送结束 chunk，thread_id 随最后一个 chunk 一起返回
	finishReason := "stop"
	sendChunk("", &finishReason, threadID)

	c.Writer.Write([]byte("data: [DONE]\n\n"))
	c.Writer.Flush()

	log.Printf("[OpenAI] 流式响应完成 model=%s threadId=%s", employeeName, threadID)
}

// handleOpenAINonStreamResponse 非流式模式：收集完整回复后一次性返回
func (s *Server) handleOpenAINonStreamResponse(c *gin.Context, employeeName, threadID, message string, isSLS bool) {
	cmsClient, err := s.createCMSClient()
	if err != nil {
		log.Printf("[OpenAI] 创建 CMS 客户端失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"message": err.Error(), "type": "server_error"},
		})
		return
	}

	request := s.buildChatRequest(employeeName, threadID, message, isSLS)
	responseChan := make(chan *cmsclient.CreateChatResponse)
	errorChan := make(chan error)

	go cmsClient.CreateChatWithSSE(request, make(map[string]*string), &dara.RuntimeOptions{}, responseChan, errorChan)

	var textParts []string
	done := false
	for !done {
		select {
		case response, ok := <-responseChan:
			if !ok {
				done = true
				break
			}
			if response.Body == nil {
				continue
			}
			for _, msg := range response.Body.Messages {
				if msg == nil {
					continue
				}
				for _, content := range msg.Contents {
					if content == nil {
						continue
					}
					if t, ok := content["type"]; ok && t == "text" {
						if v, ok := content["value"]; ok {
							if s, ok := v.(string); ok && s != "" {
								textParts = append(textParts, s)
							}
						}
					}
				}
			}

		case err, ok := <-errorChan:
			if ok && err != nil {
				log.Printf("[OpenAI] 非流式错误: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": gin.H{"message": err.Error(), "type": "server_error"},
				})
				return
			}
			done = true
		}
	}

	fullText := strings.Join(textParts, "")
	completionID := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())

	resp := OpenAIChatResponse{
		ID:      completionID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   employeeName,
		Choices: []OpenAIChoice{
			{
				Index: 0,
				Message: OpenAIMessage{
					Role:    "assistant",
					Content: fullText,
				},
				FinishReason: "stop",
			},
		},
		Usage:    OpenAIUsage{},
		ThreadID: threadID,
	}

	log.Printf("[OpenAI] 非流式响应完成 model=%s threadId=%s contentLen=%d", employeeName, threadID, len(fullText))
	c.JSON(http.StatusOK, resp)
}

// lastUserMessage 从消息数组中提取最后一条 role=user 的消息内容
func lastUserMessage(messages []OpenAIMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return strings.TrimSpace(messages[i].Content)
		}
	}
	return ""
}
