package sopchat

import (
	"fmt"

	cmsclient "github.com/alibabacloud-go/cms-20240330/v6/client"
	"github.com/alibabacloud-go/tea/tea"
)

// CreateThread 创建会话线程
func (c *Client) CreateThread(config *ThreadConfig) (*cmsclient.CreateThreadResponse, error) {
	request := &cmsclient.CreateThreadRequest{}

	if config.Title != "" {
		request.Title = tea.String(config.Title)
	}

	if len(config.Attributes) > 0 {
		// 转换 map[string]interface{} 为 Variables 结构
		// 注意：CreateThreadRequestVariables 结构体只支持 project 和 workspace 字段
		// 其他属性（如 user）会被保留在 config.Attributes 中，但可能无法通过 Variables 传递到 API
		variables := &cmsclient.CreateThreadRequestVariables{}
		hasVariables := false
		
		if projectVal, ok := config.Attributes["project"]; ok {
			if strVal, ok := projectVal.(string); ok {
				variables.Project = tea.String(strVal)
				hasVariables = true
			}
		}
		if workspaceVal, ok := config.Attributes["workspace"]; ok {
			if strVal, ok := workspaceVal.(string); ok {
				variables.Workspace = tea.String(strVal)
				hasVariables = true
			}
		}
		
		// 注意：user 属性会被添加到 config.Attributes 中（在 API 层）
		// 但由于 CreateThreadRequestVariables 结构体限制，可能无法通过 Variables 传递
		// 如果 API 实际支持 user 字段，需要更新 SDK 或使用其他方式传递
		
		// 只有当至少有一个字段被设置时才设置 Variables
		if hasVariables {
			request.Variables = variables
		}
	}

	result, err := c.CmsClient.CreateThread(tea.String(config.EmployeeName), request)
	if err != nil {
		return nil, fmt.Errorf("failed to create thread: %w", err)
	}

	return result, nil
}

// ListThreads 列出会话线程
func (c *Client) ListThreads(employeeName string, filters []ThreadFilter) (*cmsclient.ListThreadsResponse, error) {
	request := &cmsclient.ListThreadsRequest{}

	// 添加过滤条件
	if len(filters) > 0 {
		var filterList []*cmsclient.ListThreadsRequestFilter
		for _, f := range filters {
			filterList = append(filterList, &cmsclient.ListThreadsRequestFilter{
				Key:   tea.String(f.Key),
				Value: tea.String(f.Value),
			})
		}
		request.Filter = filterList
	}

	result, err := c.CmsClient.ListThreads(tea.String(employeeName), request)
	if err != nil {
		return nil, fmt.Errorf("failed to list threads: %w", err)
	}

	return result, nil
}

// GetThread 获取线程详细信息
// 注意：employeeName 参数是必需的，应该从 threadId 关联的 employee 获取
func (c *Client) GetThread(employeeName string, threadId string) (*cmsclient.GetThreadResponse, error) {
	result, err := c.CmsClient.GetThread(tea.String(employeeName), tea.String(threadId))
	if err != nil {
		return nil, fmt.Errorf("failed to get thread: %w", err)
	}

	return result, nil
}

// GetThreadData 获取线程消息数据
// 注意：employeeName 参数是必需的
func (c *Client) GetThreadData(employeeName string, threadId string) (*cmsclient.GetThreadDataResponse, error) {
	request := &cmsclient.GetThreadDataRequest{}

	result, err := c.CmsClient.GetThreadData(tea.String(employeeName), tea.String(threadId), request)
	if err != nil {
		return nil, fmt.Errorf("failed to get thread data: %w", err)
	}

	return result, nil
}
