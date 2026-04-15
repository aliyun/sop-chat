package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"sop-chat/pkg/sopchat"

	cmsclient "github.com/alibabacloud-go/cms-20240330/v6/client"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/chzyer/readline"
	"github.com/spf13/cobra"
)

// chat 相关变量
var (
	chatThreadId       string
	chatMessage        string
	chatEmployee       string
	chatDebug          bool
	chatShowToolResult bool
)

// setupChatFlags 设置 chat 命令的 flags
func setupChatFlags(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&chatEmployee, "employee", "e", "", "Digital employee name (required)")
	cmd.Flags().StringVar(&chatThreadId, "thread-id", "", "Thread ID (optional, creates new thread if not provided)")
	cmd.Flags().StringVar(&chatMessage, "message", "", "Message to send (optional, enters interactive mode if not provided)")
	cmd.Flags().BoolVar(&chatDebug, "debug", false, "Enable debug mode to show raw message structure")
	cmd.Flags().BoolVar(&chatShowToolResult, "show-tool-results", false, "Show detailed tool call results (parameters are always shown)")
}

// executeChatCmd 执行 chat 命令逻辑
func executeChatCmd(client *sopchat.Client, cmd *cobra.Command, args []string) error {
	if chatEmployee == "" {
		return fmt.Errorf("employee name is required (use -e flag)")
	}

	// 如果没有指定 thread-id，先创建一个新线程
	threadId := chatThreadId
	if threadId == "" {
		config := &sopchat.ThreadConfig{
			EmployeeName: chatEmployee,
		}
		result, err := client.CreateThread(config)
		if err != nil {
			return fmt.Errorf("failed to create thread: %w", err)
		}
		threadId = tea.StringValue(result.Body.ThreadId)
		fmt.Printf("Created new thread: %s\n", threadId)
	}

	// 如果没有提供消息，进入交互模式
	if chatMessage == "" {
		return chatInteractiveMode(client, chatEmployee, threadId, chatShowToolResult, chatDebug)
	}

	// 单次消息模式
	opts := &sopchat.ChatOptions{
		EmployeeName: chatEmployee,
		ThreadId:     threadId,
		Message:      chatMessage,
		Debug:        chatDebug,
	}

	return sendAndDisplayMessage(client, opts, chatShowToolResult)
}

// NewChatCmd 创建 chat 命令
func NewChatCmd(client *sopchat.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Send a message to digital employee",
		Long: `Send a message to digital employee and receive streaming response.

By default, a new thread will be created for each chat session.
If you want to continue an existing conversation, provide --thread-id.

Interactive Mode:
  If --message is not provided, enters interactive mode where you can
  have a continuous conversation. Type 'exit' or 'quit' to end the session.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return executeChatCmd(client, cmd, args)
		},
	}

	setupChatFlags(cmd)
	return cmd
}

// chatInteractiveMode 交互模式
func chatInteractiveMode(client *sopchat.Client, employeeName, threadId string, showToolResult, debug bool) error {
	fmt.Println("\n=== Interactive Chat Mode ===")
	fmt.Println("Type your message and press Enter to send.")
	fmt.Println("Type 'exit' or 'quit' to end the session, or press Ctrl+C/Ctrl+D.")
	fmt.Println()

	// 配置 readline
	rl, err := readline.New("")
	if err != nil {
		return fmt.Errorf("failed to initialize readline: %w", err)
	}
	defer rl.Close()

	for {
		// 显示提示符
		fmt.Println()
		rl.SetPrompt("You: ")

		// 读取用户输入
		message, err := rl.Readline()
		if err != nil {
			// EOF (Ctrl+D) 或其他错误
			fmt.Println("\nGoodbye!")
			return nil
		}

		message = strings.TrimSpace(message)

		// 处理退出命令
		if message == "exit" || message == "quit" {
			fmt.Println("Goodbye!")
			return nil
		}

		// 忽略空消息
		if message == "" {
			continue
		}

		// 发送消息
		opts := &sopchat.ChatOptions{
			EmployeeName: employeeName,
			ThreadId:     threadId,
			Message:      message,
			Debug:        debug,
		}

		if err := sendAndDisplayMessage(client, opts, showToolResult); err != nil {
			fmt.Printf("\nError: %v\n", err)
		}
	}
}

// sendAndDisplayMessage 发送消息并显示结果
func sendAndDisplayMessage(client *sopchat.Client, opts *sopchat.ChatOptions, showToolResult bool) error {
	currentRole := ""
	currentType := ""
	hasContent := false

	handler := func(msg *cmsclient.CreateChatResponseBodyMessages) error {
		msgType := tea.StringValue(msg.Type)
		msgRole := tea.StringValue(msg.Role)

		// 调试模式
		if opts.Debug {
			fmt.Printf("\n[DEBUG] Role: %s, Type: %s\n", msgRole, msgType)
			if msg.Contents != nil {
				fmt.Printf("[DEBUG] Contents: %v\n", msg.Contents)
			} else {
				fmt.Printf("[DEBUG] Contents: nil\n")
			}
			if msg.Detail != nil {
				fmt.Printf("[DEBUG] Detail: %s\n", tea.StringValue(msg.Detail))
			} else {
				fmt.Printf("[DEBUG] Detail: nil\n")
			}
			// 显示所有字段（用于调试）
			if msgRole == "system" {
				fmt.Printf("[DEBUG] Full message: %+v\n", msg)
			}
			if len(msg.Tools) > 0 {
				fmt.Printf("[DEBUG] === Tools (count: %d) ===\n", len(msg.Tools))
				for i, tool := range msg.Tools {
					fmt.Printf("[DEBUG] Tool %d:\n", i+1)
					for key, value := range tool {
						fmt.Printf("[DEBUG]   %s: %v\n", key, value)
					}
				}
				fmt.Printf("[DEBUG] === End Tools ===\n")
			}
		}

		// 检查 Events 中是否有错误信息
		hasErrorEvent := false
		var errorMessage string
		var errorCode string
		if msg.Events != nil && len(msg.Events) > 0 {
			for _, event := range msg.Events {
				// event 已经是 map[string]interface{} 类型
				if payload, ok := event["payload"].(map[string]interface{}); ok {
					if success, ok := payload["success"].(bool); ok && !success {
						hasErrorEvent = true
						if errorInfo, ok := payload["error"].(map[string]interface{}); ok {
							if msg, ok := errorInfo["message"].(string); ok && msg != "" {
								errorMessage = msg
							}
							if code, ok := errorInfo["code"].(string); ok && code != "" {
								errorCode = code
							}
						}
						break
					}
				}
			}
		}

		// 检查是否是错误消息
		isError := msgType == "error" || msgType == "failed" || hasErrorEvent
		hasDetail := msg.Detail != nil && tea.StringValue(msg.Detail) != ""

		// 检查 Contents 中是否有错误信息或任何内容
		hasErrorContent := false
		hasAnyContent := false
		if msg.Contents != nil && len(msg.Contents) > 0 {
			hasAnyContent = true
			for _, content := range msg.Contents {
				contentType, _ := content["type"].(string)
				contentValue, _ := content["value"].(string)
				if contentType == "error" {
					hasErrorContent = true
					break
				}
				if contentType != "" && contentValue != "" {
					hasAnyContent = true
				}
			}
		}

		// 检查是否有 Events（即使没有错误，Events 也可能包含重要信息）
		hasEvents := msg.Events != nil && len(msg.Events) > 0

		// 如果是错误消息或有错误信息，即使 Role 为空也要处理
		if msgRole == "" && !isError && !hasDetail && !hasErrorContent && !hasAnyContent && !hasEvents {
			return nil
		}

		// system 消息特殊处理：即使没有 Contents/Detail，也可能包含重要信息（Events）
		// 在调试模式下总是显示，非调试模式下如果没有内容则跳过
		if msgRole == "system" && !opts.Debug && !isError && !hasDetail && !hasAnyContent && len(msg.Tools) == 0 && !hasEvents {
			return nil
		}

		// 根据角色显示不同的前缀
		var prefix string
		if isError {
			prefix = "Error"
		} else {
			switch msgRole {
			case "user":
				prefix = "User"
			case "assistant":
				prefix = "Assistant"
			case "tool":
				prefix = "Tool"
			case "system":
				prefix = "System"
			default:
				if msgRole == "" {
					prefix = "Message"
				} else {
					prefix = fmt.Sprintf("Message %s", msgRole)
				}
			}
		}

		// 判断是否需要打印新的角色标识
		needNewHeader := false
		if msgRole != currentRole {
			needNewHeader = true
		} else if msgType != "chunk" && currentType == "chunk" {
			needNewHeader = true
		} else if isError && currentType != "error" {
			needNewHeader = true
		}

		// 如果需要新的标识头
		if needNewHeader {
			if hasContent {
				fmt.Println() // 换行，结束上一段
			}

			// system 消息如果没有内容，在非调试模式下跳过（已在前面检查过）
			// 这里不需要再次检查，因为前面已经处理过了

			fmt.Printf("\n[%s]\n", prefix)
			currentRole = msgRole
			hasContent = false
		}

		currentType = msgType

		// 打印消息内容
		if msg.Contents != nil && len(msg.Contents) > 0 {
			for _, content := range msg.Contents {
				contentType, _ := content["type"].(string)
				contentValue, _ := content["value"].(string)

				// 对于错误消息或错误类型的内容，即使 value 为空也要显示
				if contentType != "" {
					if contentType == "error" || isError {
						// 错误消息特殊处理
						if contentValue != "" {
							fmt.Printf("  Error: %s\n", contentValue)
						} else {
							fmt.Printf("  Error [%s]\n", contentType)
						}
						hasContent = true
					} else if contentValue != "" {
						if msgType == "chunk" || contentType == "text" {
							fmt.Print(contentValue)
							hasContent = true
						} else {
							fmt.Printf("  [%s]: %s\n", contentType, contentValue)
							hasContent = true
						}
					} else if contentType != "spin_text" {
						// 对于非 spin_text 的空值内容，也显示类型（可能是占位符）
						fmt.Printf("  [%s]: (empty)\n", contentType)
						hasContent = true
					}
				}
			}
		} else if msgRole == "system" && !hasContent && !hasErrorEvent {
			// system 消息如果没有 Contents 且没有错误事件，显示提示信息
			fmt.Printf("  (System message, no content)\n")
			hasContent = true
		}

		// 打印 Events 中的错误信息
		if hasErrorEvent {
			if hasContent {
				fmt.Println()
			}
			fmt.Printf("  Error:\n")
			if errorCode != "" {
				fmt.Printf("    Error code: %s\n", errorCode)
			}
			if errorMessage != "" {
				fmt.Printf("    Error message: %s\n", errorMessage)
			}
			hasContent = true
		}

		// 打印详细信息（错误信息优先显示）
		if msg.Detail != nil {
			detail := tea.StringValue(msg.Detail)
			if detail != "" {
				if hasContent {
					fmt.Println()
				}
				if isError {
					fmt.Printf("  Error Detail: %s\n", detail)
				} else {
					fmt.Printf("  Detail: %s\n", detail)
				}
				hasContent = true
			}
		}

		// 打印 Tools 信息
		if len(msg.Tools) > 0 {
			if !opts.Debug {
				if hasContent {
					fmt.Println()
				}
				fmt.Printf("\n  Tool calls:\n")
				for i, tool := range msg.Tools {
					// 工具名称
					if name, ok := tool["name"].(string); ok {
						fmt.Printf("    [%d] %s", i+1, name)
					} else {
						fmt.Printf("    [%d] Unknown tool", i+1)
					}

					// 工具状态
					status := ""
					if s, ok := tool["status"].(string); ok {
						status = s
						fmt.Printf(" (%s)", status)
					}
					fmt.Println()

					// 始终显示工具参数
					if arguments := tool["arguments"]; arguments != nil {
						if argBytes, err := json.Marshal(arguments); err == nil {
							fmt.Printf("        Arguments: %s\n", string(argBytes))
						} else {
							fmt.Printf("        Arguments: %v\n", arguments)
						}
					}

					// 如果工具失败，始终显示错误信息
					if status == "fail" {
						errorFound := false
						if contents, ok := tool["contents"].([]interface{}); ok && len(contents) > 0 {
							fmt.Printf("        Error:\n")
							for _, content := range contents {
								if contentMap, ok := content.(map[string]interface{}); ok {
									contentType, _ := contentMap["type"].(string)
									if value, ok := contentMap["value"].(string); ok && value != "" {
										lines := strings.Split(value, "\n")
										for _, line := range lines {
											if line != "" {
												fmt.Printf("          %s\n", line)
											}
										}
										errorFound = true
									} else if contentType != "" {
										fmt.Printf("          [%s]\n", contentType)
										errorFound = true
									}
								}
							}
						}
						if !errorFound {
							fmt.Printf("        Tool call failed\n")
						}
					} else if showToolResult {
						// 根据 flag 决定是否显示工具返回结果（成功的情况）
						if contents, ok := tool["contents"].([]interface{}); ok && len(contents) > 0 {
							fmt.Printf("        Result:\n")
							for _, content := range contents {
								if contentMap, ok := content.(map[string]interface{}); ok {
									if contentType, _ := contentMap["type"].(string); contentType == "text" {
										if value, ok := contentMap["value"].(string); ok && value != "" {
											lines := strings.Split(value, "\n")
											for _, line := range lines {
												if line != "" {
													fmt.Printf("          %s\n", line)
												}
											}
										}
									}
								}
							}
						}
					}
				}
				hasContent = true
			}
		}

		return nil
	}

	_, err := client.SendMessage(opts, handler)
	if err != nil {
		return err
	}

	// 确保输出结束时有换行
	if hasContent {
		fmt.Println()
	}

	return nil
}
