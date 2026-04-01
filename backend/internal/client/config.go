package client

import (
	"os"

	"sop-chat/internal/config"

	"github.com/joho/godotenv"
)

// Config 存储客户端配置
type Config struct {
	CloudAccountID string
	AccessKeyId     string
	AccessKeySecret string
	Endpoint        string
}

// LoadConfig 从统一配置文件或环境变量加载配置
// 优先使用 config.yaml，如果不存在则回退到环境变量。
// 凭据未配置时返回空 Config（不报错），调用方应在实际发请求时检查是否已配置。
func LoadConfig() (*Config, error) {
	// 首先尝试从统一配置文件加载
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config.yaml"
	}

	unifiedConfig, _, err := config.LoadConfig(configPath)
	if err == nil {
		// 成功加载统一配置，ToClientConfig 可能因凭据缺失而报错，此时降级为空 Config
		clientConfig, err := unifiedConfig.ToClientConfig()
		if err != nil {
			// 配置文件存在但凭据未填写，返回空配置而非错误
			return &Config{}, nil
		}
		return &Config{
			CloudAccountID: clientConfig.CloudAccountID,
			AccessKeyId:    clientConfig.AccessKeyId,
			AccessKeySecret: clientConfig.AccessKeySecret,
			Endpoint:        clientConfig.Endpoint,
		}, nil
	}

	// 配置文件不存在，尝试加载 .env 并读取环境变量（作为后备）
	_ = godotenv.Load() // .env 不存在时忽略错误

	accessKeyId := os.Getenv("ACCESS_KEY_ID")
	accessKeySecret := os.Getenv("ACCESS_KEY_SECRET")
	endpoint := os.Getenv("CMS_ENDPOINT")

	// 凭据未配置时也正常返回（空配置），服务照常启动
	return &Config{
		CloudAccountID: config.DefaultCloudAccountID,
		AccessKeyId:     accessKeyId,
		AccessKeySecret: accessKeySecret,
		Endpoint:        endpoint,
	}, nil
}
