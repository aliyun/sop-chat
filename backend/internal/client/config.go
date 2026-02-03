package client

import (
	"fmt"
	"os"

	"sop-chat/internal/config"

	"github.com/joho/godotenv"
)

// Config 存储客户端配置
type Config struct {
	AccessKeyId     string
	AccessKeySecret string
	Endpoint        string
}

// LoadConfig 从统一配置文件或环境变量加载配置
// 优先使用 config.yaml，如果不存在则回退到环境变量
func LoadConfig() (*Config, error) {
	// 首先尝试从统一配置文件加载
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config.yaml"
	}
	
	unifiedConfig, _, err := config.LoadConfig(configPath)
	if err == nil {
		// 成功加载统一配置
		clientConfig, err := unifiedConfig.ToClientConfig()
		if err != nil {
			return nil, fmt.Errorf("从统一配置转换客户端配置失败: %w", err)
		}
		return &Config{
			AccessKeyId:     clientConfig.AccessKeyId,
			AccessKeySecret: clientConfig.AccessKeySecret,
			Endpoint:        clientConfig.Endpoint,
		}, nil
	}

	// 如果统一配置文件不存在，回退到环境变量方式
	// 尝试加载 .env 文件（如果之前没有加载过）
	// 使用 Load() 而不是 Overload()，避免覆盖已存在的环境变量
	// 注意：程序入口应该已经加载过 .env，这里只是作为后备
	if err := godotenv.Load(); err != nil {
		// .env 文件不存在时忽略错误，使用系统环境变量
	}

	// 从环境变量读取配置
	accessKeyId := os.Getenv("ACCESS_KEY_ID")
	accessKeySecret := os.Getenv("ACCESS_KEY_SECRET")
	endpoint := os.Getenv("CMS_ENDPOINT")

	// 验证必需的环境变量
	if accessKeyId == "" {
		return nil, fmt.Errorf("ACCESS_KEY_ID 未配置（请在 config.yaml 或环境变量中设置）")
	}
	if accessKeySecret == "" {
		return nil, fmt.Errorf("ACCESS_KEY_SECRET 未配置（请在 config.yaml 或环境变量中设置）")
	}

	return &Config{
		AccessKeyId:     accessKeyId,
		AccessKeySecret: accessKeySecret,
		Endpoint:        endpoint,
	}, nil
}
