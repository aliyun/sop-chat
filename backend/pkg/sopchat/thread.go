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
		attrs := make(map[string]*string, len(config.Attributes))
		for k, v := range config.Attributes {
			if strVal, ok := v.(string); ok {
				attrs[k] = tea.String(strVal)
			}
		}
		if len(attrs) > 0 {
			request.Attributes = attrs
		}
	}

	if config.Project != "" || config.Workspace != "" {
		variables := &cmsclient.CreateThreadRequestVariables{}
		if config.Project != "" {
			variables.Project = tea.String(config.Project)
		}
		if config.Workspace != "" {
			variables.Workspace = tea.String(config.Workspace)
		}
		request.Variables = variables
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
