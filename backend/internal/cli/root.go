package cli

import (
	"os"

	"sop-chat/internal/client"
	"sop-chat/pkg/sopchat"

	"github.com/spf13/cobra"
)

// Execute 执行根命令
func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		// cobra 已经打印了错误，这里不需要再打印
		os.Exit(1)
	}
}

// NewRootCmd 创建根命令
func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sop-chat",
		Short: "CLI tool for managing digital employees and conversations",
		Long: `sop-chat is a command-line tool for interacting with Alibaba Cloud's
Digital Employee service. It allows you to create and manage digital
employees, conversation threads, and engage in chat conversations.`,
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
	}

	// 延迟初始化客户端（只在需要时创建）
	// 这样可以避免在显示 help 时也要求环境变量
	var sopClient *sopchat.Client
	getClient := func() (*sopchat.Client, error) {
		if sopClient == nil {
			var err error
			sopClient, err = client.SetupClient()
			if err != nil {
				return nil, err
			}
		}
		return sopClient, nil
	}

	// 创建 chat 命令（延迟初始化客户端）
	chatCmd := &cobra.Command{
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
			c, err := getClient()
			if err != nil {
				return err
			}
			return executeChatCmd(c, cmd, args)
		},
	}
	setupChatFlags(chatCmd)

	// 创建employee和thread命令（传入getClient函数用于延迟初始化）
	employeeCmd := NewEmployeeCmdLazy(getClient)
	threadCmd := NewThreadCmdLazy(getClient)

	cmd.AddCommand(chatCmd)
	cmd.AddCommand(employeeCmd)
	cmd.AddCommand(threadCmd)

	return cmd
}
