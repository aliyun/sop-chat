package auth

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// YAMLConfig YAML 配置文件结构（legacy 路径使用）
type YAMLConfig struct {
	Method string       `yaml:"method"` // legacy: "local" 等价于 "builtin"
	Local  *LocalConfig `yaml:"local,omitempty"`
	LDAP   *LDAPConfig  `yaml:"ldap,omitempty"`
	OIDC   *OIDCConfig  `yaml:"oidc,omitempty"`
}

// LocalConfig 内置用户配置（由 config 包注入，不直接映射 YAML）
type LocalConfig struct {
	PasswordSalt string       // 密码加盐：MD5(salt+password)，空则不加盐
	Users        []UserConfig `yaml:"user"`
	Roles        []RoleConfig `yaml:"roles"`
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

// LDAPConfig LDAP 认证配置
type LDAPConfig struct {
	Host         string `yaml:"host"`
	Port         int    `yaml:"port"`
	UseTLS       bool   `yaml:"useTLS"`
	BindDN       string `yaml:"bindDN"`
	BindPassword string `yaml:"bindPassword"`
	BaseDN       string `yaml:"baseDN"`
	UserFilter   string `yaml:"userFilter"`
	UsernameAttr string `yaml:"usernameAttr"`
	DisplayAttr  string `yaml:"displayAttr"`
	EmailAttr    string `yaml:"emailAttr"`
}

// OIDCConfig OIDC / OAuth2 认证配置
type OIDCConfig struct {
	IssuerURL     string   `yaml:"issuerURL"`
	ClientID      string   `yaml:"clientId"`
	ClientSecret  string   `yaml:"clientSecret"`
	RedirectURL   string   `yaml:"redirectURL"`
	Scopes        []string `yaml:"scopes,omitempty"`
	UsernameClaim string   `yaml:"usernameClaim,omitempty"`
}

// LoadYAMLConfig 从文件加载 YAML 配置
// 返回配置和实际找到的文件路径
func LoadYAMLConfig(configPath string) (*YAMLConfig, string, error) {
	originalPath := configPath

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
			// 如果配置路径包含 backend/，也尝试直接的文件名
			filepath.Base(configPath),                    // 只取文件名
			filepath.Join(wd, filepath.Base(configPath)), // 当前目录/文件名
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
			return nil, "", fmt.Errorf("配置文件不存在: %s (已尝试: %v)", originalPath, possiblePaths)
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
		return nil, configPath, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var config YAMLConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, configPath, fmt.Errorf("解析配置文件失败: %w", err)
	}

	return &config, configPath, nil
}
