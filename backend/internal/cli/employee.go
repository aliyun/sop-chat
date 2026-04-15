package cli

import (
	"fmt"
	"strings"

	"sop-chat/pkg/sopchat"

	"github.com/alibabacloud-go/tea/tea"
	"github.com/spf13/cobra"
)

// employee 相关变量
var (
	employeeName        string
	employeeDisplayName string
	employeeDescription string
	employeeDefaultRule string
	employeeRoleArn     string
	sopType             string
	// OSS 类型字段
	sopBasePath    string
	sopRegion      string
	sopBucket      string
	sopDescription string
	// Yunxiao 类型字段
	sopOrganizationId string
	sopRepositoryId   string
	sopBranchName     string
	sopToken          string
	// Builtin 类型字段
	sopBuiltinId string
)

// NewEmployeeCmd 创建 employee 命令
func NewEmployeeCmd(client *sopchat.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "employee",
		Short: "Manage digital employees",
		Long:  `Create, list, update, delete and get digital employees.`,
	}

	cmd.AddCommand(newEmployeeListCmd(client))
	cmd.AddCommand(newEmployeeGetCmd(client))
	cmd.AddCommand(newEmployeeCreateCmd(client))
	cmd.AddCommand(newEmployeeUpdateCmd(client))
	cmd.AddCommand(newEmployeeDeleteCmd(client))

	return cmd
}

// NewEmployeeCmdLazy 创建带延迟客户端初始化的 employee 命令
func NewEmployeeCmdLazy(getClient func() (*sopchat.Client, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "employee",
		Short: "Manage digital employees",
		Long:  `Create, list, update, delete and get digital employees.`,
	}

	cmd.AddCommand(newEmployeeListCmdLazy(getClient))
	cmd.AddCommand(newEmployeeGetCmdLazy(getClient))
	cmd.AddCommand(newEmployeeCreateCmdLazy(getClient))
	cmd.AddCommand(newEmployeeUpdateCmdLazy(getClient))
	cmd.AddCommand(newEmployeeDeleteCmdLazy(getClient))

	return cmd
}

// newEmployeeListCmd 列出数字员工
func newEmployeeListCmd(client *sopchat.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "list",
		Short:        "List all digital employees",
		Long:         `List all digital employees (filtered by sop- prefix).`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {

			employees, err := client.ListEmployees()
			if err != nil {
				return err
			}

			if len(employees) > 0 {
				fmt.Printf("Found %d sop- employee(s):\n", len(employees))
				fmt.Println("==========================================")
				for i, employee := range employees {
					if employee == nil {
						continue
					}
					fmt.Printf("\n[%d] Name: %s\n", i+1, tea.StringValue(employee.Name))
					fmt.Printf("    Display Name: %s\n", tea.StringValue(employee.DisplayName))
					fmt.Printf("    Description: %s\n", tea.StringValue(employee.Description))
					fmt.Printf("    Employee Type: %s\n", tea.StringValue(employee.EmployeeType))
					fmt.Printf("    Create Time: %s\n", tea.StringValue(employee.CreateTime))
					fmt.Printf("    Update Time: %s\n", tea.StringValue(employee.UpdateTime))
				}
				fmt.Println("==========================================")
			} else {
				fmt.Println("No sop- employees found.")
			}

			return nil
		},
	}

	return cmd
}

// newEmployeeGetCmd 获取数字员工详情
func newEmployeeGetCmd(client *sopchat.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "get",
		Short:        "Get a digital employee's details",
		Long:         `Get detailed configuration of a specific digital employee.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if employeeName == "" {
				return fmt.Errorf("employee name is required (use -e flag)")
			}

			result, err := client.GetEmployee(employeeName)
			if err != nil {
				return err
			}

			if result == nil || result.Body == nil {
				return fmt.Errorf("received nil response")
			}

			fmt.Println("\n==========================================")
			fmt.Printf("Name: %s\n", tea.StringValue(result.Body.Name))
			fmt.Printf("Display Name: %s\n", tea.StringValue(result.Body.DisplayName))
			fmt.Printf("Description: %s\n", tea.StringValue(result.Body.Description))
			fmt.Printf("Employee Type: %s\n", tea.StringValue(result.Body.EmployeeType))
			fmt.Printf("Region ID: %s\n", tea.StringValue(result.Body.RegionId))
			fmt.Printf("Role ARN: %s\n", tea.StringValue(result.Body.RoleArn))
			fmt.Printf("Default Rule: %s\n", tea.StringValue(result.Body.DefaultRule))
			fmt.Printf("Create Time: %s\n", tea.StringValue(result.Body.CreateTime))
			fmt.Printf("Update Time: %s\n", tea.StringValue(result.Body.UpdateTime))

			// 显示 Knowledges 信息
			if result.Body.Knowledges != nil {
				fmt.Println("\n--- Knowledges ---")

				// Sop Knowledges
				if len(result.Body.Knowledges.Sop) > 0 {
					fmt.Printf("Sop Knowledges (%d):\n", len(result.Body.Knowledges.Sop))
					fmt.Println(formatSopKnowledges(result.Body.Knowledges.Sop))
				} else {
					fmt.Println("Sop Knowledges: None")
				}

				// Bailian Knowledges
				if len(result.Body.Knowledges.Bailian) > 0 {
					fmt.Printf("\nBailian Knowledges (%d):\n", len(result.Body.Knowledges.Bailian))
					for i, bailian := range result.Body.Knowledges.Bailian {
						fmt.Printf("  [%d]\n", i+1)
						fmt.Printf("    Index ID: %s\n", tea.StringValue(bailian.IndexId))
						fmt.Printf("    Workspace ID: %s\n", tea.StringValue(bailian.WorkspaceId))
						fmt.Printf("    Region: %s\n", tea.StringValue(bailian.Region))
						if bailian.Attributes != nil {
							fmt.Printf("    Attributes: %s\n", tea.StringValue(bailian.Attributes))
						}
					}
				} else {
					fmt.Println("Bailian Knowledges: None")
				}
			} else {
				fmt.Println("\nKnowledges: None")
			}

			fmt.Println("==========================================")
			fmt.Printf("\nRequest ID: %s\n", tea.StringValue(result.Body.RequestId))

			return nil
		},
	}

	cmd.Flags().StringVarP(&employeeName, "employee", "e", "", "Digital employee name (required)")
	return cmd
}

// newEmployeeCreateCmd 创建数字员工
func newEmployeeCreateCmd(client *sopchat.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "create",
		Short:        "Create a new digital employee",
		Long:         `Create a new digital employee with specified configuration.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// 判断是否为交互模式（没有提供任何必需参数）
			if employeeName == "" && employeeDisplayName == "" {
				return createEmployeeInteractive(client)
			}

			// 命令行模式：验证必需参数
			if employeeName == "" {
				return fmt.Errorf("employee name is required")
			}
			if employeeDisplayName == "" {
				return fmt.Errorf("employee display name is required")
			}
			if employeeRoleArn == "" {
				return fmt.Errorf("role ARN is required (use --role-arn flag)")
			}

			// 构建配置
			config := &sopchat.EmployeeConfig{
				Name:          employeeName,
				DisplayName:   employeeDisplayName,
				Description:   employeeDescription,
				DefaultRule:   employeeDefaultRule,
				RoleArn:       employeeRoleArn,
				SopKnowledges: []map[string]interface{}{},
			}

			// 如果指定了 SOP 类型，添加 SOP Knowledge
			if sopType != "" {
				sop := buildSopKnowledge()
				if sop != nil {
					config.SopKnowledges = append(config.SopKnowledges, sop)
				}
			}

			// 创建员工
			result, err := client.CreateEmployee(config)
			if err != nil {
				return err
			}

			fmt.Println("Digital employee created successfully!")
			fmt.Printf("Request ID: %s\n", tea.StringValue(result.Body.RequestId))

			return nil
		},
	}

	cmd.Flags().StringVarP(&employeeName, "name", "n", "", "Employee name")
	cmd.Flags().StringVar(&employeeDisplayName, "display-name", "", "Display name")
	cmd.Flags().StringVar(&employeeDescription, "description", "", "Description")
	cmd.Flags().StringVar(&employeeDefaultRule, "default-rule", "", "Default rule")
	cmd.Flags().StringVar(&employeeRoleArn, "role-arn", "", "Role ARN or role name (required). Can be just the role name (e.g., 'my-role') or full ARN (e.g., 'acs:ram::123456789012:role/my-role')")
	cmd.Flags().StringVar(&sopType, "sop-type", "", "SOP type (oss/yunxiao/builtin)")
	cmd.Flags().StringVar(&sopRegion, "sop-region", "", "SOP region (for oss)")
	cmd.Flags().StringVar(&sopBucket, "sop-bucket", "", "SOP bucket (for oss)")
	cmd.Flags().StringVar(&sopBasePath, "sop-base-path", "", "SOP base path (for oss/yunxiao)")
	cmd.Flags().StringVar(&sopDescription, "sop-description", "", "SOP description (for oss/yunxiao)")
	cmd.Flags().StringVar(&sopOrganizationId, "sop-org-id", "", "Organization ID (for yunxiao)")
	cmd.Flags().StringVar(&sopRepositoryId, "sop-repo-id", "", "Repository ID (for yunxiao)")
	cmd.Flags().StringVar(&sopBranchName, "sop-branch", "", "Branch name (for yunxiao)")
	cmd.Flags().StringVar(&sopToken, "sop-token", "", "Token (for yunxiao)")
	cmd.Flags().StringVar(&sopBuiltinId, "sop-id", "", "ID (for builtin)")

	return cmd
}

// createEmployeeInteractive 交互式创建员工
func createEmployeeInteractive(client *sopchat.Client) error {
	fmt.Println("=== Interactive Employee Creation ===")

	name := readInput("Employee Name", "")
	if name == "" {
		return fmt.Errorf("employee name is required")
	}

	displayName := readInput("Display Name", "")
	if displayName == "" {
		return fmt.Errorf("display name is required")
	}

	description := readInput("Description (optional)", "")
	defaultRule := readInput("Default Rule (optional)", "")
	roleArn := readInput("Role ARN or role name (required, e.g., 'my-role' or full ARN)", "")
	if roleArn == "" {
		return fmt.Errorf("role ARN or role name is required")
	}

	// 管理 SOP Knowledges
	sopKnowledges := manageSopKnowledges()

	config := &sopchat.EmployeeConfig{
		Name:          name,
		DisplayName:   displayName,
		Description:   description,
		DefaultRule:   defaultRule,
		RoleArn:       roleArn,
		SopKnowledges: sopKnowledges,
	}

	result, err := client.CreateEmployee(config)
	if err != nil {
		return err
	}

	fmt.Println("\nDigital employee created successfully!")
	fmt.Printf("Request ID: %s\n", tea.StringValue(result.Body.RequestId))

	return nil
}

// newEmployeeUpdateCmd 更新数字员工
func newEmployeeUpdateCmd(client *sopchat.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "update",
		Short:        "Update a digital employee",
		Long:         `Update an existing digital employee's configuration.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if employeeName == "" {
				return fmt.Errorf("employee name is required (use -e flag)")
			}

			// 获取当前配置
			current, err := client.GetEmployee(employeeName)
			if err != nil {
				return fmt.Errorf("failed to get current employee: %w", err)
			}

			if current == nil || current.Body == nil {
				return fmt.Errorf("received nil response")
			}

			fmt.Printf("=== Updating Employee: %s ===\n", employeeName)

			// 交互式更新
			displayName := readInput("Display Name", tea.StringValue(current.Body.DisplayName))
			description := readInput("Description", tea.StringValue(current.Body.Description))
			defaultRule := readInput("Default Rule", tea.StringValue(current.Body.DefaultRule))

			// 管理 SOP Knowledges
			var initialSopList []map[string]interface{}
			if current.Body.Knowledges != nil {
				initialSopList = current.Body.Knowledges.Sop
			}
			sopKnowledges := manageSopKnowledgesWithInitial(initialSopList)

			config := &sopchat.EmployeeConfig{
				Name:          employeeName,
				DisplayName:   displayName,
				Description:   description,
				DefaultRule:   defaultRule,
				SopKnowledges: sopKnowledges,
			}

			result, err := client.UpdateEmployee(config)
			if err != nil {
				return err
			}

			fmt.Println("\nDigital employee updated successfully!")
			fmt.Printf("Request ID: %s\n", tea.StringValue(result.Body.RequestId))

			return nil
		},
	}

	cmd.Flags().StringVarP(&employeeName, "employee", "e", "", "Digital employee name (required)")
	return cmd
}

// newEmployeeDeleteCmd 删除数字员工
func newEmployeeDeleteCmd(client *sopchat.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "delete",
		Short:        "Delete a digital employee",
		Long:         `Delete an existing digital employee.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if employeeName == "" {
				return fmt.Errorf("employee name is required (use -e flag)")
			}

			// 确认删除
			confirmation := readInput(fmt.Sprintf("Are you sure you want to delete '%s'? (yes/no)", employeeName), "no")
			if strings.ToLower(confirmation) != "yes" {
				fmt.Println("Deletion cancelled.")
				return nil
			}

			err := client.DeleteEmployee(employeeName)
			if err != nil {
				return err
			}

			fmt.Printf("Digital employee '%s' deleted successfully!\n", employeeName)
			return nil
		},
	}

	cmd.Flags().StringVarP(&employeeName, "employee", "e", "", "Digital employee name (required)")
	return cmd
}

// buildSopKnowledge 从命令行参数构建 SOP Knowledge
func buildSopKnowledge() map[string]interface{} {
	sop := map[string]interface{}{
		"type": sopType,
	}

	switch sopType {
	case "oss":
		if sopBucket == "" || sopBasePath == "" {
			fmt.Println("Error: bucket and base-path are required for OSS type")
			return nil
		}
		if sopRegion != "" {
			sop["region"] = sopRegion
		}
		sop["bucket"] = sopBucket
		sop["basePath"] = sopBasePath
		if sopDescription != "" {
			sop["description"] = sopDescription
		}

	case "yunxiao":
		if sopOrganizationId == "" || sopRepositoryId == "" || sopBranchName == "" || sopBasePath == "" || sopToken == "" {
			fmt.Println("Error: organization-id, repository-id, branch-name, base-path, and token are required for yunxiao type")
			return nil
		}
		sop["organizationId"] = sopOrganizationId
		sop["repositoryId"] = sopRepositoryId
		sop["branchName"] = sopBranchName
		sop["basePath"] = sopBasePath
		sop["token"] = sopToken
		if sopDescription != "" {
			sop["description"] = sopDescription
		}

	case "builtin":
		if sopBuiltinId == "" {
			fmt.Println("Error: id is required for builtin type")
			return nil
		}
		sop["id"] = sopBuiltinId

	default:
		fmt.Printf("Error: unknown SOP type: %s\n", sopType)
		return nil
	}

	return sop
}

// 延迟加载版本的员工命令
func newEmployeeListCmdLazy(getClient func() (*sopchat.Client, error)) *cobra.Command {
	cmd := newEmployeeListCmd(nil)
	originalRunE := cmd.RunE
	cmd.RunE = func(c *cobra.Command, args []string) error {
		client, err := getClient()
		if err != nil {
			return err
		}
		// 临时替换client并执行
		return executeWithClient(client, originalRunE, c, args)
	}
	return cmd
}

func newEmployeeGetCmdLazy(getClient func() (*sopchat.Client, error)) *cobra.Command {
	cmd := newEmployeeGetCmd(nil)
	cmd.RunE = func(c *cobra.Command, args []string) error {
		client, err := getClient()
		if err != nil {
			return err
		}
		return executeEmployeeGet(client, c, args)
	}
	return cmd
}

func newEmployeeCreateCmdLazy(getClient func() (*sopchat.Client, error)) *cobra.Command {
	cmd := newEmployeeCreateCmd(nil)
	cmd.RunE = func(c *cobra.Command, args []string) error {
		client, err := getClient()
		if err != nil {
			return err
		}
		return executeEmployeeCreate(client, c, args)
	}
	return cmd
}

func newEmployeeUpdateCmdLazy(getClient func() (*sopchat.Client, error)) *cobra.Command {
	cmd := newEmployeeUpdateCmd(nil)
	cmd.RunE = func(c *cobra.Command, args []string) error {
		client, err := getClient()
		if err != nil {
			return err
		}
		return executeEmployeeUpdate(client, c, args)
	}
	return cmd
}

func newEmployeeDeleteCmdLazy(getClient func() (*sopchat.Client, error)) *cobra.Command {
	cmd := newEmployeeDeleteCmd(nil)
	cmd.RunE = func(c *cobra.Command, args []string) error {
		client, err := getClient()
		if err != nil {
			return err
		}
		return executeEmployeeDelete(client, c, args)
	}
	return cmd
}

// Helper functions that extract the logic from RunE
func executeWithClient(client *sopchat.Client, originalRunE func(*cobra.Command, []string) error, cmd *cobra.Command, args []string) error {

	employees, err := client.ListEmployees()
	if err != nil {
		return err
	}

	if len(employees) > 0 {
		fmt.Printf("Found %d sop- employee(s):\n", len(employees))
		fmt.Println("==========================================")
		for i, employee := range employees {
			if employee == nil {
				continue
			}
			fmt.Printf("\n[%d] Name: %s\n", i+1, tea.StringValue(employee.Name))
			fmt.Printf("    Display Name: %s\n", tea.StringValue(employee.DisplayName))
			fmt.Printf("    Description: %s\n", tea.StringValue(employee.Description))
			fmt.Printf("    Employee Type: %s\n", tea.StringValue(employee.EmployeeType))
			fmt.Printf("    Create Time: %s\n", tea.StringValue(employee.CreateTime))
			fmt.Printf("    Update Time: %s\n", tea.StringValue(employee.UpdateTime))
		}
		fmt.Println("==========================================")
	} else {
		fmt.Println("No sop- employees found.")
	}

	return nil
}

func executeEmployeeGet(client *sopchat.Client, cmd *cobra.Command, args []string) error {
	if employeeName == "" {
		return fmt.Errorf("employee name is required (use -e flag)")
	}

	result, err := client.GetEmployee(employeeName)
	if err != nil {
		return err
	}

	if result == nil || result.Body == nil {
		return fmt.Errorf("received nil response")
	}

	fmt.Println("\n==========================================")
	fmt.Printf("Name: %s\n", tea.StringValue(result.Body.Name))
	fmt.Printf("Display Name: %s\n", tea.StringValue(result.Body.DisplayName))
	fmt.Printf("Description: %s\n", tea.StringValue(result.Body.Description))
	fmt.Printf("Employee Type: %s\n", tea.StringValue(result.Body.EmployeeType))
	fmt.Printf("Region ID: %s\n", tea.StringValue(result.Body.RegionId))
	fmt.Printf("Role ARN: %s\n", tea.StringValue(result.Body.RoleArn))
	fmt.Printf("Default Rule: %s\n", tea.StringValue(result.Body.DefaultRule))
	fmt.Printf("Create Time: %s\n", tea.StringValue(result.Body.CreateTime))
	fmt.Printf("Update Time: %s\n", tea.StringValue(result.Body.UpdateTime))

	// 显示 Knowledges 信息
	if result.Body.Knowledges != nil {
		fmt.Println("\n--- Knowledges ---")

		// Sop Knowledges
		if len(result.Body.Knowledges.Sop) > 0 {
			fmt.Printf("Sop Knowledges (%d):\n", len(result.Body.Knowledges.Sop))
			fmt.Println(formatSopKnowledges(result.Body.Knowledges.Sop))
		} else {
			fmt.Println("Sop Knowledges: None")
		}

		// Bailian Knowledges
		if len(result.Body.Knowledges.Bailian) > 0 {
			fmt.Printf("\nBailian Knowledges (%d):\n", len(result.Body.Knowledges.Bailian))
			for i, bailian := range result.Body.Knowledges.Bailian {
				fmt.Printf("  [%d]\n", i+1)
				fmt.Printf("    Index ID: %s\n", tea.StringValue(bailian.IndexId))
				fmt.Printf("    Workspace ID: %s\n", tea.StringValue(bailian.WorkspaceId))
				fmt.Printf("    Region: %s\n", tea.StringValue(bailian.Region))
				if bailian.Attributes != nil {
					fmt.Printf("    Attributes: %s\n", tea.StringValue(bailian.Attributes))
				}
			}
		} else {
			fmt.Println("Bailian Knowledges: None")
		}
	} else {
		fmt.Println("\nKnowledges: None")
	}

	fmt.Println("==========================================")
	fmt.Printf("\nRequest ID: %s\n", tea.StringValue(result.Body.RequestId))

	return nil
}

func executeEmployeeCreate(client *sopchat.Client, cmd *cobra.Command, args []string) error {
	// 判断是否为交互模式（没有提供任何必需参数）
	if employeeName == "" && employeeDisplayName == "" {
		return createEmployeeInteractive(client)
	}

	// 命令行模式：验证必需参数
	if employeeName == "" {
		return fmt.Errorf("employee name is required")
	}
	if employeeDisplayName == "" {
		return fmt.Errorf("employee display name is required")
	}
	if employeeRoleArn == "" {
		return fmt.Errorf("role ARN is required (use --role-arn flag)")
	}

	// 构建配置
	config := &sopchat.EmployeeConfig{
		Name:          employeeName,
		DisplayName:   employeeDisplayName,
		Description:   employeeDescription,
		DefaultRule:   employeeDefaultRule,
		RoleArn:       employeeRoleArn,
		SopKnowledges: []map[string]interface{}{},
	}

	// 如果指定了 SOP 类型，添加 SOP Knowledge
	if sopType != "" {
		sop := buildSopKnowledge()
		if sop != nil {
			config.SopKnowledges = append(config.SopKnowledges, sop)
		}
	}

	// 创建员工
	result, err := client.CreateEmployee(config)
	if err != nil {
		return err
	}

	fmt.Println("Digital employee created successfully!")
	fmt.Printf("Request ID: %s\n", tea.StringValue(result.Body.RequestId))

	return nil
}

func executeEmployeeUpdate(client *sopchat.Client, cmd *cobra.Command, args []string) error {
	if employeeName == "" {
		return fmt.Errorf("employee name is required (use -e flag)")
	}

	// 获取当前配置
	current, err := client.GetEmployee(employeeName)
	if err != nil {
		return fmt.Errorf("failed to get current employee: %w", err)
	}

	if current == nil || current.Body == nil {
		return fmt.Errorf("received nil response")
	}

	fmt.Printf("=== Updating Employee: %s ===\n", employeeName)

	// 交互式更新
	displayName := readInput("Display Name", tea.StringValue(current.Body.DisplayName))
	description := readInput("Description", tea.StringValue(current.Body.Description))
	defaultRule := readInput("Default Rule", tea.StringValue(current.Body.DefaultRule))

	// 管理 SOP Knowledges
	var initialSopList []map[string]interface{}
	if current.Body.Knowledges != nil {
		initialSopList = current.Body.Knowledges.Sop
	}
	sopKnowledges := manageSopKnowledgesWithInitial(initialSopList)

	config := &sopchat.EmployeeConfig{
		Name:          employeeName,
		DisplayName:   displayName,
		Description:   description,
		DefaultRule:   defaultRule,
		SopKnowledges: sopKnowledges,
	}

	result, err := client.UpdateEmployee(config)
	if err != nil {
		return err
	}

	fmt.Println("\nDigital employee updated successfully!")
	fmt.Printf("Request ID: %s\n", tea.StringValue(result.Body.RequestId))

	return nil
}

func executeEmployeeDelete(client *sopchat.Client, cmd *cobra.Command, args []string) error {
	if employeeName == "" {
		return fmt.Errorf("employee name is required (use -e flag)")
	}

	// 确认删除
	confirmation := readInput(fmt.Sprintf("Are you sure you want to delete '%s'? (yes/no)", employeeName), "no")
	if strings.ToLower(confirmation) != "yes" {
		fmt.Println("Deletion cancelled.")
		return nil
	}

	err := client.DeleteEmployee(employeeName)
	if err != nil {
		return err
	}

	fmt.Printf("Digital employee '%s' deleted successfully!\n", employeeName)
	return nil
}
