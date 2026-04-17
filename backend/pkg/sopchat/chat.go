package sopchat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	cmsclient "github.com/alibabacloud-go/cms-20240330/v6/client"
	"github.com/alibabacloud-go/tea/tea"
)

// ChatMessageHandler 是处理流式聊天消息的回调函数
type ChatMessageHandler func(msg *cmsclient.CreateChatResponseBodyMessages) error

// ChatOptions 聊天请求选项
type ChatOptions struct {
	EmployeeName string
	ThreadId     string
	Message      string
	Debug        bool
	// ProductType 数字员工来源产品：sls（默认）或 cms。
	// 空字符串和 "sls" 均视为 SLS，对话时会附加 skill=sop 变量；"cms" 时不附加。
	ProductType string
}

// ChatResult 聊天结果
type ChatResult struct {
	ThreadId  string
	RequestId string
	TraceId   string
}

// QueryEmployeeOptions 查询数字员工的选项
// 用于统一封装 SSE 查询逻辑，支持重试
// 使用方法：
//
//	opts := &sopchat.QueryEmployeeOptions{
//	    CMSClient: cmsClient,
//	    Request:   request,
//	}
//	result, err := sopchat.QueryEmployeeWithRetry(ctx, opts)
//	text := result.Text
//
// 流式回调：
//
//	opts := &sopchat.QueryEmployeeOptions{
//	    CMSClient: cmsClient,
//	    Request:   request,
//	    OnChunk: func(accumulated string) {
//	        // 更新 UI
//	    },
//	}
//	result, err := sopchat.QueryEmployeeWithRetry(ctx, opts)
type QueryEmployeeOptions struct {
	CMSClient *cmsclient.Client
	Request   *cmsclient.CreateChatRequest
	// OnChunk 可选的流式回调，每次收到文本片段时调用，参数为累积的完整文本
	OnChunk func(accumulated string)
}

// QueryEmployeeResult 查询数字员工的结果
type QueryEmployeeResult struct {
	Text      string // 收集到的文本内容
	ThreadId  string // 线程 ID（从请求中获取）
	RequestId string // 请求 ID
	TraceId   string // 追踪 ID
}

// QueryEmployee 执行单次 CMS SSE 查询，收集文本内容
func QueryEmployee(ctx context.Context, opts *QueryEmployeeOptions) (*QueryEmployeeResult, error) {
	responseChan := make(chan *cmsclient.CreateChatResponse)
	errorChan := make(chan error)

	runtime := NewSSERuntimeOptions()
	go opts.CMSClient.CreateChatWithSSECtx(ctx, opts.Request, make(map[string]*string), runtime, responseChan, errorChan)

	result := &QueryEmployeeResult{
		ThreadId: tea.StringValue(opts.Request.ThreadId),
	}
	var textParts []string

	for {
		select {
		case <-ctx.Done():
			result.Text = strings.Join(textParts, "")
			return result, ctx.Err()

		case response, ok := <-responseChan:
			if !ok {
				result.Text = strings.Join(textParts, "")
				return result, nil
			}
			if response.StatusCode != nil && *response.StatusCode != 200 {
				statusCode := *response.StatusCode
				errorMsg := fmt.Sprintf("API returned status code %d", statusCode)
				if response.Body != nil && response.Body.Messages != nil {
					for _, msg := range response.Body.Messages {
						if msg != nil && msg.Detail != nil {
							errorMsg += ": " + *msg.Detail
							break
						}
					}
				}
				result.Text = strings.Join(textParts, "")
				return result, errors.New(errorMsg)
			}
			if response.Body == nil {
				continue
			}
			// 保存元数据
			if result.RequestId == "" && response.Body.RequestId != nil {
				result.RequestId = *response.Body.RequestId
			}
			if result.TraceId == "" && response.Body.TraceId != nil {
				result.TraceId = *response.Body.TraceId
			}
			// 检测 done 消息
			if IsDoneMessage(response.Body) {
				result.Text = strings.Join(textParts, "")
				return result, nil
			}
			// 提取文本内容
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
							if s, ok := v.(string); ok {
								textParts = append(textParts, s)
								if opts.OnChunk != nil {
									opts.OnChunk(strings.Join(textParts, ""))
								}
							}
						}
					}
				}
			}

		case err, ok := <-errorChan:
			result.Text = strings.Join(textParts, "")
			if ok && err != nil {
				return result, fmt.Errorf("SSE error: %w", err)
			}
			return result, nil
		}
	}
}

// QueryEmployeeWithRetry 执行 CMS 查询，空响应时自动重试一次
// 当数字员工返回空响应（可能是服务端工具调用达到上限）时，等待 3 秒后重试
func QueryEmployeeWithRetry(ctx context.Context, opts *QueryEmployeeOptions) (*QueryEmployeeResult, error) {
	result, err := QueryEmployee(ctx, opts)
	if err != nil {
		return result, err
	}
	if result.Text == "" {
		log.Printf("[SOPCHAT] 数字员工返回空响应（可能是服务端工具调用达到上限），3 秒后自动重试一次")
		select {
		case <-time.After(3 * time.Second):
		case <-ctx.Done():
			return result, ctx.Err()
		}
		return QueryEmployee(ctx, opts)
	}
	return result, nil
}

// SendMessage 发送聊天消息并通过回调处理流式响应
func (c *Client) SendMessage(opts *ChatOptions, handler ChatMessageHandler) (*ChatResult, error) {
	variables := map[string]interface{}{}
	// SLS 产品的数字员工需要传递 skill=sop；CMS 员工不需要
	if opts.ProductType == "" || opts.ProductType == "sls" {
		variables["skill"] = "sop"
	}

	// 创建聊天请求
	request := &cmsclient.CreateChatRequest{
		DigitalEmployeeName: tea.String(opts.EmployeeName),
		ThreadId:            tea.String(opts.ThreadId),
		Action:              tea.String("create"),
		Messages: []*cmsclient.CreateChatRequestMessages{
			{
				Role: tea.String("user"),
				Contents: []*cmsclient.CreateChatRequestMessagesContents{
					{
						Type:  tea.String("text"),
						Value: tea.String(opts.Message),
					},
				},
			},
		},
		Variables: variables,
	}

	result := &ChatResult{}

	// 统计包含Tool的Message数量（用于调试）
	toolMessageCount := 0

	// 创建 channel 用于接收 SSE 响应
	responseChan := make(chan *cmsclient.CreateChatResponse)
	errorChan := make(chan error)

	// 使用带 Context 的 SSE 调用（与 NewSSERuntimeOptions 读超时、scheduler 一致）
	ctx, cancel := context.WithTimeout(context.Background(), 31*time.Minute)
	defer cancel()
	runtime := NewSSERuntimeOptions()
	go c.CmsClient.CreateChatWithSSECtx(ctx, request, make(map[string]*string), runtime, responseChan, errorChan)

	// 处理 SSE 事件
	done := false
	for !done {
		select {
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
				return nil, errors.New(errorMsg)
			}

			// 处理响应
			if response.Body != nil {
				// 保存元数据 (使用 ThreadId 字段如果存在)
				if result.RequestId == "" && response.Body.RequestId != nil {
					result.RequestId = *response.Body.RequestId
				}
				if result.TraceId == "" && response.Body.TraceId != nil {
					result.TraceId = *response.Body.TraceId
				}

				// 处理消息
				if response.Body.Messages != nil {
					for i, msg := range response.Body.Messages {
						// #region agent log - Log all messages received from CMS API
						if msg != nil && len(msg.Tools) > 0 {
							toolMessageCount++
							log.Printf("[SOPCHAT] Message %d has %d tools (total tool messages so far: %d)\n", i, len(msg.Tools), toolMessageCount)
							for j, tool := range msg.Tools {
								if toolStatus, ok := tool["status"].(string); ok {
									hasArgs := tool["arguments"] != nil
									logMsg := fmt.Sprintf("[SOPCHAT] Message %d, Tool[%d]: status=%s, name=%v, hasArgs=%v",
										i, j, toolStatus, tool["name"], hasArgs)

									// 如果 hasArgs 为 true，打印参数
									if hasArgs {
										args := tool["arguments"]
										if argsJSON, err := json.Marshal(args); err == nil {
											logMsg += fmt.Sprintf(", args=%s", string(argsJSON))
										} else {
											logMsg += fmt.Sprintf(", args=%+v", args)
										}
									}
									log.Println(logMsg)
								}
							}
						}
						// #endregion
						if handler != nil {
							if err := handler(msg); err != nil {
								return result, err
							}
						}
					}
				} else {
					// 如果没有消息但 Body 不为空，记录警告
					log.Printf("[SOPCHAT] Warning: Response body has no messages (RequestId: %v, TraceId: %v)\n",
						result.RequestId, result.TraceId)
				}
			} else {
				// Body 为空，记录警告
				log.Printf("[SOPCHAT] Warning: Response body is nil (StatusCode: %v)\n", response.StatusCode)
			}

		case err, ok := <-errorChan:
			if ok && err != nil {
				return nil, fmt.Errorf("SSE error: %w", err)
			}
			done = true
		}
	}

	// 使用传入的 ThreadId
	result.ThreadId = opts.ThreadId

	// #region agent log - Final statistics
	log.Printf("[SOPCHAT] Chat completed. Total tool messages received: %d\n", toolMessageCount)
	// #endregion

	return result, nil
}

// SendMessageSync 发送消息并同步收集所有响应（不使用流式回调）
func (c *Client) SendMessageSync(opts *ChatOptions) (*ChatResult, []*cmsclient.CreateChatResponseBodyMessages, error) {
	var messages []*cmsclient.CreateChatResponseBodyMessages

	handler := func(msg *cmsclient.CreateChatResponseBodyMessages) error {
		messages = append(messages, msg)
		return nil
	}

	result, err := c.SendMessage(opts, handler)
	if err != nil {
		return nil, nil, err
	}

	return result, messages, nil
}
