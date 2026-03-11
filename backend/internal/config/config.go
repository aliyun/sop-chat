package config

import (
	"crypto/rand"
	"encoding/hex"
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

	// 通知渠道配置（可选）：钉钉、企业微信、Slack 等
	Channels *ChannelsConfig `yaml:"channels,omitempty"`

	// OpenAI 兼容接口配置（可选）
	OpenAI *OpenAICompatConfig `yaml:"openai,omitempty"`
}

// ChannelsConfig 通知渠道配置（聚合所有 IM/消息渠道）
// 新增渠道时在此结构体中追加字段即可，无需修改顶层 Config
type ChannelsConfig struct {
	// 支持多个钉钉机器人，每个对接一个数字员工（clientId 唯一标识一个实例）
	DingTalk []DingTalkConfig `yaml:"dingtalk,omitempty"`
	// 飞书机器人（WebSocket 长连接，无需公网 IP）
	Feishu []FeishuConfig `yaml:"feishu,omitempty"`
	// 企业微信应用（HTTP 回调，需要公网 IP 或内网穿透）
	WeCom []WeComConfig `yaml:"wecom,omitempty"`
	// 企业微信群聊机器人（WebSocket 长连接，从 AI 助手页面获取凭据）
	WeComBot []WeComBotConfig `yaml:"wecomBot,omitempty"`
}

// OpenAICompatConfig OpenAI 兼容接口配置
type OpenAICompatConfig struct {
	// 是否启用 API Key 鉴权；false 时保留配置但不校验 key
	Enabled bool `yaml:"enabled"`
	// API 密钥列表，客户端通过 Authorization: Bearer <key> 方式认证
	APIKeys []string `yaml:"apiKeys,omitempty"`
}

// ConversationRoute 群名称路由规则：将特定群的消息路由到指定数字员工
type ConversationRoute struct {
	ConversationTitle string `yaml:"conversationTitle"` // 群名称（精确匹配）
	EmployeeName      string `yaml:"employeeName"`      // 路由到的数字员工
}

// DingTalkConfig 钉钉机器人配置
type DingTalkConfig struct {
	// 是否启用钉钉机器人；false 时保留配置但不启动 Stream 连接
	Enabled      bool   `yaml:"enabled"`
	Name         string `yaml:"name,omitempty"` // 机器人显示名称（仅用于标识，不影响功能）
	ClientId     string `yaml:"clientId"`        // 企业内部应用 AppKey（唯一标识）
	ClientSecret string `yaml:"clientSecret"`    // 企业内部应用 AppSecret
	EmployeeName string `yaml:"employeeName"`    // 默认数字员工名称
	// 开启后，发送给大模型的消息会附加精简指令，要求回复简短、适合 IM 阅读
	ConciseReply bool `yaml:"conciseReply,omitempty"`
	// 群用户白名单（钉钉 senderNick）；限制群聊中可 @ 机器人提问的用户；为空时允许所有群成员
	AllowedGroupUsers []string `yaml:"allowedGroupUsers,omitempty"`
	// 单聊用户白名单（钉钉 senderNick）；限制可与机器人单聊的用户；为空时允许所有人单聊
	AllowedDirectUsers []string `yaml:"allowedDirectUsers,omitempty"`
	// 群白名单（conversationTitle）；为空时允许所有群；有值时仅允许列出的群，单聊不受此限制
	AllowedConversations []string `yaml:"allowedConversations,omitempty"`
	// 群名称路由：按群名将消息路由到不同的数字员工；匹配不到时使用顶层 employeeName
	ConversationRoutes []ConversationRoute `yaml:"conversationRoutes,omitempty"`
}

// FeishuConfig 飞书机器人配置
type FeishuConfig struct {
	// 是否启用飞书机器人
	Enabled bool `yaml:"enabled"`
	// 机器人显示名称（仅用于标识）
	Name string `yaml:"name,omitempty"`
	// 飞书应用 App ID
	AppID string `yaml:"appId"`
	// 飞书应用 App Secret
	AppSecret string `yaml:"appSecret"`
	// 验证令牌（WebSocket 模式可留空）
	VerificationToken string `yaml:"verificationToken,omitempty"`
	// 消息加密密钥（WebSocket 模式可留空）
	EventEncryptKey string `yaml:"eventEncryptKey,omitempty"`
	// 默认数字员工名称
	EmployeeName string `yaml:"employeeName"`
	// 开启后回复简短，适合 IM 阅读
	ConciseReply bool `yaml:"conciseReply,omitempty"`
	// 用户白名单（飞书 open_id）；为空时允许所有用户
	AllowedUsers []string `yaml:"allowedUsers,omitempty"`
	// 群聊白名单（飞书 chat_id）；为空时允许所有群聊
	AllowedChats []string `yaml:"allowedChats,omitempty"`
}

// WeComConfig 企业微信机器人配置
type WeComConfig struct {
	// 是否启用企业微信机器人
	Enabled bool `yaml:"enabled"`
	// 机器人显示名称（仅用于标识）
	Name string `yaml:"name,omitempty"`
	// 企业 ID
	CorpID string `yaml:"corpId"`
	// 应用 AgentID
	AgentID int `yaml:"agentId"`
	// 应用 Secret
	Secret string `yaml:"secret"`
	// 回调 Token（用于验证消息来源）
	Token string `yaml:"token"`
	// 消息加密密钥（43 位字符）
	EncodingAESKey string `yaml:"encodingAESKey"`
	// 回调服务端口（默认 8090，与主服务分开）
	CallbackPort int `yaml:"callbackPort,omitempty"`
	// 回调路径（必须以 /wecom/ 开头，默认 /wecom/callback）
	CallbackPath string `yaml:"callbackPath,omitempty"`
	// 默认数字员工名称
	EmployeeName string `yaml:"employeeName"`
	// 开启后回复简短，适合 IM 阅读
	ConciseReply bool `yaml:"conciseReply,omitempty"`
	// 用户白名单（企业微信 userid）；为空时允许所有用户
	AllowedUsers []string `yaml:"allowedUsers,omitempty"`
	// 群机器人 Webhook URL（可选）；配置后 AI 回复会同步推送到群聊
	WebhookURL string `yaml:"webhookUrl,omitempty"`
	// AI 助手群机器人长连接配置（从企业微信管理后台 AI 助手页面获取）
	BotLongConn *WeComBotLongConnConfig `yaml:"botLongConn,omitempty"`
}

// WeComBotLongConnConfig 企业微信 AI 助手群机器人长连接配置（旧结构，保留用于向后兼容读取）
type WeComBotLongConnConfig struct {
	// 是否启用长连接群机器人
	Enabled bool `yaml:"enabled"`
	// 机器人 ID（从企业微信管理后台 AI 助手页面获取）
	BotID string `yaml:"botId"`
	// 机器人 Secret（从企业微信管理后台 AI 助手页面获取）
	BotSecret string `yaml:"botSecret"`
	// WebSocket 连接地址（默认 wss://openws.work.weixin.qq.com）
	URL string `yaml:"url,omitempty"`
	// 心跳间隔（秒，默认 30）
	PingIntervalSec int `yaml:"pingIntervalSec,omitempty"`
	// 重连延迟（秒，默认 5）
	ReconnectDelaySec int `yaml:"reconnectDelaySec,omitempty"`
	// 最大重连延迟（秒，默认 60）
	MaxReconnectDelaySec int `yaml:"maxReconnectDelaySec,omitempty"`
}

// WeComBotConfig 企业微信群聊机器人配置（独立渠道，从 WeComConfig.BotLongConn 拆出）
type WeComBotConfig struct {
	// 是否启用群聊机器人
	Enabled bool `yaml:"enabled"`
	// 机器人显示名称（仅用于标识，不影响功能）
	Name string `yaml:"name,omitempty"`
	// 机器人 ID（从企业微信管理后台 AI 助手页面获取）
	BotID string `yaml:"botId"`
	// 机器人 Secret（从企业微信管理后台 AI 助手页面获取）
	BotSecret string `yaml:"botSecret"`
	// 默认数字员工名称
	EmployeeName string `yaml:"employeeName"`
	// 开启后回复简短，适合 IM 阅读
	ConciseReply bool `yaml:"conciseReply,omitempty"`
	// WebSocket 连接地址（默认 wss://openws.work.weixin.qq.com）
	URL string `yaml:"url,omitempty"`
	// 心跳间隔（秒，默认 30）
	PingIntervalSec int `yaml:"pingIntervalSec,omitempty"`
	// 重连延迟（秒，默认 5）
	ReconnectDelaySec int `yaml:"reconnectDelaySec,omitempty"`
	// 最大重连延迟（秒，默认 60）
	MaxReconnectDelaySec int `yaml:"maxReconnectDelaySec,omitempty"`
}

// CredsEqual 判断企业微信群聊机器人凭据是否与另一个配置相同
func (wb *WeComBotConfig) CredsEqual(other *WeComBotConfig) bool {
	if other == nil {
		return false
	}
	return wb.BotID == other.BotID &&
		wb.BotSecret == other.BotSecret &&
		wb.EmployeeName == other.EmployeeName &&
		wb.URL == other.URL
}

// CredsEqual 判断凭据和员工名是否与另一个配置相同（用于热重载时判断是否需要重启）
func (d *DingTalkConfig) CredsEqual(other *DingTalkConfig) bool {
	if other == nil {
		return false
	}
	return d.ClientSecret == other.ClientSecret && d.EmployeeName == other.EmployeeName
}

// CredsEqual 判断飞书凭据是否与另一个配置相同
func (f *FeishuConfig) CredsEqual(other *FeishuConfig) bool {
	if other == nil {
		return false
	}
	return f.AppID == other.AppID && f.AppSecret == other.AppSecret && f.EmployeeName == other.EmployeeName
}

// CredsEqual 判断企业微信凭据是否与另一个配置相同
func (w *WeComConfig) CredsEqual(other *WeComConfig) bool {
	if other == nil {
		return false
	}
	if w.CorpID != other.CorpID ||
		w.Secret != other.Secret ||
		w.Token != other.Token ||
		w.EncodingAESKey != other.EncodingAESKey ||
		w.AgentID != other.AgentID ||
		w.CallbackPort != other.CallbackPort ||
		w.CallbackPath != other.CallbackPath ||
		w.EmployeeName != other.EmployeeName {
		return false
	}
	// 比较长连接配置
	wLC := w.BotLongConn
	oLC := other.BotLongConn
	if (wLC == nil) != (oLC == nil) {
		return false
	}
	if wLC != nil && oLC != nil {
		if wLC.Enabled != oLC.Enabled ||
			wLC.BotID != oLC.BotID ||
			wLC.BotSecret != oLC.BotSecret ||
			wLC.URL != oLC.URL {
			return false
		}
	}
	return true
}

// GlobalConfig 全局配置
type GlobalConfig struct {
	AccessKeyId     string `yaml:"accessKeyId"`
	AccessKeySecret string `yaml:"accessKeySecret"`
	Endpoint        string `yaml:"endpoint"`
	Host            string `yaml:"host"` // 服务监听地址，默认 0.0.0.0
	Port            int    `yaml:"port"` // 服务监听端口，默认 8080
	TimeZone        string `yaml:"timeZone"` // 时区设置
	Language        string `yaml:"language"` // 语言设置
}

// AuthConfig 认证配置
// methods 为有序鉴权链，登录时依次尝试直到第一个成功。
// 为空时登录功能关闭，所有受保护 API 均不可访问。
type AuthConfig struct {
	// 鉴权链（有序列表）：builtin | ldap | oidc
	// 为空时登录关闭，提示用户在管理后台配置
	Methods []string  `yaml:"methods"`
	JWT     JWTConfig `yaml:"jwt"`

	// 内置用户密码加盐：stored = MD5(passwordSalt + plaintext)
	// 为空时退化为 MD5(plaintext)，向后兼容
	PasswordSalt string `yaml:"passwordSalt,omitempty"`

	// 内置账号（builtin 方式专用）
	BuiltinUsers []UserConfig `yaml:"builtinUsers,omitempty"`

	// 全局角色定义（所有认证方式共用；LDAP/OIDC 未来按映射规则写入此处）
	Roles []RoleConfig `yaml:"roles,omitempty"`

	LDAP *LDAPConfig `yaml:"ldap,omitempty"`
	OIDC *OIDCConfig `yaml:"oidc,omitempty"`
}

// JWTConfig JWT 令牌配置
type JWTConfig struct {
	SecretKey string `yaml:"secretKey"`
	ExpiresIn string `yaml:"expiresIn"` // 例如: "24h", "1h", "30m"
}

// LocalConfig 本地认证配置（用于 auth 包桥接，不直接映射 YAML）
type LocalConfig struct {
	Users        []UserConfig
	Roles        []RoleConfig
	PasswordSalt string // 从 AuthConfig.PasswordSalt 注入
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
	Host         string `yaml:"host"`           // LDAP 服务器地址，如 ldap.example.com
	Port         int    `yaml:"port"`           // 端口，明文默认 389，TLS 默认 636
	UseTLS       bool   `yaml:"useTLS"`         // 是否使用 TLS（LDAPS）
	BindDN       string `yaml:"bindDN"`         // 查询用 DN，如 cn=readonly,dc=example,dc=com
	BindPassword string `yaml:"bindPassword"`   // 查询用密码
	BaseDN       string `yaml:"baseDN"`         // 用户搜索根，如 ou=people,dc=example,dc=com
	UserFilter   string `yaml:"userFilter"`     // 用户搜索过滤器，如 (uid={username})
	UsernameAttr string `yaml:"usernameAttr"`   // 用户名属性，默认 uid
	DisplayAttr  string `yaml:"displayAttr"`    // 显示名属性，默认 cn
	EmailAttr    string `yaml:"emailAttr"`      // 邮箱属性，默认 mail
}

// OIDCConfig OIDC / OAuth2 认证配置
// 兼容标准 OIDC Provider（Keycloak、Dex、Okta、Azure AD 等）
type OIDCConfig struct {
	IssuerURL    string   `yaml:"issuerURL"`              // Provider 地址，如 https://accounts.example.com
	ClientID     string   `yaml:"clientId"`               // OAuth2 Client ID
	ClientSecret string   `yaml:"clientSecret"`           // OAuth2 Client Secret
	RedirectURL  string   `yaml:"redirectURL"`            // 回调地址，如 http://your-server/api/auth/oidc/callback
	Scopes       []string `yaml:"scopes,omitempty"`       // 默认: [openid, profile, email]
	UsernameClaim string  `yaml:"usernameClaim,omitempty"` // 用于提取用户名的 claim，默认 preferred_username
}

// randomHex 生成 n 字节的随机十六进制字符串
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// 极端情况下随机数生成失败，使用固定占位符并提示用户修改
		return "please-change-this-secret-key"
	}
	return hex.EncodeToString(b)
}

// DefaultConfig 返回一个可直接使用的最小默认配置：
// - 凭据为空（用户需通过配置 UI 填写）
// - auth.methods 为空（登录功能关闭，配置后重启或热重载生效）
// - JWT secretKey 随机生成，避免各实例共用同一密钥
func DefaultConfig() *Config {
	return &Config{
		Global: GlobalConfig{
			Host:     "0.0.0.0",
			Port:     8080,
			Endpoint: "cms.cn-hangzhou.aliyuncs.com",
			TimeZone: "Asia/Shanghai",
			Language: "zh",
		},
		Auth: AuthConfig{
			Methods:      []string{"builtin"},
			PasswordSalt: randomHex(16),
			JWT: JWTConfig{
				SecretKey: randomHex(32),
				ExpiresIn: "24h",
			},
		},
	}
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
	c.Global.Host = expandEnvVar(c.Global.Host)

	// 展开 Auth 配置中的环境变量
	c.Auth.JWT.SecretKey = expandEnvVar(c.Auth.JWT.SecretKey)
	c.Auth.JWT.ExpiresIn = expandEnvVar(c.Auth.JWT.ExpiresIn)
	c.Auth.PasswordSalt = expandEnvVar(c.Auth.PasswordSalt)
	if c.Auth.LDAP != nil {
		c.Auth.LDAP.BindPassword = expandEnvVar(c.Auth.LDAP.BindPassword)
	}
	if c.Auth.OIDC != nil {
		c.Auth.OIDC.ClientSecret = expandEnvVar(c.Auth.OIDC.ClientSecret)
	}

	// 展开渠道配置中的环境变量
	if c.Channels != nil {
		for i := range c.Channels.DingTalk {
			c.Channels.DingTalk[i].ClientId = expandEnvVar(c.Channels.DingTalk[i].ClientId)
			c.Channels.DingTalk[i].ClientSecret = expandEnvVar(c.Channels.DingTalk[i].ClientSecret)
			c.Channels.DingTalk[i].EmployeeName = expandEnvVar(c.Channels.DingTalk[i].EmployeeName)
		}
		for i := range c.Channels.Feishu {
			c.Channels.Feishu[i].AppID = expandEnvVar(c.Channels.Feishu[i].AppID)
			c.Channels.Feishu[i].AppSecret = expandEnvVar(c.Channels.Feishu[i].AppSecret)
			c.Channels.Feishu[i].EmployeeName = expandEnvVar(c.Channels.Feishu[i].EmployeeName)
		}
		for i := range c.Channels.WeCom {
			c.Channels.WeCom[i].CorpID = expandEnvVar(c.Channels.WeCom[i].CorpID)
			c.Channels.WeCom[i].Secret = expandEnvVar(c.Channels.WeCom[i].Secret)
			c.Channels.WeCom[i].EmployeeName = expandEnvVar(c.Channels.WeCom[i].EmployeeName)
			c.Channels.WeCom[i].WebhookURL = expandEnvVar(c.Channels.WeCom[i].WebhookURL)
			if c.Channels.WeCom[i].BotLongConn != nil {
				c.Channels.WeCom[i].BotLongConn.BotID = expandEnvVar(c.Channels.WeCom[i].BotLongConn.BotID)
				c.Channels.WeCom[i].BotLongConn.BotSecret = expandEnvVar(c.Channels.WeCom[i].BotLongConn.BotSecret)
				c.Channels.WeCom[i].BotLongConn.URL = expandEnvVar(c.Channels.WeCom[i].BotLongConn.URL)
			}
		}
		for i := range c.Channels.WeComBot {
			c.Channels.WeComBot[i].BotID = expandEnvVar(c.Channels.WeComBot[i].BotID)
			c.Channels.WeComBot[i].BotSecret = expandEnvVar(c.Channels.WeComBot[i].BotSecret)
			c.Channels.WeComBot[i].EmployeeName = expandEnvVar(c.Channels.WeComBot[i].EmployeeName)
			c.Channels.WeComBot[i].URL = expandEnvVar(c.Channels.WeComBot[i].URL)
		}
	}

	// 展开 OpenAI 配置中的环境变量
	if c.OpenAI != nil {
		for i, key := range c.OpenAI.APIKeys {
			c.OpenAI.APIKeys[i] = expandEnvVar(key)
		}
	}
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
	return &YAMLConfigForAuth{
		Local: &LocalConfig{
			Users:        c.Auth.BuiltinUsers,
			Roles:        c.Auth.Roles,
			PasswordSalt: c.Auth.PasswordSalt,
		},
		LDAP: c.Auth.LDAP,
		OIDC: c.Auth.OIDC,
	}
}

// YAMLConfigForAuth 用于传递给 auth 包的配置结构
type YAMLConfigForAuth struct {
	Local *LocalConfig
	LDAP  *LDAPConfig
	OIDC  *OIDCConfig
}

// ClientConfig 客户端配置（兼容原有结构）
type ClientConfig struct {
	AccessKeyId     string
	AccessKeySecret string
	Endpoint        string
}

// GetPort 获取端口配置（优先级: 配置文件 > 环境变量 > 默认值）
func (c *Config) GetPort() int {
	port := c.Global.Port
	if port == 0 {
		portStr := os.Getenv("PORT")
		if portStr != "" {
			if parsedPort, err := strconv.Atoi(portStr); err == nil {
				port = parsedPort
			}
		}
		if port == 0 {
			port = 8080
		}
	}
	return port
}

// GetHost 获取监听地址（优先级: 配置文件 > 环境变量 LISTEN_HOST > 默认值 0.0.0.0）
func (c *Config) GetHost() string {
	if c.Global.Host != "" {
		return c.Global.Host
	}
	if h := os.Getenv("LISTEN_HOST"); h != "" {
		return h
	}
	return "0.0.0.0"
}

// GetListenAddr 返回完整的监听地址，格式为 host:port
func (c *Config) GetListenAddr() string {
	return fmt.Sprintf("%s:%d", c.GetHost(), c.GetPort())
}

// SaveConfig 将 Config 结构体序列化为 YAML 并持久化到文件
func SaveConfig(configPath string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}
	return nil
}

// ReadRawConfig 读取配置文件的原始文本内容（用于配置 UI 展示）
func ReadRawConfig(configPath string) (string, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", fmt.Errorf("读取配置文件失败: %w", err)
	}
	return string(data), nil
}

// SaveRawConfig 将原始 YAML 文本写入配置文件，保存前验证 YAML 格式合法性
func SaveRawConfig(configPath string, content string) error {
	var cfg Config
	if err := yaml.Unmarshal([]byte(content), &cfg); err != nil {
		return fmt.Errorf("YAML 格式无效: %w", err)
	}
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}
	return nil
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
