package sopchat

import (
	cmsclient "github.com/alibabacloud-go/cms-20240330/v6/client"
)

// SopKnowledge 表示 SOP 知识配置
type SopKnowledge struct {
	Type           string `json:"type"` // oss, yunxiao, builtin
	Region         string `json:"region,omitempty"`
	Bucket         string `json:"bucket,omitempty"`
	BasePath       string `json:"basePath,omitempty"`
	Description    string `json:"description,omitempty"`
	OrganizationId string `json:"organizationId,omitempty"`
	RepositoryId   string `json:"repositoryId,omitempty"`
	BranchName     string `json:"branchName,omitempty"`
	Token          string `json:"token,omitempty"`
	ID             string `json:"id,omitempty"`
}

// EmployeeConfig 表示数字员工配置
type EmployeeConfig struct {
	Name          string                   `json:"name"`
	DisplayName   string                   `json:"displayName"`
	Description   string                   `json:"description,omitempty"`
	DefaultRule   string                   `json:"defaultRule,omitempty"`
	RoleArn       string                   `json:"roleArn,omitempty"`
	SopKnowledges []map[string]interface{} `json:"sopKnowledges,omitempty"`
}

// ThreadConfig 表示会话线程配置
type ThreadConfig struct {
	EmployeeName string                 `json:"employeeName"`
	Title        string                 `json:"title,omitempty"`
	Attributes   map[string]interface{} `json:"attributes,omitempty"`
}

// ChatConfig 表示聊天配置
type ChatConfig struct {
	EmployeeName   string `json:"employeeName"`
	ThreadId       string `json:"threadId,omitempty"`
	Message        string `json:"message"`
	ShowToolResult bool   `json:"showToolResult"`
}

// Client 封装了与 CMS 服务交互的客户端
type Client struct {
	CmsClient       *cmsclient.Client
	AccessKeyId     string
	AccessKeySecret string
	Endpoint        string
	RoleArn         string
}

// ChatMessage 表示聊天消息
type ChatMessage struct {
	Role    string                   `json:"role"`
	Content string                   `json:"content"`
	Tools   []map[string]interface{} `json:"tools,omitempty"`
}

// ThreadFilter 表示线程过滤条件
type ThreadFilter struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
