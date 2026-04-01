package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config 统一配置结构
type Config struct {
	// 服务配置
	Server ServerConfig `yaml:"server,omitempty"`

	// legacy 全局配置：仅保留向后兼容读取，不再由配置 UI 写回。
	Global GlobalConfig `yaml:"global,omitempty"`

	// 多云账号配置（可选）：每个账号包含一套访问凭据与 endpoint。
	// 新配置统一通过 cloudAccounts 管理；global.* 凭据仅保留向后兼容读取。
	CloudAccounts []CloudAccountConfig `yaml:"cloudAccounts,omitempty"`

	// 认证配置
	Auth AuthConfig `yaml:"auth"`

	// 通知渠道配置（可选）：钉钉、企业微信、Slack 等
	Channels *ChannelsConfig `yaml:"channels,omitempty"`

	// OpenAI 兼容接口配置（可选）
	OpenAI *OpenAICompatConfig `yaml:"openai,omitempty"`

	// 定时任务配置（可选）
	ScheduledTasks []ScheduledTaskConfig `yaml:"scheduledTasks,omitempty"`
}

// ServerConfig 服务级配置
type ServerConfig struct {
	Host                string `yaml:"host,omitempty"`                // 服务监听地址，默认 0.0.0.0
	Port                int    `yaml:"port,omitempty"`                // 服务监听端口，默认 8080
	TimeZone            string `yaml:"timeZone,omitempty"`            // 时区设置
	Language            string `yaml:"language,omitempty"`            // 语言设置
	BindThreadToProcess *bool  `yaml:"bindThreadToProcess,omitempty"` // 是否将 thread 绑定到进程生命周期
}

const (
	// DefaultCloudAccountID 默认云账号标识；当未显式绑定 cloudAccountId 时使用。
	DefaultCloudAccountID = "default"
)

// CloudAccountConfig 云账号配置
type CloudAccountConfig struct {
	// 账号唯一标识，供渠道/任务通过 cloudAccountId 绑定
	ID string `yaml:"id"`
	// 云厂商（当前仅用于标识，默认 aliyun）
	Provider string `yaml:"provider,omitempty"`
	// 别名（可选）：用于从用户问题中匹配环境/账号，例如 ["uat", "测试环境"]
	Aliases []string `yaml:"aliases,omitempty"`
	// 访问凭据
	AccessKeyId     string `yaml:"accessKeyId"`
	AccessKeySecret string `yaml:"accessKeySecret"`
	// API Endpoint，例如 cms.cn-hangzhou.aliyuncs.com
	Endpoint string `yaml:"endpoint"`
}

// ScheduledTaskConfig 定时任务配置
type ScheduledTaskConfig struct {
	// 任务名称（唯一标识）
	Name string `yaml:"name"`
	// 是否启用
	Enabled bool `yaml:"enabled"`
	// Cron 表达式（标准 5 字段：分 时 日 月 周）
	// 示例："0 9 * * 1-5" 表示工作日每天 9:00
	Cron string `yaml:"cron"`
	// 发送给数字员工的问题/提示语
	Prompt string `yaml:"prompt"`
	// 目标数字员工名称
	EmployeeName string `yaml:"employeeName"`
	// 绑定的云账号 ID；为空时使用默认账号（default）
	CloudAccountID string `yaml:"cloudAccountId,omitempty"`
	// 启用简洁输出：向 Prompt 末尾追加简化输出指令，适合 IM 场景
	ConciseReply bool `yaml:"conciseReply,omitempty"`
	// Product 指定该任务对接的数字员工所属产品：sls（默认）或 cms。
	// 保存时始终规范为 sls/cms，与页面选择及调度执行一致（不使用 omitempty，避免落盘丢失）。
	Product string `yaml:"product"`
	// Project 与 Workspace 根据产品类型二选一
	Project   string `yaml:"project,omitempty"`   // SLS 产品对应的 Project
	Workspace string `yaml:"workspace,omitempty"` // CMS 产品对应的 Workspace
	Region    string `yaml:"region,omitempty"`    // CMS 产品对应的 Region
	// Webhook 配置：任务结果的发送目标（已废弃，保留用于向后兼容旧配置）
	Webhook WebhookConfig `yaml:"webhook,omitempty"`
	// Webhooks 配置：任务结果的发送目标（支持多个）
	Webhooks []WebhookConfig `yaml:"webhooks,omitempty"`
}

// EffectiveWebhooks 返回该任务的有效 webhook 列表。
// 兼容旧配置：如果 Webhooks 为空但 Webhook 有值，则返回 [Webhook]。
func (t *ScheduledTaskConfig) EffectiveWebhooks() []WebhookConfig {
	if len(t.Webhooks) > 0 {
		return t.Webhooks
	}
	if t.Webhook.URL != "" {
		return []WebhookConfig{t.Webhook}
	}
	return nil
}

// WebhookConfig Webhook 目标配置
type WebhookConfig struct {
	// 类型：dingtalk | feishu | wecom | email
	Type string `yaml:"type"`
	// Webhook URL（IM 平台）或 SMTP 地址（email，格式：smtp(s)://user:pass@host:port）
	URL string `yaml:"url"`
	// 消息类型：text | markdown（默认 text）
	MsgType string `yaml:"msgType,omitempty"`
	// 消息标题（IM markdown 格式 / 邮件主题）
	Title string `yaml:"title,omitempty"`
	// 收件人（email 类型，逗号分隔多个地址）
	To string `yaml:"to,omitempty"`
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
	// Product 指定该路由对接的数字员工所属产品：sls（默认）或 cms。
	// 为空时使用渠道配置的 product。
	Product   string `yaml:"product,omitempty"`
	Project   string `yaml:"project,omitempty"`   // SLS 产品对应的 Project
	Workspace string `yaml:"workspace,omitempty"` // CMS 产品对应的 Workspace
	Region    string `yaml:"region,omitempty"`    // CMS 产品对应的 Region
}

// CloudAccountRoute 云账号路由规则：命中特定 cloudAccountId 时切换到对应数字员工。
// 主要用于一个渠道实例同时服务多个订阅，但不同订阅下的数字员工名称不一致。
type CloudAccountRoute struct {
	CloudAccountID string `yaml:"cloudAccountId"`
	EmployeeName   string `yaml:"employeeName,omitempty"`
	Product        string `yaml:"product,omitempty"`
	Project        string `yaml:"project,omitempty"`   // SLS 产品对应的 Project
	Workspace      string `yaml:"workspace,omitempty"` // CMS 产品对应的 Workspace
	Region         string `yaml:"region,omitempty"`    // CMS 产品对应的 Region
}

// DingTalkConfig 钉钉机器人配置
type DingTalkConfig struct {
	// 是否启用钉钉机器人；false 时保留配置但不启动 Stream 连接
	Enabled      bool   `yaml:"enabled"`
	Name         string `yaml:"name,omitempty"` // 机器人显示名称（仅用于标识，不影响功能）
	ClientId     string `yaml:"clientId"`       // 企业内部应用 AppKey（唯一标识）
	ClientSecret string `yaml:"clientSecret"`   // 企业内部应用 AppSecret
	EmployeeName string `yaml:"employeeName"`   // 默认数字员工名称
	// 绑定的云账号 ID；为空时使用默认账号（default）
	CloudAccountID string `yaml:"cloudAccountId,omitempty"`
	// 开启后，发送给大模型的消息会附加精简指令，要求回复简短、适合 IM 阅读
	ConciseReply bool `yaml:"conciseReply,omitempty"`
	// Product 指定该渠道对接的数字员工所属产品：sls（默认）或 cms。
	// 为空时根据 project/workspace 推断；都为空时默认 sls。
	Product string `yaml:"product,omitempty"`
	// SLS 产品：数字员工所属 project（写入 Thread Variables.Project）
	Project string `yaml:"project,omitempty"`
	// CMS 产品：数字员工所属 workspace（写入 Thread Variables.Workspace）
	Workspace string `yaml:"workspace,omitempty"`
	// CMS 产品：数字员工所属 region（写入 Thread Variables.Region）
	Region string `yaml:"region,omitempty"`
	// 群用户白名单（钉钉 senderNick）；限制群聊中可 @ 机器人提问的用户；为空时允许所有群成员
	AllowedGroupUsers []string `yaml:"allowedGroupUsers,omitempty"`
	// 单聊用户白名单（钉钉 senderNick）；限制可与机器人单聊的用户；为空时允许所有人单聊
	AllowedDirectUsers []string `yaml:"allowedDirectUsers,omitempty"`
	// 群白名单（conversationTitle）；为空时允许所有群；有值时仅允许列出的群，单聊不受此限制
	AllowedConversations []string `yaml:"allowedConversations,omitempty"`
	// AI 流式卡片配置（可选）：配置后优先使用流式卡片回复，失败时降级为普通 Markdown
	// 卡片模板 ID，在钉钉开放平台创建的 AI 卡片模板
	CardTemplateId string `yaml:"cardTemplateId,omitempty"`
	// 流式更新变量名，对应卡片模板中的变量名，默认 "content"
	CardContentKey string `yaml:"cardContentKey,omitempty"`
	// 群名称路由：按群名将消息路由到不同的数字员工；匹配不到时使用顶层 employeeName
	ConversationRoutes []ConversationRoute `yaml:"conversationRoutes,omitempty"`
	// 云账号路由：按消息里识别到的 cloudAccountId 切换数字员工；匹配不到时使用顶层 employeeName
	CloudAccountRoutes []CloudAccountRoute `yaml:"cloudAccountRoutes,omitempty"`
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
	// 绑定的云账号 ID；为空时使用默认账号（default）
	CloudAccountID string `yaml:"cloudAccountId,omitempty"`
	// 开启后回复简短，适合 IM 阅读
	ConciseReply bool `yaml:"conciseReply,omitempty"`
	// Product 指定该渠道对接的数字员工所属产品：sls（默认）或 cms。
	// 为空时根据 project/workspace 推断；都为空时默认 sls。
	Product string `yaml:"product,omitempty"`
	// SLS 产品：数字员工所属 project（写入 Thread Variables.Project）
	Project string `yaml:"project,omitempty"`
	// CMS 产品：数字员工所属 workspace（写入 Thread Variables.Workspace）
	Workspace string `yaml:"workspace,omitempty"`
	// CMS 产品：数字员工所属 region（写入 Thread Variables.Region）
	Region string `yaml:"region,omitempty"`
	// 用户白名单（飞书 open_id）；为空时允许所有用户
	AllowedUsers []string `yaml:"allowedUsers,omitempty"`
	// 群聊白名单（飞书 chat_id）；为空时允许所有群聊
	AllowedChats []string `yaml:"allowedChats,omitempty"`
	// 云账号路由：按消息里识别到的 cloudAccountId 切换数字员工；匹配不到时使用顶层 employeeName
	CloudAccountRoutes []CloudAccountRoute `yaml:"cloudAccountRoutes,omitempty"`
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
	// 绑定的云账号 ID；为空时使用默认账号（default）
	CloudAccountID string `yaml:"cloudAccountId,omitempty"`
	// 开启后回复简短，适合 IM 阅读
	ConciseReply bool `yaml:"conciseReply,omitempty"`
	// Product 指定该渠道对接的数字员工所属产品：sls（默认）或 cms。
	// 为空时根据 project/workspace 推断；都为空时默认 sls。
	Product string `yaml:"product,omitempty"`
	// SLS 产品：数字员工所属 project（写入 Thread Variables.Project）
	Project string `yaml:"project,omitempty"`
	// CMS 产品：数字员工所属 workspace（写入 Thread Variables.Workspace）
	Workspace string `yaml:"workspace,omitempty"`
	// CMS 产品：数字员工所属 region（写入 Thread Variables.Region）
	Region string `yaml:"region,omitempty"`
	// 用户白名单（企业微信 userid）；为空时允许所有用户
	AllowedUsers []string `yaml:"allowedUsers,omitempty"`
	// 群机器人 Webhook URL（可选）；配置后 AI 回复会同步推送到群聊
	WebhookURL string `yaml:"webhookUrl,omitempty"`
	// AI 助手群机器人长连接配置（从企业微信管理后台 AI 助手页面获取）
	BotLongConn *WeComBotLongConnConfig `yaml:"botLongConn,omitempty"`
	// 云账号路由：按消息里识别到的 cloudAccountId 切换数字员工；匹配不到时使用顶层 employeeName
	CloudAccountRoutes []CloudAccountRoute `yaml:"cloudAccountRoutes,omitempty"`
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
	// 绑定的云账号 ID；为空时使用默认账号（default）
	CloudAccountID string `yaml:"cloudAccountId,omitempty"`
	// 开启后回复简短，适合 IM 阅读
	ConciseReply bool `yaml:"conciseReply,omitempty"`
	// Product 指定该渠道对接的数字员工所属产品：sls（默认）或 cms。
	// 为空时根据 project/workspace 推断；都为空时默认 sls。
	Product string `yaml:"product,omitempty"`
	// SLS 产品：数字员工所属 project（写入 Thread Variables.Project）
	Project string `yaml:"project,omitempty"`
	// CMS 产品：数字员工所属 workspace（写入 Thread Variables.Workspace）
	Workspace string `yaml:"workspace,omitempty"`
	// CMS 产品：数字员工所属 region（写入 Thread Variables.Region）
	Region string `yaml:"region,omitempty"`
	// WebSocket 连接地址（默认 wss://openws.work.weixin.qq.com）
	URL string `yaml:"url,omitempty"`
	// 心跳间隔（秒，默认 30）
	PingIntervalSec int `yaml:"pingIntervalSec,omitempty"`
	// 重连延迟（秒，默认 5）
	ReconnectDelaySec int `yaml:"reconnectDelaySec,omitempty"`
	// 最大重连延迟（秒，默认 60）
	MaxReconnectDelaySec int `yaml:"maxReconnectDelaySec,omitempty"`
	// 云账号路由：按消息里识别到的 cloudAccountId 切换数字员工；匹配不到时使用顶层 employeeName
	CloudAccountRoutes []CloudAccountRoute `yaml:"cloudAccountRoutes,omitempty"`
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

// GlobalConfig legacy 全局配置
type GlobalConfig struct {
	// legacy 凭据字段：仅保留向后兼容读取，不再由配置 UI 写入。
	AccessKeyId     string `yaml:"accessKeyId,omitempty"`
	AccessKeySecret string `yaml:"accessKeySecret,omitempty"`
	Endpoint        string `yaml:"endpoint,omitempty"`
	Host            string `yaml:"host,omitempty"`     // legacy 服务监听地址
	Port            int    `yaml:"port,omitempty"`     // legacy 服务监听端口
	TimeZone        string `yaml:"timeZone,omitempty"` // legacy 时区设置
	Language        string `yaml:"language,omitempty"` // legacy 语言设置
	// legacy thread 生命周期开关：新配置应写在 server.bindThreadToProcess。
	BindThreadToProcess *bool `yaml:"bindThreadToProcess,omitempty"`
	// legacy 对接产品默认值：新配置应在各渠道/任务上显式配置 product。
	Product   string `yaml:"product,omitempty"`
	Project   string `yaml:"project,omitempty"`   // SLS 产品对应的 Project
	Workspace string `yaml:"workspace,omitempty"` // CMS 产品对应的 Workspace
	Region    string `yaml:"region,omitempty"`    // CMS 产品对应的 Region
}

// ProductContext 表示一次对话/线程需要的产品变量上下文。
type ProductContext struct {
	Product   string
	Project   string
	Workspace string
	Region    string
}

const (
	// ConciseReplyInstruction 开启简洁模式时附加到用户消息末尾的指令。
	ConciseReplyInstruction = "\n\n（请用简洁的纯文本回答，避免复杂排版，适合在 IM 中直接阅读，控制在几句话以内。尽量拟人的语气，少用 markdown。）"
	// StandardSOPReplyInstruction 在关闭简洁模式且对接 SLS/SOP 员工时，提示模型按完整 SOP 规范作答。
	StandardSOPReplyInstruction = "\n\n（请严格按照 SOP 文档和标准流程完整回答，不要为了适应 IM 而省略关键判断、排查步骤、影响面、结论和建议；如果有既定模板或报告格式，请尽量按模板完整输出。）"
)

// NormalizeProduct 将 product 规范为 cms 或 sls（大小写与空白容错）。
func NormalizeProduct(s string) string {
	if strings.TrimSpace(strings.ToLower(s)) == "cms" {
		return "cms"
	}
	return "sls"
}

// ResolveProduct 根据显式 product 或 project/workspace 推断有效产品类型。
func ResolveProduct(product, project, workspace string) string {
	if strings.TrimSpace(workspace) != "" {
		return "cms"
	}
	if strings.TrimSpace(project) != "" {
		return "sls"
	}
	return NormalizeProduct(product)
}

// NewProductContext 构造规范化后的产品上下文。
func NewProductContext(product, project, workspace, region string) ProductContext {
	return ProductContext{
		Product:   ResolveProduct(product, project, workspace),
		Project:   strings.TrimSpace(project),
		Workspace: strings.TrimSpace(workspace),
		Region:    strings.TrimSpace(region),
	}
}

// MergeProductContext 将 override 覆盖到 base 上，并重新推断最终 product。
func MergeProductContext(base ProductContext, product, project, workspace, region string) ProductContext {
	ctx := base
	if strings.TrimSpace(product) != "" {
		ctx.Product = strings.TrimSpace(strings.ToLower(product))
	}
	if strings.TrimSpace(project) != "" {
		ctx.Project = strings.TrimSpace(project)
	}
	if strings.TrimSpace(workspace) != "" {
		ctx.Workspace = strings.TrimSpace(workspace)
	}
	if strings.TrimSpace(region) != "" {
		ctx.Region = strings.TrimSpace(region)
	}
	ctx.Product = ResolveProduct(ctx.Product, ctx.Project, ctx.Workspace)
	return ctx
}

// ApplyReplyStyleInstruction 根据 conciseReply 和产品类型附加消息风格提示。
func ApplyReplyStyleInstruction(message string, conciseReply bool, product string) string {
	if conciseReply {
		return message + ConciseReplyInstruction
	}
	if IsSlsProduct(product) {
		return message + StandardSOPReplyInstruction
	}
	return message
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
	Host         string `yaml:"host"`         // LDAP 服务器地址，如 ldap.example.com
	Port         int    `yaml:"port"`         // 端口，明文默认 389，TLS 默认 636
	UseTLS       bool   `yaml:"useTLS"`       // 是否使用 TLS（LDAPS）
	BindDN       string `yaml:"bindDN"`       // 查询用 DN，如 cn=readonly,dc=example,dc=com
	BindPassword string `yaml:"bindPassword"` // 查询用密码
	BaseDN       string `yaml:"baseDN"`       // 用户搜索根，如 ou=people,dc=example,dc=com
	UserFilter   string `yaml:"userFilter"`   // 用户搜索过滤器，如 (uid={username})
	UsernameAttr string `yaml:"usernameAttr"` // 用户名属性，默认 uid
	DisplayAttr  string `yaml:"displayAttr"`  // 显示名属性，默认 cn
	EmailAttr    string `yaml:"emailAttr"`    // 邮箱属性，默认 mail
}

// OIDCConfig OIDC / OAuth2 认证配置
// 兼容标准 OIDC Provider（Keycloak、Dex、Okta、Azure AD 等）
type OIDCConfig struct {
	IssuerURL     string   `yaml:"issuerURL"`               // Provider 地址，如 https://accounts.example.com
	ClientID      string   `yaml:"clientId"`                // OAuth2 Client ID
	ClientSecret  string   `yaml:"clientSecret"`            // OAuth2 Client Secret
	RedirectURL   string   `yaml:"redirectURL"`             // 回调地址，如 http://your-server/api/auth/oidc/callback
	Scopes        []string `yaml:"scopes,omitempty"`        // 默认: [openid, profile, email]
	UsernameClaim string   `yaml:"usernameClaim,omitempty"` // 用于提取用户名的 claim，默认 preferred_username
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
// - 凭据通过 cloudAccounts 配置；legacy global.* 不再作为默认写入项
// - auth.methods 为空（登录功能关闭，配置后重启或热重载生效）
// - JWT secretKey 随机生成，避免各实例共用同一密钥
func DefaultConfig() *Config {
	bindThread := true
	return &Config{
		Server: ServerConfig{
			Host:                "0.0.0.0",
			Port:                8080,
			TimeZone:            "Asia/Shanghai",
			Language:            "zh",
			BindThreadToProcess: &bindThread,
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

// BindThreadToProcess 返回是否将 thread 绑定到进程生命周期。
// 配置缺失时默认开启（true）。
func (c *Config) BindThreadToProcess() bool {
	if c == nil {
		return true
	}
	if c.Server.BindThreadToProcess != nil {
		return *c.Server.BindThreadToProcess
	}
	if c.Global.BindThreadToProcess != nil {
		return *c.Global.BindThreadToProcess
	}
	return true
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
	config.applyCompatibilityDefaults()

	return &config, configPath, nil
}

// expandEnvVars 展开配置中的环境变量引用
// 支持格式: $VAR 或 ${VAR}
func (c *Config) expandEnvVars() {
	// 展开 Server 配置中的环境变量
	c.Server.Host = expandEnvVar(c.Server.Host)
	c.Server.TimeZone = expandEnvVar(c.Server.TimeZone)
	c.Server.Language = expandEnvVar(c.Server.Language)

	// 展开 legacy Global 配置中的环境变量
	c.Global.AccessKeyId = expandEnvVar(c.Global.AccessKeyId)
	c.Global.AccessKeySecret = expandEnvVar(c.Global.AccessKeySecret)
	c.Global.Endpoint = expandEnvVar(c.Global.Endpoint)
	c.Global.Host = expandEnvVar(c.Global.Host)
	c.Global.TimeZone = expandEnvVar(c.Global.TimeZone)
	c.Global.Language = expandEnvVar(c.Global.Language)
	c.Global.Product = expandEnvVar(c.Global.Product)

	// 展开多云账号配置中的环境变量
	for i := range c.CloudAccounts {
		c.CloudAccounts[i].ID = expandEnvVar(c.CloudAccounts[i].ID)
		c.CloudAccounts[i].Provider = expandEnvVar(c.CloudAccounts[i].Provider)
		for j := range c.CloudAccounts[i].Aliases {
			c.CloudAccounts[i].Aliases[j] = expandEnvVar(c.CloudAccounts[i].Aliases[j])
		}
		c.CloudAccounts[i].AccessKeyId = expandEnvVar(c.CloudAccounts[i].AccessKeyId)
		c.CloudAccounts[i].AccessKeySecret = expandEnvVar(c.CloudAccounts[i].AccessKeySecret)
		c.CloudAccounts[i].Endpoint = expandEnvVar(c.CloudAccounts[i].Endpoint)
	}

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
			c.Channels.DingTalk[i].CloudAccountID = expandEnvVar(c.Channels.DingTalk[i].CloudAccountID)
			for j := range c.Channels.DingTalk[i].ConversationRoutes {
				c.Channels.DingTalk[i].ConversationRoutes[j].ConversationTitle = expandEnvVar(c.Channels.DingTalk[i].ConversationRoutes[j].ConversationTitle)
				c.Channels.DingTalk[i].ConversationRoutes[j].EmployeeName = expandEnvVar(c.Channels.DingTalk[i].ConversationRoutes[j].EmployeeName)
				c.Channels.DingTalk[i].ConversationRoutes[j].Product = expandEnvVar(c.Channels.DingTalk[i].ConversationRoutes[j].Product)
				c.Channels.DingTalk[i].ConversationRoutes[j].Project = expandEnvVar(c.Channels.DingTalk[i].ConversationRoutes[j].Project)
				c.Channels.DingTalk[i].ConversationRoutes[j].Workspace = expandEnvVar(c.Channels.DingTalk[i].ConversationRoutes[j].Workspace)
				c.Channels.DingTalk[i].ConversationRoutes[j].Region = expandEnvVar(c.Channels.DingTalk[i].ConversationRoutes[j].Region)
			}
			for j := range c.Channels.DingTalk[i].CloudAccountRoutes {
				c.Channels.DingTalk[i].CloudAccountRoutes[j].CloudAccountID = expandEnvVar(c.Channels.DingTalk[i].CloudAccountRoutes[j].CloudAccountID)
				c.Channels.DingTalk[i].CloudAccountRoutes[j].EmployeeName = expandEnvVar(c.Channels.DingTalk[i].CloudAccountRoutes[j].EmployeeName)
				c.Channels.DingTalk[i].CloudAccountRoutes[j].Product = expandEnvVar(c.Channels.DingTalk[i].CloudAccountRoutes[j].Product)
				c.Channels.DingTalk[i].CloudAccountRoutes[j].Project = expandEnvVar(c.Channels.DingTalk[i].CloudAccountRoutes[j].Project)
				c.Channels.DingTalk[i].CloudAccountRoutes[j].Workspace = expandEnvVar(c.Channels.DingTalk[i].CloudAccountRoutes[j].Workspace)
				c.Channels.DingTalk[i].CloudAccountRoutes[j].Region = expandEnvVar(c.Channels.DingTalk[i].CloudAccountRoutes[j].Region)
			}
		}
		for i := range c.Channels.Feishu {
			c.Channels.Feishu[i].AppID = expandEnvVar(c.Channels.Feishu[i].AppID)
			c.Channels.Feishu[i].AppSecret = expandEnvVar(c.Channels.Feishu[i].AppSecret)
			c.Channels.Feishu[i].EmployeeName = expandEnvVar(c.Channels.Feishu[i].EmployeeName)
			c.Channels.Feishu[i].CloudAccountID = expandEnvVar(c.Channels.Feishu[i].CloudAccountID)
			for j := range c.Channels.Feishu[i].CloudAccountRoutes {
				c.Channels.Feishu[i].CloudAccountRoutes[j].CloudAccountID = expandEnvVar(c.Channels.Feishu[i].CloudAccountRoutes[j].CloudAccountID)
				c.Channels.Feishu[i].CloudAccountRoutes[j].EmployeeName = expandEnvVar(c.Channels.Feishu[i].CloudAccountRoutes[j].EmployeeName)
				c.Channels.Feishu[i].CloudAccountRoutes[j].Product = expandEnvVar(c.Channels.Feishu[i].CloudAccountRoutes[j].Product)
				c.Channels.Feishu[i].CloudAccountRoutes[j].Project = expandEnvVar(c.Channels.Feishu[i].CloudAccountRoutes[j].Project)
				c.Channels.Feishu[i].CloudAccountRoutes[j].Workspace = expandEnvVar(c.Channels.Feishu[i].CloudAccountRoutes[j].Workspace)
				c.Channels.Feishu[i].CloudAccountRoutes[j].Region = expandEnvVar(c.Channels.Feishu[i].CloudAccountRoutes[j].Region)
			}
		}
		for i := range c.Channels.WeCom {
			c.Channels.WeCom[i].CorpID = expandEnvVar(c.Channels.WeCom[i].CorpID)
			c.Channels.WeCom[i].Secret = expandEnvVar(c.Channels.WeCom[i].Secret)
			c.Channels.WeCom[i].EmployeeName = expandEnvVar(c.Channels.WeCom[i].EmployeeName)
			c.Channels.WeCom[i].WebhookURL = expandEnvVar(c.Channels.WeCom[i].WebhookURL)
			c.Channels.WeCom[i].CloudAccountID = expandEnvVar(c.Channels.WeCom[i].CloudAccountID)
			for j := range c.Channels.WeCom[i].CloudAccountRoutes {
				c.Channels.WeCom[i].CloudAccountRoutes[j].CloudAccountID = expandEnvVar(c.Channels.WeCom[i].CloudAccountRoutes[j].CloudAccountID)
				c.Channels.WeCom[i].CloudAccountRoutes[j].EmployeeName = expandEnvVar(c.Channels.WeCom[i].CloudAccountRoutes[j].EmployeeName)
				c.Channels.WeCom[i].CloudAccountRoutes[j].Product = expandEnvVar(c.Channels.WeCom[i].CloudAccountRoutes[j].Product)
				c.Channels.WeCom[i].CloudAccountRoutes[j].Project = expandEnvVar(c.Channels.WeCom[i].CloudAccountRoutes[j].Project)
				c.Channels.WeCom[i].CloudAccountRoutes[j].Workspace = expandEnvVar(c.Channels.WeCom[i].CloudAccountRoutes[j].Workspace)
				c.Channels.WeCom[i].CloudAccountRoutes[j].Region = expandEnvVar(c.Channels.WeCom[i].CloudAccountRoutes[j].Region)
			}
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
			c.Channels.WeComBot[i].CloudAccountID = expandEnvVar(c.Channels.WeComBot[i].CloudAccountID)
			c.Channels.WeComBot[i].URL = expandEnvVar(c.Channels.WeComBot[i].URL)
			for j := range c.Channels.WeComBot[i].CloudAccountRoutes {
				c.Channels.WeComBot[i].CloudAccountRoutes[j].CloudAccountID = expandEnvVar(c.Channels.WeComBot[i].CloudAccountRoutes[j].CloudAccountID)
				c.Channels.WeComBot[i].CloudAccountRoutes[j].EmployeeName = expandEnvVar(c.Channels.WeComBot[i].CloudAccountRoutes[j].EmployeeName)
				c.Channels.WeComBot[i].CloudAccountRoutes[j].Product = expandEnvVar(c.Channels.WeComBot[i].CloudAccountRoutes[j].Product)
				c.Channels.WeComBot[i].CloudAccountRoutes[j].Project = expandEnvVar(c.Channels.WeComBot[i].CloudAccountRoutes[j].Project)
				c.Channels.WeComBot[i].CloudAccountRoutes[j].Workspace = expandEnvVar(c.Channels.WeComBot[i].CloudAccountRoutes[j].Workspace)
				c.Channels.WeComBot[i].CloudAccountRoutes[j].Region = expandEnvVar(c.Channels.WeComBot[i].CloudAccountRoutes[j].Region)
			}
		}
	}

	// 展开 OpenAI 配置中的环境变量
	if c.OpenAI != nil {
		for i, key := range c.OpenAI.APIKeys {
			c.OpenAI.APIKeys[i] = expandEnvVar(key)
		}
	}

	// 展开定时任务配置中的环境变量
	for i := range c.ScheduledTasks {
		c.ScheduledTasks[i].Webhook.URL = expandEnvVar(c.ScheduledTasks[i].Webhook.URL)
		c.ScheduledTasks[i].EmployeeName = expandEnvVar(c.ScheduledTasks[i].EmployeeName)
		c.ScheduledTasks[i].CloudAccountID = expandEnvVar(c.ScheduledTasks[i].CloudAccountID)
	}
}

func (c *Config) applyCompatibilityDefaults() {
	if c == nil {
		return
	}

	if c.Server.Host == "" {
		c.Server.Host = c.Global.Host
	}
	if c.Server.Port == 0 {
		c.Server.Port = c.Global.Port
	}
	if c.Server.TimeZone == "" {
		c.Server.TimeZone = c.Global.TimeZone
	}
	if c.Server.Language == "" {
		c.Server.Language = c.Global.Language
	}
	if c.Server.BindThreadToProcess == nil && c.Global.BindThreadToProcess != nil {
		c.Server.BindThreadToProcess = c.Global.BindThreadToProcess
	}

	if len(c.CloudAccounts) == 0 {
		if strings.TrimSpace(c.Global.AccessKeyId) != "" ||
			strings.TrimSpace(c.Global.AccessKeySecret) != "" ||
			strings.TrimSpace(c.Global.Endpoint) != "" {
			c.CloudAccounts = []CloudAccountConfig{
				{
					ID:              DefaultCloudAccountID,
					Provider:        "aliyun",
					AccessKeyId:     c.Global.AccessKeyId,
					AccessKeySecret: c.Global.AccessKeySecret,
					Endpoint:        c.Global.Endpoint,
				},
			}
		}
	}

	for i := range c.CloudAccounts {
		if strings.TrimSpace(c.CloudAccounts[i].Provider) == "" {
			c.CloudAccounts[i].Provider = "aliyun"
		}
		if strings.TrimSpace(c.CloudAccounts[i].Endpoint) == "" {
			c.CloudAccounts[i].Endpoint = strings.TrimSpace(c.Global.Endpoint)
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

// ToClientConfig 转换为客户端配置（默认账号）。
// 行为：
// 1) 优先使用 cloudAccounts 中默认账号（id=default，若不存在则取第一个）；
// 2) 若未配置 cloudAccounts，则回退到 legacy global.accessKeyId/accessKeySecret/endpoint（仅兼容旧配置）。
func (c *Config) ToClientConfig() (*ClientConfig, error) {
	return c.ResolveClientConfig("")
}

// ResolveClientConfig 根据 cloudAccountId 解析客户端配置。
// cloudAccountId 为空时使用默认账号（default）。
// 若未配置 cloudAccounts，则回退到 legacy global.* 凭据（仅兼容旧配置）。
func (c *Config) ResolveClientConfig(cloudAccountID string) (*ClientConfig, error) {
	targetID := NormalizeCloudAccountID(cloudAccountID)

	// 指定了 cloudAccountId：必须在 cloudAccounts 中可解析（"default" 允许回退 global）
	if strings.TrimSpace(cloudAccountID) != "" {
		if acc := c.findCloudAccountByID(targetID); acc != nil {
			return c.clientConfigFromCloudAccount(acc)
		}
		if targetID != DefaultCloudAccountID {
			return nil, fmt.Errorf("cloud account %q not found", targetID)
		}
		// targetID == default：优先取默认账号（id=default，或第一个账号）
		if acc := c.defaultCloudAccount(); acc != nil {
			return c.clientConfigFromCloudAccount(acc)
		}
		// cloudAccounts 为空时回退 legacy global
		return c.clientConfigFromGlobal()
	}

	// 未指定 cloudAccountId：优先使用默认 cloud account
	if acc := c.defaultCloudAccount(); acc != nil {
		return c.clientConfigFromCloudAccount(acc)
	}
	// 回退 legacy global
	return c.clientConfigFromGlobal()
}

// NormalizeCloudAccountID 规范化 cloudAccountId：空值统一映射为 default。
func NormalizeCloudAccountID(id string) string {
	s := strings.TrimSpace(id)
	if s == "" {
		return DefaultCloudAccountID
	}
	return s
}

func (c *Config) findCloudAccountByID(id string) *CloudAccountConfig {
	if c == nil {
		return nil
	}
	target := NormalizeCloudAccountID(id)
	for i := range c.CloudAccounts {
		accountID := NormalizeCloudAccountID(c.CloudAccounts[i].ID)
		if accountID == target {
			return &c.CloudAccounts[i]
		}
	}
	return nil
}

func (c *Config) defaultCloudAccount() *CloudAccountConfig {
	if c == nil || len(c.CloudAccounts) == 0 {
		return nil
	}
	for i := range c.CloudAccounts {
		if NormalizeCloudAccountID(c.CloudAccounts[i].ID) == DefaultCloudAccountID {
			return &c.CloudAccounts[i]
		}
	}
	return &c.CloudAccounts[0]
}

// MatchCloudAccountIDsByText 从用户文本中匹配云账号 ID（匹配规则：账号 id 或 aliases 子串命中，大小写不敏感）。
// 若 allowedIDs 非空，仅在允许集合内匹配。
func (c *Config) MatchCloudAccountIDsByText(text string, allowedIDs []string) []string {
	if c == nil {
		return nil
	}
	query := strings.TrimSpace(strings.ToLower(text))
	if query == "" {
		return nil
	}

	allow := make(map[string]struct{}, len(allowedIDs))
	for _, id := range allowedIDs {
		allow[NormalizeCloudAccountID(id)] = struct{}{}
	}
	useAllow := len(allow) > 0

	matched := make(map[string]struct{})
	for i := range c.CloudAccounts {
		account := &c.CloudAccounts[i]
		accountID := NormalizeCloudAccountID(account.ID)
		if useAllow {
			if _, ok := allow[accountID]; !ok {
				continue
			}
		}
		tokens := make([]string, 0, 1+len(account.Aliases))
		tokens = append(tokens, accountID)
		tokens = append(tokens, account.Aliases...)
		for _, token := range tokens {
			t := strings.TrimSpace(strings.ToLower(token))
			if t == "" {
				continue
			}
			if containsCloudAccountToken(query, t) {
				matched[accountID] = struct{}{}
				break
			}
		}
	}

	result := make([]string, 0, len(matched))
	for id := range matched {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func containsCloudAccountToken(query, token string) bool {
	if query == "" || token == "" {
		return false
	}
	if !strings.Contains(query, token) {
		return false
	}
	if !requiresCloudAccountTokenBoundary(token) {
		return true
	}
	pattern := `(^|[^a-z0-9])` + regexp.QuoteMeta(token) + `([^a-z0-9]|$)`
	return regexp.MustCompile(pattern).MatchString(query)
}

func requiresCloudAccountTokenBoundary(token string) bool {
	for _, r := range token {
		if r < 'a' || r > 'z' {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

// ListCloudAccountIDs 返回配置中的云账号 ID（规范化、去重、排序）。
func (c *Config) ListCloudAccountIDs() []string {
	if c == nil {
		return nil
	}
	set := make(map[string]struct{})
	for i := range c.CloudAccounts {
		id := NormalizeCloudAccountID(c.CloudAccounts[i].ID)
		set[id] = struct{}{}
	}
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// ResolveMessageCloudAccountID 根据消息文本解析目标 cloudAccountId。
// 返回值 matched 表示消息里是否唯一命中了一个云账号；
// ambiguous 非空表示消息同时命中了多个账号，此时调用方应自行决定是否提示用户。
// 未命中时回退到 fallbackCloudAccountID（为空则回退 default/第一个账号）。
func (c *Config) ResolveMessageCloudAccountID(message, fallbackCloudAccountID string) (cloudAccountID string, matched bool, ambiguous []string) {
	targetID := NormalizeCloudAccountID(fallbackCloudAccountID)
	if c == nil {
		return targetID, false, nil
	}

	if acc := c.defaultCloudAccount(); targetID == DefaultCloudAccountID && acc != nil {
		targetID = NormalizeCloudAccountID(acc.ID)
	}

	accountIDs := c.ListCloudAccountIDs()
	if len(accountIDs) <= 1 {
		return targetID, false, nil
	}

	matches := c.MatchCloudAccountIDsByText(message, nil)
	if len(matches) == 1 {
		return matches[0], true, nil
	}
	if len(matches) > 1 {
		return targetID, false, matches
	}
	return targetID, false, nil
}

// FindCloudAccountRoute 按 cloudAccountId 查找匹配的渠道路由。
func FindCloudAccountRoute(routes []CloudAccountRoute, cloudAccountID string) *CloudAccountRoute {
	targetID := NormalizeCloudAccountID(cloudAccountID)
	for i := range routes {
		if NormalizeCloudAccountID(routes[i].CloudAccountID) == targetID {
			return &routes[i]
		}
	}
	return nil
}

func (c *Config) clientConfigFromCloudAccount(account *CloudAccountConfig) (*ClientConfig, error) {
	if c == nil || account == nil {
		return nil, fmt.Errorf("cloud account config is nil")
	}
	accountID := NormalizeCloudAccountID(account.ID)

	accessKeyId := strings.TrimSpace(account.AccessKeyId)
	if accessKeyId == "" {
		accessKeyId = os.Getenv("ACCESS_KEY_ID")
	}
	accessKeySecret := strings.TrimSpace(account.AccessKeySecret)
	if accessKeySecret == "" {
		accessKeySecret = os.Getenv("ACCESS_KEY_SECRET")
	}
	endpoint := strings.TrimSpace(account.Endpoint)
	if endpoint == "" {
		endpoint = strings.TrimSpace(c.Global.Endpoint)
	}
	if endpoint == "" {
		endpoint = os.Getenv("CMS_ENDPOINT")
	}

	if accessKeyId == "" {
		return nil, fmt.Errorf("ACCESS_KEY_ID not configured for cloud account %q", accountID)
	}
	if accessKeySecret == "" {
		return nil, fmt.Errorf("ACCESS_KEY_SECRET not configured for cloud account %q", accountID)
	}

	return &ClientConfig{
		CloudAccountID:  accountID,
		AccessKeyId:     accessKeyId,
		AccessKeySecret: accessKeySecret,
		Endpoint:        endpoint,
		Product:         c.GetLegacyProduct(),
	}, nil
}

func (c *Config) clientConfigFromGlobal() (*ClientConfig, error) {
	if c == nil {
		return nil, fmt.Errorf("config is nil")
	}
	accessKeyId := strings.TrimSpace(c.Global.AccessKeyId)
	if accessKeyId == "" {
		accessKeyId = os.Getenv("ACCESS_KEY_ID")
	}
	accessKeySecret := strings.TrimSpace(c.Global.AccessKeySecret)
	if accessKeySecret == "" {
		accessKeySecret = os.Getenv("ACCESS_KEY_SECRET")
	}
	endpoint := strings.TrimSpace(c.Global.Endpoint)
	if endpoint == "" {
		endpoint = os.Getenv("CMS_ENDPOINT")
	}

	if accessKeyId == "" {
		return nil, fmt.Errorf("ACCESS_KEY_ID not configured (please set cloudAccounts[].accessKeyId; legacy global.accessKeyId is only kept for backward compatibility)")
	}
	if accessKeySecret == "" {
		return nil, fmt.Errorf("ACCESS_KEY_SECRET not configured (please set cloudAccounts[].accessKeySecret; legacy global.accessKeySecret is only kept for backward compatibility)")
	}

	return &ClientConfig{
		CloudAccountID:  DefaultCloudAccountID,
		AccessKeyId:     accessKeyId,
		AccessKeySecret: accessKeySecret,
		Endpoint:        endpoint,
		Product:         c.GetLegacyProduct(),
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
	CloudAccountID  string
	AccessKeyId     string
	AccessKeySecret string
	Endpoint        string
	// Product 为 legacy 默认产品，仅用于兼容旧配置；新配置应在渠道/任务上显式配置。
	Product string
}

// GetPort 获取端口配置（优先级: 配置文件 > 环境变量 > 默认值）
func (c *Config) GetPort() int {
	port := c.Server.Port
	if port == 0 {
		port = c.Global.Port
	}
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
	if c.Server.Host != "" {
		return c.Server.Host
	}
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

// IsSlsProduct 判断给定的 product 值是否对应 SLS 产品。
// 空字符串和 "sls" 均视为 SLS（向后兼容默认行为）。
func IsSlsProduct(product string) bool {
	return product == "" || product == "sls"
}

// NormalizeScheduledTaskProduct 将表单/配置中的 product 规范为 cms 或 sls（大小写与空白容错）。
func NormalizeScheduledTaskProduct(s string) string {
	return NormalizeProduct(s)
}

// ResolveScheduledTaskProduct 解析定时任务 / 手动触发测试 / 保存配置使用的 product，与页面下拉选项一致（仅 cms 或 sls）。
// taskProduct 非空时以其为准并规范化；为空时：Workspace 非空则视为 cms，Project 非空则视为 sls，否则使用 globalProduct（再为空则默认 sls）。
func ResolveScheduledTaskProduct(taskProduct, project, workspace, globalProduct string) string {
	if strings.TrimSpace(taskProduct) != "" {
		return ResolveProduct(taskProduct, project, workspace)
	}
	if strings.TrimSpace(workspace) != "" {
		return "cms"
	}
	if strings.TrimSpace(project) != "" {
		return "sls"
	}
	g := strings.TrimSpace(globalProduct)
	if g != "" {
		return NormalizeProduct(g)
	}
	return "sls"
}

// GetLegacyProduct 获取 legacy 全局 product（如果未配置则返回默认值 "sls"）。
func (c *Config) GetLegacyProduct() string {
	if c.Global.Product == "" {
		return "sls"
	}
	return NormalizeProduct(c.Global.Product)
}

// GetLegacyProductContext 返回 legacy global 中的产品变量默认值。
func (c *Config) GetLegacyProductContext() ProductContext {
	if c == nil {
		return NewProductContext("", "", "", "")
	}
	return NewProductContext(c.Global.Product, c.Global.Project, c.Global.Workspace, c.Global.Region)
}

// GetTimeZone 获取时区配置（如果未配置则返回默认值 "Asia/Shanghai"）
func (c *Config) GetTimeZone() string {
	if c.Server.TimeZone != "" {
		return c.Server.TimeZone
	}
	if c.Global.TimeZone != "" {
		return c.Global.TimeZone
	}
	return "Asia/Shanghai"
}

// GetLanguage 获取语言配置（如果未配置则返回默认值 "zh"）
func (c *Config) GetLanguage() string {
	if c.Server.Language != "" {
		return c.Server.Language
	}
	if c.Global.Language != "" {
		return c.Global.Language
	}
	return "zh"
}
