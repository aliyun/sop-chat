package cli

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"sop-chat/pkg/sopchat"

	cmsclient "github.com/alibabacloud-go/cms-20240330/v6/client"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/spf13/cobra"
)

// thread 相关变量
var (
	threadEmployeeName string
	threadTitle        string
	threadId           string
	threadFilters      []string
	threadAttrs        []string
)

// NewThreadCmd 创建 thread 命令
func NewThreadCmd(client *sopchat.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "thread",
		Short: "Manage conversation threads",
		Long:  `Create, list, and get conversation threads.`,
	}

	cmd.AddCommand(newThreadCreateCmd(client))
	cmd.AddCommand(newThreadListCmd(client))
	cmd.AddCommand(newThreadGetCmd(client))

	return cmd
}

// NewThreadCmdLazy 创建带延迟客户端初始化的 thread 命令
func NewThreadCmdLazy(getClient func() (*sopchat.Client, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "thread",
		Short: "Manage conversation threads",
		Long:  `Create, list, and get conversation threads.`,
	}

	cmd.AddCommand(newThreadCreateCmdLazy(getClient))
	cmd.AddCommand(newThreadListCmdLazy(getClient))
	cmd.AddCommand(newThreadGetCmdLazy(getClient))

	return cmd
}

// newThreadCreateCmd 创建线程
func newThreadCreateCmd(client *sopchat.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "create",
		Short:        "Create a new thread",
		Long:         `Create a new conversation thread for a digital employee.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if threadEmployeeName == "" {
				return fmt.Errorf("employee name is required (use -e flag)")
			}

			attrs, err := parseThreadAttrs(threadAttrs)
			if err != nil {
				return err
			}

			config := &sopchat.ThreadConfig{
				EmployeeName: threadEmployeeName,
				Title:        threadTitle,
				Attributes:   attrs,
			}

			result, err := client.CreateThread(config)
			if err != nil {
				return err
			}

			if result.Body == nil {
				return fmt.Errorf("received nil response body")
			}

			fmt.Println("✅ Thread created successfully!")
			fmt.Printf("Thread ID: %s\n", tea.StringValue(result.Body.ThreadId))
			if result.Body.RequestId != nil {
				fmt.Printf("Request ID: %s\n", tea.StringValue(result.Body.RequestId))
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&threadEmployeeName, "employee", "e", "", "Digital employee name (required)")
	cmd.Flags().StringVar(&threadTitle, "title", "", "Thread title (optional)")
	cmd.Flags().StringArrayVar(&threadAttrs, "attr", []string{}, "Thread attribute as key=value (supported keys: workspace, project); can be specified multiple times")

	return cmd
}

// newThreadListCmd 列出线程
func newThreadListCmd(client *sopchat.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "list",
		Short:        "List threads for an employee",
		Long:         `List all conversation threads for a digital employee.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if threadEmployeeName == "" {
				return fmt.Errorf("employee name is required (use -e flag)")
			}

			filters, err := parseThreadAttrs(threadFilters)
			if err != nil {
				return fmt.Errorf("invalid --filter: %w", err)
			}
			var filterList []sopchat.ThreadFilter
			for k, v := range filters {
				filterList = append(filterList, sopchat.ThreadFilter{Key: k, Value: v.(string)})
			}

			result, err := client.ListThreads(threadEmployeeName, filterList)
			if err != nil {
				return err
			}

			if result.Body == nil || len(result.Body.Threads) == 0 {
				fmt.Println("No threads found.")
				return nil
			}

			fmt.Printf("Found %d thread(s):\n", len(result.Body.Threads))
			fmt.Println("==========================================")

			for i, thread := range result.Body.Threads {
				fmt.Printf("\n[%d] Thread ID: %s\n", i+1, tea.StringValue(thread.ThreadId))
				fmt.Printf("    Title: %s\n", tea.StringValue(thread.Title))
				fmt.Printf("    Create Time: %s\n", tea.StringValue(thread.CreateTime))
				fmt.Printf("    Update Time: %s\n", tea.StringValue(thread.UpdateTime))
				if len(thread.Attributes) > 0 {
					fmt.Println("    Attributes:")
					for k, v := range thread.Attributes {
						fmt.Printf("      %s: %s\n", k, tea.StringValue(v))
					}
				}
			}

			fmt.Println("==========================================")
			return nil
		},
	}

	cmd.Flags().StringVarP(&threadEmployeeName, "employee", "e", "", "Digital employee name (required)")
	cmd.Flags().StringArrayVar(&threadFilters, "filter", []string{}, "Filter by attribute as key=value (can be specified multiple times)")

	return cmd
}

// newThreadGetCmd 获取线程详情
func newThreadGetCmd(client *sopchat.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "get",
		Short:        "Get thread details and messages",
		Long:         `Get detailed information and all messages for a specific thread.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if threadId == "" {
				return fmt.Errorf("thread ID is required (use --thread-id flag)")
			}
			if threadEmployeeName == "" {
				return fmt.Errorf("employee name is required (use -e flag)")
			}

			// 获取线程元信息
			thread, err := client.GetThread(threadEmployeeName, threadId)
			if err != nil {
				return fmt.Errorf("failed to get thread: %w", err)
			}

			if thread.Body == nil {
				return fmt.Errorf("received nil thread response")
			}

			// 显示线程信息
			fmt.Println("\n==========================================")
			fmt.Printf("Thread ID: %s\n", tea.StringValue(thread.Body.ThreadId))
			fmt.Printf("Title: %s\n", tea.StringValue(thread.Body.Title))
			fmt.Printf("Digital Employee: %s\n", tea.StringValue(thread.Body.DigitalEmployeeName))
			fmt.Printf("Create Time: %s\n", tea.StringValue(thread.Body.CreateTime))
			fmt.Printf("Update Time: %s\n", tea.StringValue(thread.Body.UpdateTime))

			if len(thread.Body.Attributes) > 0 {
				fmt.Println("\nAttributes:")
				for k, v := range thread.Body.Attributes {
					fmt.Printf("  %s: %s\n", k, tea.StringValue(v))
				}
			}

			// 获取线程消息数据
			threadData, err := client.GetThreadData(threadEmployeeName, threadId)
			if err != nil {
				return fmt.Errorf("failed to get thread data: %w", err)
			}

			if threadData.Body == nil || len(threadData.Body.Data) == 0 {
				fmt.Println("\n==========================================")
				fmt.Println("No messages in this thread.")
				return nil
			}

			// 收集所有消息并按时间戳排序
			type MessageWithTime struct {
				Message   interface{}
				Timestamp int64
			}

			var allMessages []MessageWithTime
			for _, data := range threadData.Body.Data {
				if data.Messages == nil {
					continue
				}
				for _, msg := range data.Messages {
					timestamp := int64(0)
					if msg.Timestamp != nil {
						if ts, err := parseTimestamp(*msg.Timestamp); err == nil {
							timestamp = ts
						}
					}
					allMessages = append(allMessages, MessageWithTime{
						Message:   msg,
						Timestamp: timestamp,
					})
				}
			}

			// 按时间戳排序（升序，从旧到新）
			sort.Slice(allMessages, func(i, j int) bool {
				return allMessages[i].Timestamp < allMessages[j].Timestamp
			})

			// 显示消息
			fmt.Println("\n==========================================")
			fmt.Printf("Messages (%d):\n", len(allMessages))
			fmt.Println("==========================================")

			for _, msgWithTime := range allMessages {
				displayThreadMessage(msgWithTime.Message)
			}

			fmt.Println("==========================================")
			return nil
		},
	}

	cmd.Flags().StringVarP(&threadEmployeeName, "employee", "e", "", "Digital employee name (required)")
	cmd.Flags().StringVar(&threadId, "thread-id", "", "Thread ID (required)")

	return cmd
}

// displayThreadMessage 显示线程消息
func displayThreadMessage(msg interface{}) {
	// 尝试将消息转换为SDK的结构体类型
	sdkMsg, ok := msg.(*cmsclient.GetThreadDataResponseBodyDataMessages)
	if !ok {
		// 如果不是SDK结构体，尝试作为map处理
		msgMap, mapOk := msg.(map[string]interface{})
		if !mapOk {
			fmt.Printf("Warning: Unexpected message type: %T\n", msg)
			return
		}
		displayThreadMessageFromMap(msgMap)
		return
	}

	// 处理SDK结构体
	role := tea.StringValue(sdkMsg.Role)
	if role == "" {
		return
	}

	// 根据角色显示不同的前缀
	var prefix string
	switch role {
	case "user":
		prefix = "👤 User"
	case "assistant":
		prefix = "🤖 Assistant"
	case "tool":
		prefix = "🔧 Tool"
	default:
		prefix = fmt.Sprintf("📋 %s", role)
	}

	fmt.Printf("\n[%s]\n", prefix)

	// 显示内容
	if sdkMsg.Contents != nil && len(sdkMsg.Contents) > 0 {
		for _, content := range sdkMsg.Contents {
			contentType, _ := content["type"].(string)
			contentValue, _ := content["value"].(string)
			if contentType == "text" && contentValue != "" {
				fmt.Println(contentValue)
			}
		}
	}

	// 显示工具调用
	if sdkMsg.Tools != nil && len(sdkMsg.Tools) > 0 {
		fmt.Printf("\n  🔧 Tool calls:\n")
		for i, tool := range sdkMsg.Tools {
			// 工具名称
			name, _ := tool["name"].(string)
			fmt.Printf("    [%d] %s", i+1, name)

			// 工具状态
			if status, ok := tool["status"].(string); ok {
				fmt.Printf(" (%s)", status)
			}
			fmt.Println()

			// 显示工具参数
			if arguments := tool["arguments"]; arguments != nil {
				if argBytes, err := json.Marshal(arguments); err == nil {
					fmt.Printf("        Arguments: %s\n", string(argBytes))
				} else {
					fmt.Printf("        Arguments: %v\n", arguments)
				}
			}

			// 显示工具返回结果
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
}

// displayThreadMessageFromMap 从map显示消息（备用方法）
func displayThreadMessageFromMap(msgMap map[string]interface{}) {
	role, _ := msgMap["role"].(string)
	if role == "" {
		return
	}

	// 根据角色显示不同的前缀
	var prefix string
	switch role {
	case "user":
		prefix = "👤 User"
	case "assistant":
		prefix = "🤖 Assistant"
	case "tool":
		prefix = "🔧 Tool"
	default:
		prefix = fmt.Sprintf("📋 %s", role)
	}

	fmt.Printf("\n[%s]\n", prefix)

	// 显示内容
	if contents, ok := msgMap["contents"].([]interface{}); ok {
		for _, content := range contents {
			if contentMap, ok := content.(map[string]interface{}); ok {
				contentType, _ := contentMap["type"].(string)
				contentValue, _ := contentMap["value"].(string)
				if contentType == "text" && contentValue != "" {
					fmt.Println(contentValue)
				}
			}
		}
	}

	// 显示工具调用
	if tools, ok := msgMap["tools"].([]interface{}); ok && len(tools) > 0 {
		fmt.Printf("\n  🔧 Tool calls:\n")
		for i, tool := range tools {
			if toolMap, ok := tool.(map[string]interface{}); ok {
				// 工具名称
				name, _ := toolMap["name"].(string)
				fmt.Printf("    [%d] %s", i+1, name)

				// 工具状态
				if status, ok := toolMap["status"].(string); ok {
					fmt.Printf(" (%s)", status)
				}
				fmt.Println()

				// 显示工具参数
				if arguments := toolMap["arguments"]; arguments != nil {
					if argBytes, err := json.Marshal(arguments); err == nil {
						fmt.Printf("        Arguments: %s\n", string(argBytes))
					} else {
						fmt.Printf("        Arguments: %v\n", arguments)
					}
				}

				// 显示工具返回结果
				if contents, ok := toolMap["contents"].([]interface{}); ok && len(contents) > 0 {
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
	}
}

// newThreadCreateCmdLazy 延迟加载版本的创建线程命令
func newThreadCreateCmdLazy(getClient func() (*sopchat.Client, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "create",
		Short:        "Create a new thread",
		Long:         `Create a new conversation thread for a digital employee.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}
			if threadEmployeeName == "" {
				return fmt.Errorf("employee name is required (use -e flag)")
			}

			attrs, err := parseThreadAttrs(threadAttrs)
			if err != nil {
				return err
			}

			config := &sopchat.ThreadConfig{
				EmployeeName: threadEmployeeName,
				Title:        threadTitle,
				Attributes:   attrs,
			}

			result, err := client.CreateThread(config)
			if err != nil {
				return err
			}

			if result.Body == nil {
				return fmt.Errorf("received nil response body")
			}

			fmt.Println("✅ Thread created successfully!")
			fmt.Printf("Thread ID: %s\n", tea.StringValue(result.Body.ThreadId))
			if result.Body.RequestId != nil {
				fmt.Printf("Request ID: %s\n", tea.StringValue(result.Body.RequestId))
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&threadEmployeeName, "employee", "e", "", "Digital employee name (required)")
	cmd.Flags().StringVar(&threadTitle, "title", "", "Thread title (optional)")
	cmd.Flags().StringArrayVar(&threadAttrs, "attr", []string{}, "Thread attribute as key=value (supported keys: workspace, project); can be specified multiple times")

	return cmd
}

// newThreadListCmdLazy 延迟加载版本的列出线程命令
func newThreadListCmdLazy(getClient func() (*sopchat.Client, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "list",
		Short:        "List threads for an employee",
		Long:         `List all conversation threads for a digital employee.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}
			if threadEmployeeName == "" {
				return fmt.Errorf("employee name is required (use -e flag)")
			}

			filters, err := parseThreadAttrs(threadFilters)
			if err != nil {
				return fmt.Errorf("invalid --filter: %w", err)
			}
			var filterList []sopchat.ThreadFilter
			for k, v := range filters {
				filterList = append(filterList, sopchat.ThreadFilter{Key: k, Value: v.(string)})
			}

			result, err := client.ListThreads(threadEmployeeName, filterList)
			if err != nil {
				return err
			}

			if result.Body == nil || len(result.Body.Threads) == 0 {
				fmt.Println("No threads found.")
				return nil
			}

			fmt.Printf("Found %d thread(s):\n", len(result.Body.Threads))
			fmt.Println("==========================================")

			for i, thread := range result.Body.Threads {
				fmt.Printf("\n[%d] Thread ID: %s\n", i+1, tea.StringValue(thread.ThreadId))
				fmt.Printf("    Title: %s\n", tea.StringValue(thread.Title))
				fmt.Printf("    Create Time: %s\n", tea.StringValue(thread.CreateTime))
				fmt.Printf("    Update Time: %s\n", tea.StringValue(thread.UpdateTime))
				if len(thread.Attributes) > 0 {
					fmt.Println("    Attributes:")
					for k, v := range thread.Attributes {
						fmt.Printf("      %s: %s\n", k, tea.StringValue(v))
					}
				}
			}

			fmt.Println("==========================================")
			return nil
		},
	}

	cmd.Flags().StringVarP(&threadEmployeeName, "employee", "e", "", "Digital employee name (required)")
	cmd.Flags().StringArrayVar(&threadFilters, "filter", []string{}, "Filter by attribute as key=value (can be specified multiple times)")

	return cmd
}

// newThreadGetCmdLazy 延迟加载版本的获取线程详情命令
func newThreadGetCmdLazy(getClient func() (*sopchat.Client, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "get",
		Short:        "Get thread details and messages",
		Long:         `Get detailed information and all messages for a specific thread.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}
			if threadId == "" {
				return fmt.Errorf("thread ID is required (use --thread-id flag)")
			}
			if threadEmployeeName == "" {
				return fmt.Errorf("employee name is required (use -e flag)")
			}

			// 获取线程元信息
			thread, err := client.GetThread(threadEmployeeName, threadId)
			if err != nil {
				return fmt.Errorf("failed to get thread: %w", err)
			}

			if thread.Body == nil {
				return fmt.Errorf("received nil thread response")
			}

			// 显示线程信息
			fmt.Println("\n==========================================")
			fmt.Printf("Thread ID: %s\n", tea.StringValue(thread.Body.ThreadId))
			fmt.Printf("Title: %s\n", tea.StringValue(thread.Body.Title))
			fmt.Printf("Digital Employee: %s\n", tea.StringValue(thread.Body.DigitalEmployeeName))
			fmt.Printf("Create Time: %s\n", tea.StringValue(thread.Body.CreateTime))
			fmt.Printf("Update Time: %s\n", tea.StringValue(thread.Body.UpdateTime))

			if len(thread.Body.Attributes) > 0 {
				fmt.Println("\nAttributes:")
				for k, v := range thread.Body.Attributes {
					fmt.Printf("  %s: %s\n", k, tea.StringValue(v))
				}
			}

			// 获取线程消息数据
			threadData, err := client.GetThreadData(threadEmployeeName, threadId)
			if err != nil {
				return fmt.Errorf("failed to get thread data: %w", err)
			}

			if threadData.Body == nil || len(threadData.Body.Data) == 0 {
				fmt.Println("\n==========================================")
				fmt.Println("No messages in this thread.")
				return nil
			}

			// 收集所有消息并按时间戳排序
			type MessageWithTime struct {
				Message   interface{}
				Timestamp int64
			}

			var allMessages []MessageWithTime
			for _, data := range threadData.Body.Data {
				if data.Messages == nil {
					continue
				}
				for _, msg := range data.Messages {
					timestamp := int64(0)
					if msg.Timestamp != nil {
						if ts, err := parseTimestamp(*msg.Timestamp); err == nil {
							timestamp = ts
						}
					}
					allMessages = append(allMessages, MessageWithTime{
						Message:   msg,
						Timestamp: timestamp,
					})
				}
			}

			// 按时间戳排序（升序，从旧到新）
			sort.Slice(allMessages, func(i, j int) bool {
				return allMessages[i].Timestamp < allMessages[j].Timestamp
			})

			// 显示消息
			fmt.Println("\n==========================================")
			fmt.Printf("Messages (%d):\n", len(allMessages))
			fmt.Println("==========================================")

			for _, msgWithTime := range allMessages {
				displayThreadMessage(msgWithTime.Message)
			}

			fmt.Println("==========================================")
			return nil
		},
	}

	cmd.Flags().StringVarP(&threadEmployeeName, "employee", "e", "", "Digital employee name (required)")
	cmd.Flags().StringVar(&threadId, "thread-id", "", "Thread ID (required)")

	return cmd
}

// parseThreadAttrs 解析 --attr key=value 列表，返回 map
var attrKeyRe = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

func parseThreadAttrs(attrs []string) (map[string]interface{}, error) {
	m := make(map[string]interface{})
	for _, a := range attrs {
		idx := strings.Index(a, "=")
		if idx <= 0 {
			return nil, fmt.Errorf("invalid format %q: expected key=value", a)
		}
		key := a[:idx]
		if !attrKeyRe.MatchString(key) {
			return nil, fmt.Errorf("invalid attribute key %q: only letters, digits and underscores are allowed", key)
		}
		m[key] = a[idx+1:]
	}
	return m, nil
}

// parseTimestamp 解析时间戳字符串为 int64 (Unix 时间戳，秒)
// 支持 ISO8601 格式和 Unix 时间戳字符串
func parseTimestamp(ts string) (int64, error) {
	// 首先尝试解析为 Unix 时间戳（秒）
	if unixTs, err := strconv.ParseInt(ts, 10, 64); err == nil {
		return unixTs, nil
	}

	// 尝试解析为 ISO8601 格式
	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05.999999999Z07:00",
	}

	for _, layout := range layouts {
		if t, err := time.Parse(layout, ts); err == nil {
			return t.Unix(), nil
		}
	}

	return 0, fmt.Errorf("unable to parse timestamp: %s", ts)
}
