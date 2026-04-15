package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"sop-chat/internal/client"
	"sop-chat/internal/config"
	"sop-chat/internal/embed"
	"sop-chat/internal/scheduler"
	"sop-chat/pkg/sopchat"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
)

// configUITokenMiddleware 验证配置 UI 访问令牌的中间件
func (s *Server) configUITokenMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.Query("token")
		if token == "" || token != s.configUIToken {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的访问令牌"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// handleConfigUIPage 返回配置管理 UI 页面（携带 token 才能访问）
func (s *Server) handleConfigUIPage(c *gin.Context) {
	token := c.Query("token")
	if token == "" || token != s.configUIToken {
		c.Data(http.StatusUnauthorized, "text/html; charset=utf-8", []byte(`<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="UTF-8"><title>访问被拒绝</title>
<style>body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI','Roboto',system-ui,sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;background:linear-gradient(135deg,#667eea 0%,#764ba2 100%);}
.box{text-align:center;padding:2.5rem 3rem;background:#fff;border-radius:16px;box-shadow:0 20px 60px rgba(102,126,234,0.25);max-width:380px;}
.icon{font-size:2.5rem;margin-bottom:1rem;}
h1{color:#2d3748;font-size:1.2rem;margin-bottom:0.5rem;}p{color:#718096;font-size:0.875rem;line-height:1.6;}</style></head>
<body><div class="box"><div class="icon">🔒</div><h1>访问被拒绝</h1><p>需要有效的访问令牌，请使用服务器启动时输出的链接。</p></div></body></html>`))
		return
	}
	htmlBytes := embed.GetConfigHTML()
	if htmlBytes == nil {
		c.Data(http.StatusServiceUnavailable, "text/html; charset=utf-8", []byte(`<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="UTF-8"><title>配置 UI 未构建</title>
<style>body{font-family:system-ui,sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;background:#f5f7ff}
.box{text-align:center;padding:2rem 2.5rem;background:#fff;border-radius:12px;box-shadow:0 4px 24px rgba(0,0,0,.1);max-width:440px}
h1{color:#2d3748;font-size:1.1rem;margin-bottom:.75rem}
p{color:#718096;font-size:.875rem;line-height:1.7}
code{background:#eef0ff;padding:2px 6px;border-radius:4px;font-size:.8rem}</style></head>
<body><div class="box"><h1>配置 UI 尚未构建</h1>
<p>请先执行 <code>make build-frontend</code> 构建前端，<br>或将 <code>frontend/public/config.html</code> 复制到<br><code>backend/internal/embed/frontend/config.html</code> 后重新编译。</p></div></body></html>`))
		return
	}
	c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
	c.Data(http.StatusOK, "text/html; charset=utf-8", htmlBytes)
}

// configUIResponse 是返回给前端的结构化配置数据（不暴露文件路径）
type configUIResponse struct {
	Server         configUIServer          `json:"server"`
	CloudAccounts  []configUICloudAccount  `json:"cloudAccounts"`
	Auth           configUIAuth            `json:"auth"`
	DingTalk       []configUIDingTalk      `json:"dingtalk"`
	Feishu         []configUIFeishu        `json:"feishu"`
	WeCom          []configUIWeCom         `json:"wecom"`
	WeComBot       []configUIWeComBot      `json:"wecomBot"`
	OpenAIEnabled  bool                    `json:"openaiEnabled"`
	OpenAI         configUIOpenAI          `json:"openai"`
	ScheduledTasks []configUIScheduledTask `json:"scheduledTasks"`
}

// configUIScheduledTask 定时任务配置（UI 层）
type configUIScheduledTask struct {
	Name           string            `json:"name"`
	Enabled        bool              `json:"enabled"`
	Cron           string            `json:"cron"`
	Prompt         string            `json:"prompt"`
	EmployeeName   string            `json:"employeeName"`
	CloudAccountID string            `json:"cloudAccountId"`
	ConciseReply   bool              `json:"conciseReply"`
	Product        string            `json:"product"`
	Project        string            `json:"project"`
	Workspace      string            `json:"workspace"`
	Region         string            `json:"region"`
	Webhook        *configUIWebhook  `json:"webhook,omitempty"` // 向后兼容：旧前端可能只传单个
	Webhooks       []configUIWebhook `json:"webhooks"`
}

// configUIWebhook Webhook 配置（UI 层）
type configUIWebhook struct {
	Type    string `json:"type"`
	URL     string `json:"url"`
	MsgType string `json:"msgType"`
	Title   string `json:"title"`
	To      string `json:"to,omitempty"`
}

// effectiveWebhooks 返回有效的 webhook 列表，兼容旧前端只传 webhook 单个字段的情况。
func (t *configUIScheduledTask) effectiveWebhooks() []configUIWebhook {
	if len(t.Webhooks) > 0 {
		return t.Webhooks
	}
	if t.Webhook != nil && t.Webhook.URL != "" {
		return []configUIWebhook{*t.Webhook}
	}
	return nil
}

type configUIServer struct {
	Host                string `json:"host"`
	Port                int    `json:"port"`
	TimeZone            string `json:"timeZone"`
	Language            string `json:"language"`
	BindThreadToProcess *bool  `json:"bindThreadToProcess,omitempty"`
}

type configUICloudAccount struct {
	ID              string   `json:"id"`
	Provider        string   `json:"provider"`
	Aliases         []string `json:"aliases"`
	AccessKeyId     string   `json:"accessKeyId"`
	AccessKeySecret string   `json:"accessKeySecret"`
	Endpoint        string   `json:"endpoint"`
}

type configUIAuth struct {
	Methods      []string       `json:"methods"`                // 鉴权链，对应 auth.methods
	JWTSecretKey string         `json:"jwtSecretKey"`           // maps to auth.jwt.secretKey
	JWTExpiresIn string         `json:"jwtExpiresIn"`           // maps to auth.jwt.expiresIn
	PasswordSalt string         `json:"passwordSalt,omitempty"` // MD5(salt+password)
	Local        *configUILocal `json:"local,omitempty"`
}

type configUILocal struct {
	Users []configUIUser `json:"users"`
	Roles []configUIRole `json:"roles"`
}

type configUIUser struct {
	Name     string `json:"name"`
	Password string `json:"password"` // MD5 哈希值
}

type configUIRole struct {
	Name  string   `json:"name"`
	Users []string `json:"users"`
}

type configUIConversationRoute struct {
	ConversationTitle string `json:"conversationTitle"`
	EmployeeName      string `json:"employeeName"`
	Product           string `json:"product"`
	Project           string `json:"project"`
	Workspace         string `json:"workspace"`
	Region            string `json:"region"`
}

type configUICloudAccountRoute struct {
	CloudAccountID string `json:"cloudAccountId"`
	EmployeeName   string `json:"employeeName"`
	Product        string `json:"product"`
	Project        string `json:"project"`
	Workspace      string `json:"workspace"`
	Region         string `json:"region"`
}

type configUIDingTalk struct {
	Enabled              bool                        `json:"enabled"`
	Name                 string                      `json:"name"`
	ClientId             string                      `json:"clientId"`
	ClientSecret         string                      `json:"clientSecret"`
	EmployeeName         string                      `json:"employeeName"`
	CloudAccountID       string                      `json:"cloudAccountId"`
	ConciseReply         bool                        `json:"conciseReply"`
	CardTemplateId       string                      `json:"cardTemplateId"`
	CardContentKey       string                      `json:"cardContentKey"`
	Product              string                      `json:"product"`
	Project              string                      `json:"project"`
	Workspace            string                      `json:"workspace"`
	Region               string                      `json:"region"`
	AllowedGroupUsers    []string                    `json:"allowedGroupUsers"`
	AllowedDirectUsers   []string                    `json:"allowedDirectUsers"`
	AllowedConversations []string                    `json:"allowedConversations"`
	ConversationRoutes   []configUIConversationRoute `json:"conversationRoutes"`
	CloudAccountRoutes   []configUICloudAccountRoute `json:"cloudAccountRoutes"`
}

type configUIFeishu struct {
	Enabled            bool                        `json:"enabled"`
	Name               string                      `json:"name"`
	AppID              string                      `json:"appId"`
	AppSecret          string                      `json:"appSecret"`
	VerificationToken  string                      `json:"verificationToken"`
	EventEncryptKey    string                      `json:"eventEncryptKey"`
	EmployeeName       string                      `json:"employeeName"`
	CloudAccountID     string                      `json:"cloudAccountId"`
	ConciseReply       bool                        `json:"conciseReply"`
	Product            string                      `json:"product"`
	Project            string                      `json:"project"`
	Workspace          string                      `json:"workspace"`
	Region             string                      `json:"region"`
	AllowedUsers       []string                    `json:"allowedUsers"`
	AllowedChats       []string                    `json:"allowedChats"`
	CloudAccountRoutes []configUICloudAccountRoute `json:"cloudAccountRoutes"`
}

type configUIWeCom struct {
	Enabled        bool     `json:"enabled"`
	Name           string   `json:"name"`
	CorpID         string   `json:"corpId"`
	AgentID        int      `json:"agentId"`
	Secret         string   `json:"secret"`
	Token          string   `json:"token"`
	EncodingAESKey string   `json:"encodingAESKey"`
	CallbackPort   int      `json:"callbackPort"`
	CallbackPath   string   `json:"callbackPath"`
	EmployeeName   string   `json:"employeeName"`
	CloudAccountID string   `json:"cloudAccountId"`
	ConciseReply   bool     `json:"conciseReply"`
	Product        string   `json:"product"`
	Project        string   `json:"project"`
	Workspace      string   `json:"workspace"`
	Region         string   `json:"region"`
	AllowedUsers   []string `json:"allowedUsers"`
	WebhookURL     string   `json:"webhookUrl"`
	BotLongConn    struct {
		Enabled              bool   `json:"enabled"`
		BotID                string `json:"botId"`
		BotSecret            string `json:"botSecret"`
		URL                  string `json:"url"`
		PingIntervalSec      int    `json:"pingIntervalSec"`
		ReconnectDelaySec    int    `json:"reconnectDelaySec"`
		MaxReconnectDelaySec int    `json:"maxReconnectDelaySec"`
	} `json:"botLongConn"`
	CloudAccountRoutes []configUICloudAccountRoute `json:"cloudAccountRoutes"`
}

type configUIWeComBot struct {
	Enabled              bool                        `json:"enabled"`
	Name                 string                      `json:"name"`
	BotID                string                      `json:"botId"`
	BotSecret            string                      `json:"botSecret"`
	EmployeeName         string                      `json:"employeeName"`
	CloudAccountID       string                      `json:"cloudAccountId"`
	ConciseReply         bool                        `json:"conciseReply"`
	Product              string                      `json:"product"`
	Project              string                      `json:"project"`
	Workspace            string                      `json:"workspace"`
	Region               string                      `json:"region"`
	URL                  string                      `json:"url"`
	PingIntervalSec      int                         `json:"pingIntervalSec"`
	ReconnectDelaySec    int                         `json:"reconnectDelaySec"`
	MaxReconnectDelaySec int                         `json:"maxReconnectDelaySec"`
	CloudAccountRoutes   []configUICloudAccountRoute `json:"cloudAccountRoutes"`
}

type configUIOpenAI struct {
	APIKeys []string `json:"apiKeys"`
}

type configUIFieldPresence struct {
	Server         bool
	CloudAccounts  bool
	Auth           bool
	DingTalk       bool
	Feishu         bool
	WeCom          bool
	WeComBot       bool
	OpenAIEnabled  bool
	OpenAI         bool
	ScheduledTasks bool
}

func detectConfigUIFieldPresence(raw map[string]json.RawMessage) configUIFieldPresence {
	_, hasServer := raw["server"]
	_, hasCloudAccounts := raw["cloudAccounts"]
	_, hasAuth := raw["auth"]
	_, hasDingTalk := raw["dingtalk"]
	_, hasFeishu := raw["feishu"]
	_, hasWeCom := raw["wecom"]
	_, hasWeComBot := raw["wecomBot"]
	_, hasOpenAIEnabled := raw["openaiEnabled"]
	_, hasOpenAI := raw["openai"]
	_, hasScheduledTasks := raw["scheduledTasks"]
	return configUIFieldPresence{
		Server:         hasServer,
		CloudAccounts:  hasCloudAccounts,
		Auth:           hasAuth,
		DingTalk:       hasDingTalk,
		Feishu:         hasFeishu,
		WeCom:          hasWeCom,
		WeComBot:       hasWeComBot,
		OpenAIEnabled:  hasOpenAIEnabled,
		OpenAI:         hasOpenAI,
		ScheduledTasks: hasScheduledTasks,
	}
}

func cloneConfigForSave(existing *config.Config) (*config.Config, error) {
	if existing == nil {
		return &config.Config{}, nil
	}
	data, err := yaml.Marshal(existing)
	if err != nil {
		return nil, fmt.Errorf("序列化现有配置失败: %w", err)
	}
	var cloned config.Config
	if err := yaml.Unmarshal(data, &cloned); err != nil {
		return nil, fmt.Errorf("复制现有配置失败: %w", err)
	}
	return &cloned, nil
}

func buildConfigFromUI(existing *config.Config, req configUIResponse, presence configUIFieldPresence) (*config.Config, error) {
	cfg, err := cloneConfigForSave(existing)
	if err != nil {
		return nil, err
	}

	if presence.Server {
		cfg.Server.Host = req.Server.Host
		cfg.Server.Port = req.Server.Port
		cfg.Server.TimeZone = req.Server.TimeZone
		cfg.Server.Language = req.Server.Language
		if req.Server.BindThreadToProcess != nil {
			cfg.Server.BindThreadToProcess = req.Server.BindThreadToProcess
		}
		// 保存为新结构时，不再重复写回 legacy 的服务配置字段。
		cfg.Global.Host = ""
		cfg.Global.Port = 0
		cfg.Global.TimeZone = ""
		cfg.Global.Language = ""
		cfg.Global.BindThreadToProcess = nil
	}

	if presence.CloudAccounts {
		cfg.CloudAccounts = nil
		if len(req.CloudAccounts) > 0 {
			cfg.CloudAccounts = make([]config.CloudAccountConfig, 0, len(req.CloudAccounts))
			for _, account := range req.CloudAccounts {
				if account.AccessKeyId == "" && account.AccessKeySecret == "" && account.Endpoint == "" {
					continue
				}
				accountID := config.NormalizeCloudAccountID(account.ID)
				provider := strings.TrimSpace(strings.ToLower(account.Provider))
				if provider == "" {
					provider = "aliyun"
				}
				cfg.CloudAccounts = append(cfg.CloudAccounts, config.CloudAccountConfig{
					ID:              accountID,
					Provider:        provider,
					Aliases:         account.Aliases,
					AccessKeyId:     account.AccessKeyId,
					AccessKeySecret: account.AccessKeySecret,
					Endpoint:        account.Endpoint,
				})
			}
		}
		// 只有在 UI 已显式写入 cloudAccounts 时，才清理 legacy AK/SK，避免部分页面/旧前端把配置抹空。
		if len(cfg.CloudAccounts) > 0 {
			cfg.Global.AccessKeyId = ""
			cfg.Global.AccessKeySecret = ""
			cfg.Global.Endpoint = ""
		}
	}

	if presence.Auth {
		ldapCfg := cfg.Auth.LDAP
		oidcCfg := cfg.Auth.OIDC
		cfg.Auth.Methods = req.Auth.Methods
		cfg.Auth.JWT = config.JWTConfig{
			SecretKey: req.Auth.JWTSecretKey,
			ExpiresIn: req.Auth.JWTExpiresIn,
		}
		cfg.Auth.PasswordSalt = req.Auth.PasswordSalt
		cfg.Auth.LDAP = ldapCfg
		cfg.Auth.OIDC = oidcCfg
		if req.Auth.Local != nil {
			cfg.Auth.BuiltinUsers = make([]config.UserConfig, len(req.Auth.Local.Users))
			cfg.Auth.Roles = make([]config.RoleConfig, len(req.Auth.Local.Roles))
			for i, u := range req.Auth.Local.Users {
				cfg.Auth.BuiltinUsers[i] = config.UserConfig{Name: u.Name, Password: u.Password}
			}
			for i, r := range req.Auth.Local.Roles {
				cfg.Auth.Roles[i] = config.RoleConfig{Name: r.Name, Users: r.Users}
			}
		}
	}

	if presence.DingTalk {
		if cfg.Channels == nil {
			cfg.Channels = &config.ChannelsConfig{}
		}
		cfg.Channels.DingTalk = make([]config.DingTalkConfig, 0, len(req.DingTalk))
		for _, dt := range req.DingTalk {
			if dt.ClientId != "" || dt.ClientSecret != "" || dt.EmployeeName != "" {
				routes := make([]config.ConversationRoute, 0, len(dt.ConversationRoutes))
				for _, r := range dt.ConversationRoutes {
					if r.ConversationTitle != "" && r.EmployeeName != "" {
						routes = append(routes, config.ConversationRoute{
							ConversationTitle: r.ConversationTitle,
							EmployeeName:      r.EmployeeName,
							Product:           r.Product,
							Project:           r.Project,
							Workspace:         r.Workspace,
							Region:            r.Region,
						})
					}
				}
				cfg.Channels.DingTalk = append(cfg.Channels.DingTalk, config.DingTalkConfig{
					Enabled:              dt.Enabled,
					Name:                 dt.Name,
					ClientId:             dt.ClientId,
					ClientSecret:         dt.ClientSecret,
					EmployeeName:         dt.EmployeeName,
					CloudAccountID:       config.NormalizeCloudAccountID(dt.CloudAccountID),
					ConciseReply:         dt.ConciseReply,
					CardTemplateId:       dt.CardTemplateId,
					CardContentKey:       dt.CardContentKey,
					Product:              dt.Product,
					Project:              dt.Project,
					Workspace:            dt.Workspace,
					Region:               dt.Region,
					AllowedGroupUsers:    dt.AllowedGroupUsers,
					AllowedDirectUsers:   dt.AllowedDirectUsers,
					AllowedConversations: dt.AllowedConversations,
					ConversationRoutes:   routes,
					CloudAccountRoutes:   fromUICloudAccountRoutes(dt.CloudAccountRoutes),
				})
			}
		}
	}

	if presence.Feishu {
		if cfg.Channels == nil {
			cfg.Channels = &config.ChannelsConfig{}
		}
		cfg.Channels.Feishu = make([]config.FeishuConfig, 0, len(req.Feishu))
		for _, ft := range req.Feishu {
			if ft.AppID != "" || ft.AppSecret != "" || ft.EmployeeName != "" {
				allowedUsers := ft.AllowedUsers
				if allowedUsers == nil {
					allowedUsers = []string{}
				}
				allowedChats := ft.AllowedChats
				if allowedChats == nil {
					allowedChats = []string{}
				}
				cfg.Channels.Feishu = append(cfg.Channels.Feishu, config.FeishuConfig{
					Enabled:            ft.Enabled,
					Name:               ft.Name,
					AppID:              ft.AppID,
					AppSecret:          ft.AppSecret,
					VerificationToken:  ft.VerificationToken,
					EventEncryptKey:    ft.EventEncryptKey,
					EmployeeName:       ft.EmployeeName,
					CloudAccountID:     config.NormalizeCloudAccountID(ft.CloudAccountID),
					ConciseReply:       ft.ConciseReply,
					Product:            ft.Product,
					Project:            ft.Project,
					Workspace:          ft.Workspace,
					Region:             ft.Region,
					AllowedUsers:       allowedUsers,
					AllowedChats:       allowedChats,
					CloudAccountRoutes: fromUICloudAccountRoutes(ft.CloudAccountRoutes),
				})
			}
		}
	}

	if presence.WeCom {
		if cfg.Channels == nil {
			cfg.Channels = &config.ChannelsConfig{}
		}
		cfg.Channels.WeCom = make([]config.WeComConfig, 0, len(req.WeCom))
		for _, wc := range req.WeCom {
			if wc.CorpID != "" || wc.Secret != "" || wc.EmployeeName != "" {
				allowedUsers := wc.AllowedUsers
				if allowedUsers == nil {
					allowedUsers = []string{}
				}
				cfg.Channels.WeCom = append(cfg.Channels.WeCom, config.WeComConfig{
					Enabled:            wc.Enabled,
					Name:               wc.Name,
					CorpID:             wc.CorpID,
					AgentID:            wc.AgentID,
					Secret:             wc.Secret,
					Token:              wc.Token,
					EncodingAESKey:     wc.EncodingAESKey,
					CallbackPort:       wc.CallbackPort,
					CallbackPath:       wc.CallbackPath,
					EmployeeName:       wc.EmployeeName,
					CloudAccountID:     config.NormalizeCloudAccountID(wc.CloudAccountID),
					ConciseReply:       wc.ConciseReply,
					Product:            wc.Product,
					Project:            wc.Project,
					Workspace:          wc.Workspace,
					Region:             wc.Region,
					AllowedUsers:       allowedUsers,
					WebhookURL:         wc.WebhookURL,
					CloudAccountRoutes: fromUICloudAccountRoutes(wc.CloudAccountRoutes),
				})
				if wc.BotLongConn.Enabled || wc.BotLongConn.BotID != "" || wc.BotLongConn.BotSecret != "" || wc.BotLongConn.URL != "" {
					last := &cfg.Channels.WeCom[len(cfg.Channels.WeCom)-1]
					last.BotLongConn = &config.WeComBotLongConnConfig{
						Enabled:              wc.BotLongConn.Enabled,
						BotID:                wc.BotLongConn.BotID,
						BotSecret:            wc.BotLongConn.BotSecret,
						URL:                  wc.BotLongConn.URL,
						PingIntervalSec:      wc.BotLongConn.PingIntervalSec,
						ReconnectDelaySec:    wc.BotLongConn.ReconnectDelaySec,
						MaxReconnectDelaySec: wc.BotLongConn.MaxReconnectDelaySec,
					}
				}
			}
		}
	}

	if presence.WeComBot {
		if cfg.Channels == nil {
			cfg.Channels = &config.ChannelsConfig{}
		}
		cfg.Channels.WeComBot = make([]config.WeComBotConfig, 0, len(req.WeComBot))
		for _, wb := range req.WeComBot {
			if wb.BotID != "" || wb.BotSecret != "" || wb.EmployeeName != "" {
				cfg.Channels.WeComBot = append(cfg.Channels.WeComBot, config.WeComBotConfig{
					Enabled:              wb.Enabled,
					Name:                 wb.Name,
					BotID:                wb.BotID,
					BotSecret:            wb.BotSecret,
					EmployeeName:         wb.EmployeeName,
					CloudAccountID:       config.NormalizeCloudAccountID(wb.CloudAccountID),
					ConciseReply:         wb.ConciseReply,
					Product:              wb.Product,
					Project:              wb.Project,
					Workspace:            wb.Workspace,
					Region:               wb.Region,
					URL:                  wb.URL,
					PingIntervalSec:      wb.PingIntervalSec,
					ReconnectDelaySec:    wb.ReconnectDelaySec,
					MaxReconnectDelaySec: wb.MaxReconnectDelaySec,
					CloudAccountRoutes:   fromUICloudAccountRoutes(wb.CloudAccountRoutes),
				})
			}
		}
	}

	if presence.OpenAI || presence.OpenAIEnabled {
		cfg.OpenAI = nil
		if len(req.OpenAI.APIKeys) > 0 {
			cfg.OpenAI = &config.OpenAICompatConfig{
				Enabled: req.OpenAIEnabled,
				APIKeys: req.OpenAI.APIKeys,
			}
		}
		if cfg.OpenAI == nil && req.OpenAIEnabled {
			cfg.OpenAI = &config.OpenAICompatConfig{Enabled: true}
		}
	}

	if presence.ScheduledTasks {
		cfg.ScheduledTasks = nil
		for _, t := range req.ScheduledTasks {
			webhooks := t.effectiveWebhooks()
			if t.Name != "" || t.EmployeeName != "" || len(webhooks) > 0 {
				taskProduct := config.ResolveScheduledTaskProduct(t.Product, t.Project, t.Workspace, cfg.GetLegacyProduct())
				cfgWebhooks := make([]config.WebhookConfig, len(webhooks))
				for j, wh := range webhooks {
					cfgWebhooks[j] = config.WebhookConfig{
						Type:    wh.Type,
						URL:     wh.URL,
						MsgType: wh.MsgType,
						Title:   wh.Title,
						To:      wh.To,
					}
				}
				cfg.ScheduledTasks = append(cfg.ScheduledTasks, config.ScheduledTaskConfig{
					Name:           t.Name,
					Enabled:        t.Enabled,
					Cron:           t.Cron,
					Prompt:         t.Prompt,
					EmployeeName:   t.EmployeeName,
					CloudAccountID: config.NormalizeCloudAccountID(t.CloudAccountID),
					ConciseReply:   t.ConciseReply,
					Product:        taskProduct,
					Project:        t.Project,
					Workspace:      t.Workspace,
					Region:         t.Region,
					Webhooks:       cfgWebhooks,
				})
			}
		}
	}

	if cfg.Channels != nil &&
		len(cfg.Channels.DingTalk) == 0 &&
		len(cfg.Channels.Feishu) == 0 &&
		len(cfg.Channels.WeCom) == 0 &&
		len(cfg.Channels.WeComBot) == 0 {
		cfg.Channels = nil
	}

	return cfg, nil
}

func toUIConversationRoutes(routes []config.ConversationRoute, base config.ProductContext) []configUIConversationRoute {
	result := make([]configUIConversationRoute, 0, len(routes))
	for _, route := range routes {
		ctx := config.MergeProductContext(base, route.Product, route.Project, route.Workspace, route.Region)
		result = append(result, configUIConversationRoute{
			ConversationTitle: route.ConversationTitle,
			EmployeeName:      route.EmployeeName,
			Product:           ctx.Product,
			Project:           ctx.Project,
			Workspace:         ctx.Workspace,
			Region:            ctx.Region,
		})
	}
	return result
}

func toUICloudAccountRoutes(routes []config.CloudAccountRoute, base config.ProductContext) []configUICloudAccountRoute {
	result := make([]configUICloudAccountRoute, 0, len(routes))
	for _, route := range routes {
		ctx := config.MergeProductContext(base, route.Product, route.Project, route.Workspace, route.Region)
		result = append(result, configUICloudAccountRoute{
			CloudAccountID: config.NormalizeCloudAccountID(route.CloudAccountID),
			EmployeeName:   route.EmployeeName,
			Product:        ctx.Product,
			Project:        ctx.Project,
			Workspace:      ctx.Workspace,
			Region:         ctx.Region,
		})
	}
	return result
}

func fromUICloudAccountRoutes(routes []configUICloudAccountRoute) []config.CloudAccountRoute {
	result := make([]config.CloudAccountRoute, 0, len(routes))
	for _, route := range routes {
		if strings.TrimSpace(route.CloudAccountID) == "" || strings.TrimSpace(route.EmployeeName) == "" {
			continue
		}
		result = append(result, config.CloudAccountRoute{
			CloudAccountID: config.NormalizeCloudAccountID(route.CloudAccountID),
			EmployeeName:   route.EmployeeName,
			Product:        route.Product,
			Project:        route.Project,
			Workspace:      route.Workspace,
			Region:         route.Region,
		})
	}
	return result
}

// handleGetConfig 返回结构化的配置 JSON（不包含文件路径）
func (s *Server) handleGetConfig(c *gin.Context) {
	s.mu.RLock()
	cfg := s.globalConfig
	s.mu.RUnlock()

	if cfg == nil {
		// 配置文件不存在时返回空配置，让前端呈现空表单供用户填写
		c.JSON(http.StatusOK, configUIResponse{
			CloudAccounts:  []configUICloudAccount{},
			DingTalk:       []configUIDingTalk{},
			Feishu:         []configUIFeishu{},
			WeCom:          []configUIWeCom{},
			WeComBot:       []configUIWeComBot{},
			OpenAI:         configUIOpenAI{APIKeys: []string{}},
			ScheduledTasks: []configUIScheduledTask{},
		})
		return
	}

	resp := configUIResponse{
		Server: configUIServer{
			Host:     cfg.GetHost(),
			Port:     cfg.GetPort(),
			TimeZone: cfg.GetTimeZone(),
			Language: cfg.GetLanguage(),
			BindThreadToProcess: func() *bool {
				v := cfg.BindThreadToProcess()
				return &v
			}(),
		},
		Auth: configUIAuth{
			Methods:      cfg.Auth.Methods,
			JWTSecretKey: cfg.Auth.JWT.SecretKey,
			JWTExpiresIn: cfg.Auth.JWT.ExpiresIn,
			PasswordSalt: cfg.Auth.PasswordSalt,
		},
		OpenAIEnabled: cfg.OpenAI != nil && cfg.OpenAI.Enabled,
	}

	if len(cfg.CloudAccounts) > 0 {
		resp.CloudAccounts = make([]configUICloudAccount, len(cfg.CloudAccounts))
		for i, account := range cfg.CloudAccounts {
			resp.CloudAccounts[i] = configUICloudAccount{
				ID:              config.NormalizeCloudAccountID(account.ID),
				Provider:        account.Provider,
				Aliases:         account.Aliases,
				AccessKeyId:     account.AccessKeyId,
				AccessKeySecret: account.AccessKeySecret,
				Endpoint:        account.Endpoint,
			}
		}
	} else {
		resp.CloudAccounts = []configUICloudAccount{}
	}

	if len(cfg.Auth.BuiltinUsers) > 0 || len(cfg.Auth.Roles) > 0 {
		local := &configUILocal{
			Users: make([]configUIUser, len(cfg.Auth.BuiltinUsers)),
			Roles: make([]configUIRole, len(cfg.Auth.Roles)),
		}
		for i, u := range cfg.Auth.BuiltinUsers {
			local.Users[i] = configUIUser{Name: u.Name, Password: u.Password}
		}
		for i, r := range cfg.Auth.Roles {
			users := r.Users
			if users == nil {
				users = []string{}
			}
			local.Roles[i] = configUIRole{Name: r.Name, Users: users}
		}
		resp.Auth.Local = local
	}

	// 始终返回所有钉钉实例（包括 enabled=false 的，凭据应保留显示）
	if cfg.Channels != nil && len(cfg.Channels.DingTalk) > 0 {
		resp.DingTalk = make([]configUIDingTalk, len(cfg.Channels.DingTalk))
		legacyDefaults := cfg.GetLegacyProductContext()
		for i, dt := range cfg.Channels.DingTalk {
			base := config.MergeProductContext(legacyDefaults, dt.Product, dt.Project, dt.Workspace, dt.Region)
			resp.DingTalk[i] = configUIDingTalk{
				Enabled:              dt.Enabled,
				Name:                 dt.Name,
				ClientId:             dt.ClientId,
				ClientSecret:         dt.ClientSecret,
				EmployeeName:         dt.EmployeeName,
				CloudAccountID:       config.NormalizeCloudAccountID(dt.CloudAccountID),
				ConciseReply:         dt.ConciseReply,
				CardTemplateId:       dt.CardTemplateId,
				CardContentKey:       dt.CardContentKey,
				Product:              base.Product,
				Project:              base.Project,
				Workspace:            base.Workspace,
				Region:               base.Region,
				AllowedGroupUsers:    dt.AllowedGroupUsers,
				AllowedDirectUsers:   dt.AllowedDirectUsers,
				AllowedConversations: dt.AllowedConversations,
				ConversationRoutes:   toUIConversationRoutes(dt.ConversationRoutes, base),
				CloudAccountRoutes:   toUICloudAccountRoutes(dt.CloudAccountRoutes, base),
			}
		}
	} else {
		resp.DingTalk = []configUIDingTalk{}
	}

	// 飞书配置
	if cfg.Channels != nil && len(cfg.Channels.Feishu) > 0 {
		resp.Feishu = make([]configUIFeishu, len(cfg.Channels.Feishu))
		legacyDefaults := cfg.GetLegacyProductContext()
		for i, ft := range cfg.Channels.Feishu {
			base := config.MergeProductContext(legacyDefaults, ft.Product, ft.Project, ft.Workspace, ft.Region)
			resp.Feishu[i] = configUIFeishu{
				Enabled:            ft.Enabled,
				Name:               ft.Name,
				AppID:              ft.AppID,
				AppSecret:          ft.AppSecret,
				VerificationToken:  ft.VerificationToken,
				EventEncryptKey:    ft.EventEncryptKey,
				EmployeeName:       ft.EmployeeName,
				CloudAccountID:     config.NormalizeCloudAccountID(ft.CloudAccountID),
				ConciseReply:       ft.ConciseReply,
				Product:            base.Product,
				Project:            base.Project,
				Workspace:          base.Workspace,
				Region:             base.Region,
				AllowedUsers:       ft.AllowedUsers,
				AllowedChats:       ft.AllowedChats,
				CloudAccountRoutes: toUICloudAccountRoutes(ft.CloudAccountRoutes, base),
			}
		}
	} else {
		resp.Feishu = []configUIFeishu{}
	}

	// 企业微信配置
	if cfg.Channels != nil && len(cfg.Channels.WeCom) > 0 {
		resp.WeCom = make([]configUIWeCom, len(cfg.Channels.WeCom))
		legacyDefaults := cfg.GetLegacyProductContext()
		for i, wc := range cfg.Channels.WeCom {
			base := config.MergeProductContext(legacyDefaults, wc.Product, wc.Project, wc.Workspace, wc.Region)
			resp.WeCom[i] = configUIWeCom{
				Enabled:            wc.Enabled,
				Name:               wc.Name,
				CorpID:             wc.CorpID,
				AgentID:            wc.AgentID,
				Secret:             wc.Secret,
				Token:              wc.Token,
				EncodingAESKey:     wc.EncodingAESKey,
				CallbackPort:       wc.CallbackPort,
				CallbackPath:       wc.CallbackPath,
				EmployeeName:       wc.EmployeeName,
				CloudAccountID:     config.NormalizeCloudAccountID(wc.CloudAccountID),
				ConciseReply:       wc.ConciseReply,
				Product:            base.Product,
				Project:            base.Project,
				Workspace:          base.Workspace,
				Region:             base.Region,
				AllowedUsers:       wc.AllowedUsers,
				WebhookURL:         wc.WebhookURL,
				CloudAccountRoutes: toUICloudAccountRoutes(wc.CloudAccountRoutes, base),
			}
			if wc.BotLongConn != nil {
				resp.WeCom[i].BotLongConn.Enabled = wc.BotLongConn.Enabled
				resp.WeCom[i].BotLongConn.BotID = wc.BotLongConn.BotID
				resp.WeCom[i].BotLongConn.BotSecret = wc.BotLongConn.BotSecret
				resp.WeCom[i].BotLongConn.URL = wc.BotLongConn.URL
				resp.WeCom[i].BotLongConn.PingIntervalSec = wc.BotLongConn.PingIntervalSec
				resp.WeCom[i].BotLongConn.ReconnectDelaySec = wc.BotLongConn.ReconnectDelaySec
				resp.WeCom[i].BotLongConn.MaxReconnectDelaySec = wc.BotLongConn.MaxReconnectDelaySec
			}
		}
	} else {
		resp.WeCom = []configUIWeCom{}
	}

	// 企业微信群聊机器人配置（独立渠道）
	if cfg.Channels != nil && len(cfg.Channels.WeComBot) > 0 {
		resp.WeComBot = make([]configUIWeComBot, len(cfg.Channels.WeComBot))
		legacyDefaults := cfg.GetLegacyProductContext()
		for i, wb := range cfg.Channels.WeComBot {
			base := config.MergeProductContext(legacyDefaults, wb.Product, wb.Project, wb.Workspace, wb.Region)
			resp.WeComBot[i] = configUIWeComBot{
				Enabled:              wb.Enabled,
				Name:                 wb.Name,
				BotID:                wb.BotID,
				BotSecret:            wb.BotSecret,
				EmployeeName:         wb.EmployeeName,
				CloudAccountID:       config.NormalizeCloudAccountID(wb.CloudAccountID),
				ConciseReply:         wb.ConciseReply,
				Product:              base.Product,
				Project:              base.Project,
				Workspace:            base.Workspace,
				Region:               base.Region,
				URL:                  wb.URL,
				PingIntervalSec:      wb.PingIntervalSec,
				ReconnectDelaySec:    wb.ReconnectDelaySec,
				MaxReconnectDelaySec: wb.MaxReconnectDelaySec,
				CloudAccountRoutes:   toUICloudAccountRoutes(wb.CloudAccountRoutes, base),
			}
		}
	} else {
		resp.WeComBot = []configUIWeComBot{}
	}

	// 始终读取 OpenAI 字段（即使 enabled=false，密钥也应保留显示）
	if cfg.OpenAI != nil {
		keys := cfg.OpenAI.APIKeys
		if keys == nil {
			keys = []string{}
		}
		resp.OpenAI = configUIOpenAI{APIKeys: keys}
	} else {
		resp.OpenAI = configUIOpenAI{APIKeys: []string{}}
	}

	// 定时任务配置
	if len(cfg.ScheduledTasks) > 0 {
		resp.ScheduledTasks = make([]configUIScheduledTask, len(cfg.ScheduledTasks))
		for i, t := range cfg.ScheduledTasks {
			effectiveProduct := config.ResolveScheduledTaskProduct(t.Product, t.Project, t.Workspace, cfg.GetLegacyProduct())
			webhooks := t.EffectiveWebhooks()
			uiWebhooks := make([]configUIWebhook, len(webhooks))
			for j, wh := range webhooks {
				uiWebhooks[j] = configUIWebhook{
					Type:    wh.Type,
					URL:     wh.URL,
					MsgType: wh.MsgType,
					Title:   wh.Title,
					To:      wh.To,
				}
			}
			if len(uiWebhooks) == 0 {
				uiWebhooks = []configUIWebhook{}
			}
			resp.ScheduledTasks[i] = configUIScheduledTask{
				Name:           t.Name,
				Enabled:        t.Enabled,
				Cron:           t.Cron,
				Prompt:         t.Prompt,
				EmployeeName:   t.EmployeeName,
				CloudAccountID: config.NormalizeCloudAccountID(t.CloudAccountID),
				ConciseReply:   t.ConciseReply,
				Product:        effectiveProduct,
				Project:        t.Project,
				Workspace:      t.Workspace,
				Region:         t.Region,
				Webhooks:       uiWebhooks,
			}
		}
	} else {
		resp.ScheduledTasks = []configUIScheduledTask{}
	}

	c.JSON(http.StatusOK, resp)
}

// handleSaveConfig 接收结构化 JSON 配置，转换为 Config 结构体，保存并热重载
func (s *Server) handleSaveConfig(c *gin.Context) {
	if s.configPath == "" {
		// 首次保存时尚无配置文件，在工作目录下创建 config.yaml
		s.configPath = "config.yaml"
		log.Printf("首次保存配置，将创建新文件: %s", s.configPath)
	}

	body, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取请求失败: " + err.Error()})
		return
	}

	var req configUIResponse
	if err := json.Unmarshal(body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误: " + err.Error()})
		return
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误: " + err.Error()})
		return
	}

	s.mu.RLock()
	existing := s.globalConfig
	s.mu.RUnlock()

	cfg, err := buildConfigFromUI(existing, req, detectConfigUIFieldPresence(raw))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 保存到文件
	if err := config.SaveConfig(s.configPath, cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("配置已保存: %s，开始热重载...", s.configPath)

	// 热重载内存中的配置
	if err := s.reloadConfig(); err != nil {
		log.Printf("热重载失败: %v", err)
		c.JSON(http.StatusOK, gin.H{
			"message": "配置已保存，但热重载失败（" + err.Error() + "），请手动重启服务器",
			"warning": true,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "配置已保存并成功应用，无需重启"})
}

// handleTriggerTask 立即执行一个定时任务（使用前端传入的当前表单值，无需先保存）
func (s *Server) handleTriggerTask(c *gin.Context) {
	var req configUIScheduledTask
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误: " + err.Error()})
		return
	}
	if req.EmployeeName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "employeeName 不能为空"})
		return
	}
	if req.Prompt == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "prompt 不能为空"})
		return
	}

	s.mu.RLock()
	cfg := s.config
	globalCfg := s.globalConfig
	s.mu.RUnlock()

	var (
		clientCfg *config.ClientConfig
		err       error
	)
	if globalCfg != nil {
		clientCfg, err = globalCfg.ResolveClientConfig(req.CloudAccountID)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"ok": false, "error": "CMS 凭据未配置或云账号无效: " + err.Error()})
			return
		}
	} else if cfg != nil && cfg.AccessKeyId != "" {
		// 兼容极端场景：globalCfg 暂不可用时，回退到旧的单账号缓存配置
		clientCfg = &config.ClientConfig{
			CloudAccountID:  config.DefaultCloudAccountID,
			AccessKeyId:     cfg.AccessKeyId,
			AccessKeySecret: cfg.AccessKeySecret,
			Endpoint:        cfg.Endpoint,
		}
	} else {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "CMS 凭据未配置，请先在基础设置中新增 cloudAccounts"})
		return
	}

	// 与保存/Cron 使用同一 Resolve，保证与页面「对接产品」一致（仅 cms|sls）
	taskProduct := config.ResolveScheduledTaskProduct(req.Product, req.Project, req.Workspace, clientCfg.Product)
	taskProject := req.Project
	taskWorkspace := req.Workspace
	taskRegion := req.Region
	fullPrompt := req.Prompt
	if req.ConciseReply {
		fullPrompt += "\n\n简化最终输出 适合聊天工具上阅读"
	}
	promptLog := scheduler.PromptForLog(fullPrompt, 1200)
	log.Printf("[trigger-task] task=%q cloudAccountId=%q 使用 product=%q 问题=%s（原始 product=%q 全局=%q workspace=%q project=%q）",
		req.Name, clientCfg.CloudAccountID, taskProduct, promptLog, req.Product, clientCfg.Product, req.Workspace, req.Project)

	type triggerResult struct {
		reply string
		err   error
	}
	done := make(chan triggerResult, 1)

	go func() {
		prompt := req.Prompt
		if req.ConciseReply {
			prompt += "\n\n简化最终输出 适合聊天工具上阅读"
		}
		reply, err := scheduler.QueryEmployeeWithVariables(clientCfg, req.EmployeeName, prompt, taskProduct, taskProject, taskWorkspace, taskRegion)
		done <- triggerResult{reply: reply, err: err}
	}()

	select {
	case res := <-done:
		if res.err != nil {
			log.Printf("[Scheduler] 触发测试失败 task=%q product=%q 问题=%s employee=%q: %v", req.Name, taskProduct, promptLog, req.EmployeeName, res.err)
			c.JSON(http.StatusOK, gin.H{"ok": false, "error": res.err.Error()})
			return
		}
		log.Printf("[Scheduler] 触发测试完成 task=%q product=%q 问题=%s employee=%q 响应(%d 字): %s",
			req.Name, taskProduct, promptLog, req.EmployeeName, len([]rune(res.reply)), res.reply)

		// 如果填了 Webhook，顺便推送
		webhooks := req.effectiveWebhooks()
		webhookSent := false
		var webhookErrors []string
		for _, wh := range webhooks {
			if wh.URL == "" {
				continue
			}
			raw, err := scheduler.SendToWebhook(config.WebhookConfig{
				Type:    wh.Type,
				URL:     wh.URL,
				MsgType: wh.MsgType,
				Title:   wh.Title,
				To:      wh.To,
			}, res.reply)
			if err != nil {
				webhookErrors = append(webhookErrors, fmt.Sprintf("[%s] %v", wh.Type, err))
				log.Printf("[Scheduler] 触发测试 webhook 发送失败 task=%q product=%q 问题=%s type=%s: %v（平台响应: %s）", req.Name, taskProduct, promptLog, wh.Type, err, raw)
			} else {
				webhookSent = true
				log.Printf("[Scheduler] 触发测试 webhook 发送成功 task=%q product=%q 问题=%s type=%s 平台响应: %s", req.Name, taskProduct, promptLog, wh.Type, raw)
			}
		}

		webhookErr := strings.Join(webhookErrors, "; ")
		c.JSON(http.StatusOK, gin.H{
			"ok":          true,
			"reply":       res.reply,
			"webhookSent": webhookSent,
			"webhookErr":  webhookErr,
		})

	case <-time.After(31 * time.Minute):
		log.Printf("[trigger-task] task=%q product=%q 问题=%s employee=%q 请求超时（HTTP 等待 31m，与 SSE 30m 对齐）", req.Name, taskProduct, promptLog, req.EmployeeName)
		// 与 queryEmployee 内 SSE 的 30m 超时对齐，避免后台仍在跑但 HTTP 已提前返回
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "请求超时（30 分钟），数字员工响应过慢"})
	}
}

// handleTestAK 用提交的 AK 凭据向 apsara-ops 发送一条测试消息，验证凭据有效性和权限
func (s *Server) handleTestAK(c *gin.Context) {
	var req struct {
		AccessKeyId     string `json:"accessKeyId"`
		AccessKeySecret string `json:"accessKeySecret"`
		Endpoint        string `json:"endpoint"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误: " + err.Error()})
		return
	}
	if req.AccessKeyId == "" || req.AccessKeySecret == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "AccessKeyId 和 AccessKeySecret 不能为空"})
		return
	}

	endpoint := req.Endpoint
	if endpoint == "" {
		endpoint = "cms.cn-hangzhou.aliyuncs.com"
	}

	sopClient, err := client.NewCMSClient(&client.Config{
		AccessKeyId:     req.AccessKeyId,
		AccessKeySecret: req.AccessKeySecret,
		Endpoint:        endpoint,
	})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "创建客户端失败: " + err.Error()})
		return
	}

	type testResult struct {
		text string
		err  error
	}
	done := make(chan testResult, 1)

	go func() {
		_, msgs, err := sopClient.SendMessageSync(&sopchat.ChatOptions{
			EmployeeName: "apsara-ops",
			ThreadId:     "",
			Message:      "现在几点了",
		})
		if err != nil {
			done <- testResult{err: err}
			return
		}
		// 从 Contents 中提取 type=text 的文本内容
		var sb strings.Builder
		for _, msg := range msgs {
			if msg == nil {
				continue
			}
			for _, content := range msg.Contents {
				if content == nil {
					continue
				}
				if t, ok := content["type"]; ok && t == "text" {
					if v, ok := content["value"]; ok {
						if s, ok := v.(string); ok {
							sb.WriteString(s)
						}
					}
				}
			}
		}
		done <- testResult{text: sb.String()}
	}()

	select {
	case res := <-done:
		if res.err != nil {
			log.Printf("[test-ak] 测试失败: %v", res.err)
			c.JSON(http.StatusOK, gin.H{"ok": false, "error": res.err.Error()})
			return
		}
		preview := res.text
		if len([]rune(preview)) > 120 {
			preview = string([]rune(preview)[:120]) + "..."
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "preview": preview})
	case <-time.After(60 * time.Second):
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "请求超时（60s），请检查网络或 Endpoint 是否正确"})
	}
}

// handleTestCMS 校验 CMS Region/Workspace 配置是否有效。
// 通过创建一个带 workspace 变量的 thread 来验证 workspace 是否存在于指定 region 下。
func (s *Server) handleTestCMS(c *gin.Context) {
	var req struct {
		AccessKeyId     string `json:"accessKeyId"`
		AccessKeySecret string `json:"accessKeySecret"`
		Endpoint        string `json:"endpoint"`
		EmployeeName    string `json:"employeeName"`
		Region          string `json:"region"`
		Workspace       string `json:"workspace"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误: " + err.Error()})
		return
	}
	if req.AccessKeyId == "" || req.AccessKeySecret == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "AccessKeyId 和 AccessKeySecret 不能为空"})
		return
	}
	if req.Region == "" || req.Workspace == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Region 和 Workspace 不能为空"})
		return
	}

	employeeName := req.EmployeeName
	if employeeName == "" {
		employeeName = "apsara-ops"
	}

	endpoint := req.Endpoint
	if endpoint == "" {
		endpoint = "cms.cn-hangzhou.aliyuncs.com"
	}

	// 校验 workspace 时使用 region 对应的 endpoint
	validateEndpoint := fmt.Sprintf("cms.%s.aliyuncs.com", req.Region)

	sopClient, err := client.NewCMSClient(&client.Config{
		AccessKeyId:     req.AccessKeyId,
		AccessKeySecret: req.AccessKeySecret,
		Endpoint:        validateEndpoint,
	})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "创建客户端失败: " + err.Error()})
		return
	}

	// 通过 GetWorkspace 直接验证 workspace 是否存在
	_, err = sopClient.CmsClient.GetWorkspace(&req.Workspace)
	if err != nil {
		log.Printf("[test-cms] 校验失败: %v", err)
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("Workspace %q 在 Region %s 下不存在或无权访问: %v", req.Workspace, req.Region, err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": fmt.Sprintf("校验通过：Workspace %q 在 Region %s 下存在", req.Workspace, req.Region),
	})
}
