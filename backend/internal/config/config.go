package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Config 统一配置结构
type Config struct {
	// 全局配置
	Global GlobalConfig `yaml:"global"`

	// 认证配置
	Auth AuthConfig `yaml:"auth"`
}

// GlobalConfig 全局配置
type GlobalConfig struct {
	AccessKeyId     string `yaml:"accessKeyId"`
	AccessKeySecret string `yaml:"accessKeySecret"`
	Endpoint        string `yaml:"endpoint"`
	Port            int    `yaml:"port"`     // 服务监听端口
	TimeZone        string `yaml:"timeZone"` // 时区设置
	Language        string `yaml:"language"` // 语言设置
}

// AuthConfig 认证配置
type AuthConfig struct {
	Method       string       `yaml:"method"`
	JWTSecretKey string       `yaml:"jwtSecretKey"`
	JWTExpiresIn string       `yaml:"jwtExpiresIn"` // 例如: "24h", "1h"
	DataDir      string       `yaml:"dataDir"`
	Local        *LocalConfig `yaml:"local,omitempty"`
	LDAP         *LDAPConfig  `yaml:"ldap,omitempty"`
	OIDC         *OIDCConfig  `yaml:"oidc,omitempty"`
}

// LocalConfig 本地认证配置
type LocalConfig struct {
	Users []UserConfig `yaml:"user"`
	Roles []RoleConfig `yaml:"roles"`
}

// UserConfig 用户配置
type UserConfig struct {
	Name     string `yaml:"name"`
	Password string `yaml:"password"` // MD5 哈希后的密码
}

// RoleConfig 角色配置
type RoleConfig struct {
	Name  string   `yaml:"name"`
	Users []string `yaml:"user"`
}

// LDAPConfig LDAP 配置（未来支持）
type LDAPConfig struct {
	// TODO: 实现 LDAP 配置
}

// OIDCConfig OIDC 配置（未来支持）
type OIDCConfig struct {
	// TODO: 实现 OIDC 配置
}

// LoadConfig 从文件加载统一配置
// 返回配置和实际找到的文件路径
func LoadConfig(configPath string) (*Config, string, error) {
	originalPath := configPath

	// 如果路径为空，使用默认路径
	if configPath == "" {
		configPath = "config.yaml"
	}

	// 如果路径是相对路径，尝试从多个位置查找
	if !filepath.IsAbs(configPath) {
		// 获取当前工作目录
		wd, _ := os.Getwd()

		// 尝试从多个位置查找
		possiblePaths := []string{
			configPath,                                     // 原始路径
			filepath.Join(".", configPath),                 // ./xxx
			filepath.Join(wd, configPath),                  // 当前目录/xxx
			filepath.Join("backend", configPath),           // backend/xxx
			filepath.Join(wd, "backend", configPath),       // 当前目录/backend/xxx
			filepath.Join(wd, "..", "backend", configPath), // 上级目录/backend/xxx
			filepath.Base(configPath),                      // 只取文件名
			filepath.Join(wd, filepath.Base(configPath)),   // 当前目录/文件名
		}

		var foundPath string
		for _, path := range possiblePaths {
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				// 转换为绝对路径
				absPath, err := filepath.Abs(path)
				if err == nil {
					foundPath = absPath
					break
				}
				foundPath = path
				break
			}
		}

		if foundPath == "" {
			return nil, "", fmt.Errorf("config file not found: %s (tried: %v)", originalPath, possiblePaths)
		}
		configPath = foundPath
	} else {
		// 如果是绝对路径，也转换为标准化的绝对路径
		absPath, err := filepath.Abs(configPath)
		if err == nil {
			configPath = absPath
		}
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, configPath, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, configPath, fmt.Errorf("failed to parse config file: %w", err)
	}

	// 解析环境变量引用
	config.expandEnvVars()

	return &config, configPath, nil
}

// expandEnvVars 展开配置中的环境变量引用
// 支持格式: $VAR 或 ${VAR}
func (c *Config) expandEnvVars() {
	// 展开 Global 配置中的环境变量
	c.Global.AccessKeyId = expandEnvVar(c.Global.AccessKeyId)
	c.Global.AccessKeySecret = expandEnvVar(c.Global.AccessKeySecret)
	c.Global.Endpoint = expandEnvVar(c.Global.Endpoint)

	// 展开 Auth 配置中的环境变量
	c.Auth.JWTSecretKey = expandEnvVar(c.Auth.JWTSecretKey)
	c.Auth.JWTExpiresIn = expandEnvVar(c.Auth.JWTExpiresIn)
	c.Auth.DataDir = expandEnvVar(c.Auth.DataDir)
}

// expandEnvVar 展开单个字符串中的环境变量引用
// 支持格式: $VAR 或 ${VAR}
// 如果变量不存在，保持原样（不替换）
func expandEnvVar(value string) string {
	if value == "" {
		return value
	}

	// 匹配 ${VAR} 格式
	re1 := regexp.MustCompile(`\$\{([^}]+)\}`)
	value = re1.ReplaceAllStringFunc(value, func(match string) string {
		varName := match[2 : len(match)-1] // 提取 ${} 中的变量名
		if envValue := os.Getenv(varName); envValue != "" {
			return envValue
		}
		return match // 如果环境变量不存在，保持原样
	})

	// 匹配 $VAR 格式（但不匹配 ${VAR}，因为已经被处理过了）
	// 使用单词边界来匹配 $VAR，避免匹配到 ${VAR} 的一部分
	re2 := regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)`)
	value = re2.ReplaceAllStringFunc(value, func(match string) string {
		varName := match[1:] // 提取 $ 后的变量名
		if envValue := os.Getenv(varName); envValue != "" {
			return envValue
		}
		return match // 如果环境变量不存在，保持原样
	})

	return value
}

// ToClientConfig 转换为客户端配置
// 优先使用配置文件中的值，如果为空则从环境变量读取
// 配置文件中的环境变量引用（$VAR 或 ${VAR}）会在加载时自动展开
func (c *Config) ToClientConfig() (*ClientConfig, error) {
	// 使用配置文件中的值（环境变量引用已在加载时展开）
	// 如果仍然为空，则从环境变量读取（作为后备）
	accessKeyId := c.Global.AccessKeyId
	if accessKeyId == "" {
		accessKeyId = os.Getenv("ACCESS_KEY_ID")
	}

	accessKeySecret := c.Global.AccessKeySecret
	if accessKeySecret == "" {
		accessKeySecret = os.Getenv("ACCESS_KEY_SECRET")
	}

	endpoint := c.Global.Endpoint
	if endpoint == "" {
		endpoint = os.Getenv("CMS_ENDPOINT")
	}

	// 验证必需的配置
	if accessKeyId == "" {
		return nil, fmt.Errorf("ACCESS_KEY_ID not configured (please set it in config.yaml's global.accessKeyId or ACCESS_KEY_ID environment variable)")
	}
	if accessKeySecret == "" {
		return nil, fmt.Errorf("ACCESS_KEY_SECRET not configured (please set it in config.yaml's global.accessKeySecret or ACCESS_KEY_SECRET environment variable)")
	}

	return &ClientConfig{
		AccessKeyId:     accessKeyId,
		AccessKeySecret: accessKeySecret,
		Endpoint:        endpoint,
	}, nil
}

// GetAuthConfig 获取认证配置（返回原始配置结构，由 auth 包转换）
func (c *Config) GetAuthConfig() *AuthConfig {
	return &c.Auth
}

// GetYAMLConfig 获取 YAML 配置（用于兼容旧的 YAML 配置加载方式）
func (c *Config) GetYAMLConfig() *YAMLConfigForAuth {
	if c.Auth.Local == nil {
		return nil
	}
	return &YAMLConfigForAuth{
		Method: c.Auth.Method,
		Local:  c.Auth.Local,
		LDAP:   c.Auth.LDAP,
		OIDC:   c.Auth.OIDC,
	}
}

// YAMLConfigForAuth 用于传递给 auth 包的配置结构
type YAMLConfigForAuth struct {
	Method string
	Local  *LocalConfig
	LDAP   *LDAPConfig
	OIDC   *OIDCConfig
}

// ClientConfig 客户端配置（兼容原有结构）
type ClientConfig struct {
	AccessKeyId     string
	AccessKeySecret string
	Endpoint        string
}

// GetPort 获取端口配置（优先级: 配置文件 > 环境变量 > 默认值）
// 返回端口号（int），如果未配置则返回 0
func (c *Config) GetPort() int {
	port := c.Global.Port
	if port == 0 {
		// 尝试从环境变量读取
		portStr := os.Getenv("PORT")
		if portStr != "" {
			// 解析环境变量中的端口号
			if parsedPort, err := strconv.Atoi(portStr); err == nil {
				port = parsedPort
			}
		}
		// 如果仍然为 0，使用默认值
		if port == 0 {
			port = 8080
		}
	}
	return port
}

// GetTimeZone 获取时区配置（如果未配置则返回默认值 "Asia/Shanghai"）
func (c *Config) GetTimeZone() string {
	if c.Global.TimeZone == "" {
		return "Asia/Shanghai"
	}
	return c.Global.TimeZone
}

// GetLanguage 获取语言配置（如果未配置则返回默认值 "zh"）
func (c *Config) GetLanguage() string {
	if c.Global.Language == "" {
		return "zh"
	}
	return c.Global.Language
}
