package client

import (
	"fmt"

	"sop-chat/pkg/sopchat"

	cmsclient "github.com/alibabacloud-go/cms-20240330/v6/client"
	openapiutil "github.com/alibabacloud-go/darabonba-openapi/v2/utils"
	"github.com/alibabacloud-go/tea/tea"
)

// NewCMSClient 创建并返回配置好的 CMS 客户端
func NewCMSClient(config *Config) (*sopchat.Client, error) {
	// 创建客户端配置
	cmsConfig := &openapiutil.Config{
		AccessKeyId:     tea.String(config.AccessKeyId),
		AccessKeySecret: tea.String(config.AccessKeySecret),
		Endpoint:        tea.String(config.Endpoint),
	}

	// 创建 CMS 客户端
	cmsClient, err := cmsclient.NewClient(cmsConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create CMS client: %w", err)
	}

	return &sopchat.Client{
		CmsClient:       cmsClient,
		AccessKeyId:     config.AccessKeyId,
		AccessKeySecret: config.AccessKeySecret,
		Endpoint:        config.Endpoint,
	}, nil
}

// SetupClient 是一个便捷函数，加载配置并创建客户端
func SetupClient() (*sopchat.Client, error) {
	config, err := LoadConfig()
	if err != nil {
		return nil, err
	}

	return NewCMSClient(config)
}
