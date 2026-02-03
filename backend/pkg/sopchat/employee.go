package sopchat

import (
	"fmt"
	"strings"

	cmsclient "github.com/alibabacloud-go/cms-20240330/v6/client"
	"github.com/alibabacloud-go/tea/tea"
)

// ListEmployees 列出数字员工
func (c *Client) ListEmployees() ([]*cmsclient.ListDigitalEmployeesResponseBodyDigitalEmployees, error) {
	request := &cmsclient.ListDigitalEmployeesRequest{
		Tags: []*cmsclient.Tag{
			{
				Key:   tea.String("domain"),
				Value: tea.String("sop"),
			},
		},
	}

	result, err := c.CmsClient.ListDigitalEmployees(request)
	if err != nil {
		return nil, fmt.Errorf("failed to list employees: %w", err)
	}

	if result.Body == nil || result.Body.DigitalEmployees == nil {
		return []*cmsclient.ListDigitalEmployeesResponseBodyDigitalEmployees{}, nil
	}

	employees := result.Body.DigitalEmployees

	return employees, nil
}

// GetEmployee 获取指定数字员工的详细信息
func (c *Client) GetEmployee(name string) (*cmsclient.GetDigitalEmployeeResponse, error) {
	result, err := c.CmsClient.GetDigitalEmployee(tea.String(name))
	if err != nil {
		return nil, fmt.Errorf("failed to get employee: %w", err)
	}

	return result, nil
}

// CreateEmployee 创建数字员工
func (c *Client) CreateEmployee(config *EmployeeConfig) (*cmsclient.CreateDigitalEmployeeResponse, error) {
	request := &cmsclient.CreateDigitalEmployeeRequest{
		Name:        tea.String(config.Name),
		DisplayName: tea.String(config.DisplayName),
		Tags: []*cmsclient.Tag{
			{
				Key:   tea.String("domain"),
				Value: tea.String("sop"),
			},
		},
	}

	if config.Description != "" {
		request.Description = tea.String(config.Description)
	}

	if config.DefaultRule != "" {
		request.DefaultRule = tea.String(config.DefaultRule)
	}

	if config.RoleArn != "" {
		// 构建完整的 RoleARN
		// 如果用户提供的是完整的 ARN（包含 acs:ram::），直接使用
		// 如果只是角色名，则构建为 acs:ram::<accountId>:role/<roleName>
		roleArn := config.RoleArn
		if !strings.HasPrefix(roleArn, "acs:ram::") {
			// 获取账号ID
			accountId, err := GetAccountId(c.AccessKeyId, c.AccessKeySecret)
			if err != nil {
				return nil, fmt.Errorf("failed to get account id: %w", err)
			}
			// 构建完整的 ARN
			roleArn = fmt.Sprintf("acs:ram::%s:role/%s", accountId, roleArn)
		}
		request.RoleArn = tea.String(roleArn)
	}

	if len(config.SopKnowledges) > 0 {
		request.Knowledges = &cmsclient.CreateDigitalEmployeeRequestKnowledges{
			Sop: config.SopKnowledges,
		}
	}

	result, err := c.CmsClient.CreateDigitalEmployee(request)
	if err != nil {
		return nil, fmt.Errorf("failed to create employee: %w", err)
	}

	return result, nil
}

// UpdateEmployee 更新数字员工
func (c *Client) UpdateEmployee(config *EmployeeConfig) (*cmsclient.UpdateDigitalEmployeeResponse, error) {
	request := &cmsclient.UpdateDigitalEmployeeRequest{}

	if config.DisplayName != "" {
		request.DisplayName = tea.String(config.DisplayName)
	}

	if config.Description != "" {
		request.Description = tea.String(config.Description)
	}

	if config.DefaultRule != "" {
		request.DefaultRule = tea.String(config.DefaultRule)
	}

	if config.RoleArn != "" {
		// RoleArn 是必需的参数，即使不修改也需要传递
		// 如果用户提供的是完整的 ARN（包含 acs:ram::），直接使用
		// 如果只是角色名，则构建为 acs:ram::<accountId>:role/<roleName>
		roleArn := config.RoleArn
		if !strings.HasPrefix(roleArn, "acs:ram::") {
			// 获取账号ID
			accountId, err := GetAccountId(c.AccessKeyId, c.AccessKeySecret)
			if err != nil {
				return nil, fmt.Errorf("failed to get account id: %w", err)
			}
			// 构建完整的 ARN
			roleArn = fmt.Sprintf("acs:ram::%s:role/%s", accountId, roleArn)
		}
		request.RoleArn = tea.String(roleArn)
	}

	if len(config.SopKnowledges) > 0 {
		request.Knowledges = &cmsclient.UpdateDigitalEmployeeRequestKnowledges{
			Sop: config.SopKnowledges,
		}
	}

	result, err := c.CmsClient.UpdateDigitalEmployee(tea.String(config.Name), request)
	if err != nil {
		return nil, fmt.Errorf("failed to update employee: %w", err)
	}

	return result, nil
}

// DeleteEmployee 删除数字员工
func (c *Client) DeleteEmployee(name string) error {
	_, err := c.CmsClient.DeleteDigitalEmployee(tea.String(name))
	if err != nil {
		return fmt.Errorf("failed to delete employee: %w", err)
	}

	return nil
}
