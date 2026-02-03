package sopchat

import (
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	sts20150401 "github.com/alibabacloud-go/sts-20150401/v2/client"
	util "github.com/alibabacloud-go/tea-utils/v2/service"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/aliyun/credentials-go/credentials"
	credential "github.com/aliyun/credentials-go/credentials"
)

// createStsClient 创建 STS 客户端
func createStsClient(accessKeyId string, accessKeySecret string) (*sts20150401.Client, error) {
	cred, err := credential.NewCredential(
		&credentials.Config{
			Type:            tea.String("access_key"),
			AccessKeyId:     tea.String(accessKeyId),
			AccessKeySecret: tea.String(accessKeySecret),
		},
	)
	if err != nil {
		return nil, err
	}

	config := &openapi.Config{
		Credential: cred,
	}
	config.Endpoint = tea.String("sts.cn-hangzhou.aliyuncs.com")

	result, err := sts20150401.NewClient(config)
	return result, err
}

// GetAccountId 获取阿里云账号ID
func GetAccountId(accessKeyId string, accessKeySecret string) (string, error) {
	client, err := createStsClient(accessKeyId, accessKeySecret)
	if err != nil {
		return "", err
	}

	runtime := &util.RuntimeOptions{}

	resp, err := client.GetCallerIdentityWithOptions(runtime)
	if err != nil {
		return "", err
	}
	return *resp.Body.AccountId, nil
}
