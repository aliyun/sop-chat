package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	cmsclient "github.com/alibabacloud-go/cms-20240330/v6/client"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/gin-gonic/gin"

	"sop-chat/pkg/sopchat"
)

// ChatStreamRequest 聊天流式请求
type ChatStreamRequest struct {
	EmployeeName   string `json:"employeeName" binding:"required"`
	ThreadId       string `json:"threadId"`
	Message        string `json:"message" binding:"required"`
	CloudAccountID string `json:"cloudAccountId,omitempty"`
}

// handleChatStream 处理流式聊天请求 (SSE)
// 直接调用 SDK 的 SSE 接口，不做任何封装
func (s *Server) handleChatStream(c *gin.Context) {
	var req ChatStreamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  "Invalid request parameters",
			"detail": err.Error(),
		})
		return
	}

	var (
		cmsClient *cmsclient.Client
		err       error
	)
	runtimeCfg, options, err := s.resolveEmployeeRuntime(req.EmployeeName, req.CloudAccountID, req.Message)
	if err != nil {
		log.Printf("chat stream runtime unresolved: %v", err)
		statusCode := http.StatusInternalServerError
		payload := gin.H{
			"error":  "Failed to create client",
			"detail": err.Error(),
		}
		if len(options) > 0 {
			statusCode = http.StatusBadRequest
			payload["error"] = "目标环境不明确，请确认云账号"
			payload["needConfirm"] = true
			payload["options"] = options
		}
		c.JSON(statusCode, payload)
		return
	}

	cmsClient, err = buildRawCMSClient(runtimeCfg.ClientConfig)
	if err != nil {
		log.Printf("Failed to create client from resolved cloud account: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "Failed to create client",
			"detail": err.Error(),
		})
		return
	}

	// 设置 SSE 响应头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Transfer-Encoding", "chunked")

	// 创建 channel 用于控制
	clientGone := c.Writer.CloseNotify()

	// 发送 SSE JSON 消息的辅助函数
	sendSSEJSON := func(jsonData string) {
		c.Writer.Write([]byte(fmt.Sprintf("data: %s\n\n", jsonData)))
		c.Writer.Flush()
	}

	// 从配置中获取时区和语言设置，如果未配置则使用默认值
	timeZone := "Asia/Shanghai"
	language := "zh"
	if s.globalConfig != nil {
		timeZone = s.globalConfig.GetTimeZone()
		language = s.globalConfig.GetLanguage()
	}

	variables := buildEmployeeChatVariables(timeZone, language, runtimeCfg.Context)

	// 创建聊天请求
	request := &cmsclient.CreateChatRequest{
		DigitalEmployeeName: tea.String(req.EmployeeName),
		ThreadId:            tea.String(req.ThreadId),
		Action:              tea.String("create"),
		Messages: []*cmsclient.CreateChatRequestMessages{
			{
				Role: tea.String("user"),
				Contents: []*cmsclient.CreateChatRequestMessagesContents{
					{
						Type:  tea.String("text"),
						Value: tea.String(req.Message),
					},
				},
			},
		},
		Variables: variables,
	}

	// 创建 channel 用于接收 SSE 响应
	responseChan := make(chan *cmsclient.CreateChatResponse)
	errorChan := make(chan error)

	// 使用带 Context 的 SSE 调用，支持客户端断开时取消（与 CMS SSE 读超时一致，避免长对话 5 分钟被掐断）
	ctx, cancel := context.WithTimeout(c.Request.Context(), 31*time.Minute)
	defer cancel()
	runtime := sopchat.NewSSERuntimeOptions()
	go cmsClient.CreateChatWithSSECtx(ctx, request, make(map[string]*string), runtime, responseChan, errorChan)

	// 用于保存元数据
	var requestId, threadId string
	threadId = req.ThreadId // 使用请求中的 threadId

	// 处理 SSE 事件
	done := false
	for !done {
		select {
		case <-clientGone:
			log.Println("Client disconnected")
			return

		case response, ok := <-responseChan:
			if !ok {
				// channel 已关闭，表示 SSE 流结束
				done = true
				break
			}

			// 检查响应状态码
			if response.StatusCode != nil && *response.StatusCode != 200 {
				statusCode := *response.StatusCode
				errorMsg := fmt.Sprintf("API returned status code %d", statusCode)
				if response.Body != nil && response.Body.Messages != nil {
					// 尝试从消息中提取错误信息
					for _, msg := range response.Body.Messages {
						if msg != nil && msg.Detail != nil {
							errorMsg += ": " + *msg.Detail
							break
						}
					}
				}
				errorJSON := map[string]interface{}{
					"type":  "error",
					"error": errorMsg,
				}
				if jsonData, err := json.Marshal(errorJSON); err == nil {
					sendSSEJSON(string(jsonData))
				}
				return
			}

			// 处理响应
			if response.Body != nil {
				// 保存元数据
				if requestId == "" && response.Body.RequestId != nil {
					requestId = *response.Body.RequestId
				}
				// ThreadId 不在 Body 中，使用请求中的 threadId

				// 检测 done 消息，提前结束 SSE 循环
				if sopchat.IsDoneMessage(response.Body) {
					done = true
					break
				}

				// 处理消息：直接将消息序列化为 JSON 并转发
				if response.Body.Messages != nil {
					for _, msg := range response.Body.Messages {
						// 检查客户端是否已断开
						select {
						case <-clientGone:
							log.Println("Client disconnected")
							return
						default:
						}

						if msg == nil {
							continue
						}

						// 将消息对象序列化为 JSON
						msgJSON, err := json.Marshal(msg)
						if err != nil {
							log.Printf("Failed to serialize message: %v", err)
							continue // 继续处理，不中断流
						}

						// 直接转发原始消息
						sendSSEJSON(string(msgJSON))
					}
				}
			}

		case err, ok := <-errorChan:
			if ok && err != nil {
				log.Printf("SSE error: %v", err)
				errorJSON := map[string]interface{}{
					"type":  "error",
					"error": err.Error(),
				}
				if jsonData, err := json.Marshal(errorJSON); err == nil {
					sendSSEJSON(string(jsonData))
				}
				return
			}
			done = true
		}
	}

	// 发送完成信号
	doneMsg := map[string]interface{}{
		"type": "done",
	}
	if threadId != "" {
		doneMsg["threadId"] = threadId
	}
	if requestId != "" {
		doneMsg["requestId"] = requestId
	}
	if doneJSON, err := json.Marshal(doneMsg); err == nil {
		sendSSEJSON(string(doneJSON))
	}

	log.Println("Message sending completed")
}
