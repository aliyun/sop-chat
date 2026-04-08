package api

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"reflect"
	"strings"
	"time"

	"sop-chat/internal/client"
	"sop-chat/internal/config"
	"sop-chat/internal/embed"
	"sop-chat/internal/scheduler"
	"sop-chat/pkg/sopchat"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	"github.com/go-ldap/ldap/v3"
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
<body><div class="box"><h1>⚙️ 配置 UI 尚未构建</h1>
<p>请先执行 <code>make build-frontend</code> 构建前端，<br>或将 <code>frontend/public/config.html</code> 复制到<br><code>backend/internal/embed/frontend/config.html</code> 后重新编译。</p></div></body></html>`))
		return
	}
	c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
	c.Data(http.StatusOK, "text/html; charset=utf-8", htmlBytes)
}

// configUIResponse 是返回给前端的结构化配置数据（不暴露文件路径）
type configUIResponse struct {
	Global         configUIGlobal          `json:"global"`
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
	Name         string            `json:"name"`
	Enabled      bool              `json:"enabled"`
	Cron         string            `json:"cron"`
	Prompt       string            `json:"prompt"`
	EmployeeName string            `json:"employeeName"`
	ConciseReply bool              `json:"conciseReply"`
	Product      string            `json:"product"`
	Project      string            `json:"project"`
	Workspace    string            `json:"workspace"`
	Region       string            `json:"region"`
	Webhook      *configUIWebhook  `json:"webhook,omitempty"` // 向后兼容：旧前端可能只传单个
	Webhooks     []configUIWebhook `json:"webhooks"`
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

type configUIGlobal struct {
	AccessKeyId         string `json:"accessKeyId"`
	AccessKeySecret     string `json:"accessKeySecret"`
	Endpoint            string `json:"endpoint"`
	Host                string `json:"host"`
	Port                int    `json:"port"`
	TimeZone            string `json:"timeZone"`
	Language            string `json:"language"`
	BindThreadToProcess *bool  `json:"bindThreadToProcess,omitempty"`
	Product             string `json:"product"`
	Project             string `json:"project"`
	Workspace           string `json:"workspace"`
	Region              string `json:"region"`
}

type configUIAuth struct {
	Methods      []string       `json:"methods"`                // 鉴权链，对应 auth.methods
	JWTSecretKey string         `json:"jwtSecretKey"`           // maps to auth.jwt.secretKey
	JWTExpiresIn string         `json:"jwtExpiresIn"`           // maps to auth.jwt.expiresIn
	PasswordSalt string         `json:"passwordSalt,omitempty"` // MD5(salt+password)
	Local        *configUILocal `json:"local,omitempty"`
	LDAP         *configUILDAP  `json:"ldap,omitempty"`
	OIDC         *configUIOIDC  `json:"oidc,omitempty"`
}

type configUILDAP struct {
	Host              string                         `json:"host"`
	Port              int                            `json:"port"`
	UseTLS            bool                           `json:"useTLS"`
	BindDN            string                         `json:"bindDN"`
	BindPassword      string                         `json:"bindPassword"`
	BaseDN            string                         `json:"baseDN"`
	UserFilter        string                         `json:"userFilter"`
	UsernameAttr      string                         `json:"usernameAttr"`
	DisplayAttr       string                         `json:"displayAttr"`
	EmailAttr         string                         `json:"emailAttr"`
	GroupRoleMappings []configUILDAPGroupRoleMapping `json:"groupRoleMappings"`
}

type configUILDAPGroupRoleMapping struct {
	GroupDN string `json:"groupDN"`
	Role    string `json:"role"`
}

type configUIOIDC struct {
	IssuerURL     string   `json:"issuerURL"`
	ClientID      string   `json:"clientId"`
	ClientSecret  string   `json:"clientSecret"`
	RedirectURL   string   `json:"redirectURL"`
	Scopes        []string `json:"scopes"`
	UsernameClaim string   `json:"usernameClaim"`
	DisplayName   string   `json:"displayName"`
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
	Name      string   `json:"name"`
	Users     []string `json:"users"`
	Employees []string `json:"employees"` // 该角色可见的数字员工列表
}

type configUIConversationRoute struct {
	ConversationTitle string `json:"conversationTitle"`
	EmployeeName      string `json:"employeeName"`
	Product           string `json:"product"`
	Project           string `json:"project"`
	Workspace         string `json:"workspace"`
	Region            string `json:"region"`
}

type configUIDingTalk struct {
	Enabled              bool                        `json:"enabled"`
	Name                 string                      `json:"name"`
	ClientId             string                      `json:"clientId"`
	ClientSecret         string                      `json:"clientSecret"`
	EmployeeName         string                      `json:"employeeName"`
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
}

type configUIFeishu struct {
	Enabled           bool     `json:"enabled"`
	Name              string   `json:"name"`
	AppID             string   `json:"appId"`
	AppSecret         string   `json:"appSecret"`
	VerificationToken string   `json:"verificationToken"`
	EventEncryptKey   string   `json:"eventEncryptKey"`
	EmployeeName      string   `json:"employeeName"`
	ConciseReply      bool     `json:"conciseReply"`
	Product           string   `json:"product"`
	Project           string   `json:"project"`
	Workspace         string   `json:"workspace"`
	Region            string   `json:"region"`
	AllowedUsers      []string `json:"allowedUsers"`
	AllowedChats      []string `json:"allowedChats"`
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
}

type configUIWeComBot struct {
	Enabled      bool   `json:"enabled"`
	Name         string `json:"name"`
	BotID        string `json:"botId"`
	BotSecret    string `json:"botSecret"`
	EmployeeName string `json:"employeeName"`
	ConciseReply bool   `json:"conciseReply"`
	Product      string `json:"product"`
	Project      string `json:"project"`
	Workspace    string `json:"workspace"`
	Region       string `json:"region"`
}

type configUIOpenAI struct {
	APIKeys []string `json:"apiKeys"`
}

// handleGetConfig 返回结构化的配置 JSON（不包含文件路径）
func (s *Server) handleGetConfig(c *gin.Context) {
	s.mu.RLock()
	cfg := s.globalConfig
	s.mu.RUnlock()

	if cfg == nil {
		// 配置文件不存在时返回空配置，让前端呈现空表单供用户填写
		c.JSON(http.StatusOK, configUIResponse{
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
		Global: configUIGlobal{
			AccessKeyId:     cfg.Global.AccessKeyId,
			AccessKeySecret: cfg.Global.AccessKeySecret,
			Endpoint:        cfg.Global.Endpoint,
			Host:            cfg.Global.Host,
			Port:            cfg.Global.Port,
			TimeZone:        cfg.Global.TimeZone,
			Language:        cfg.Global.Language,
			BindThreadToProcess: func() *bool {
				v := cfg.BindThreadToProcess()
				return &v
			}(),
			Product:   cfg.Global.Product,
			Project:   cfg.Global.Project,
			Workspace: cfg.Global.Workspace,
			Region:    cfg.Global.Region,
		},
		Auth: configUIAuth{
			Methods:      cfg.Auth.Methods,
			JWTSecretKey: cfg.Auth.JWT.SecretKey,
			JWTExpiresIn: cfg.Auth.JWT.ExpiresIn,
			PasswordSalt: cfg.Auth.PasswordSalt,
		},
		OpenAIEnabled: cfg.OpenAI != nil && cfg.OpenAI.Enabled,
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
			employees := r.Employees
			if employees == nil {
				employees = []string{}
			}
			local.Roles[i] = configUIRole{Name: r.Name, Users: users, Employees: employees}
		}
		resp.Auth.Local = local
	}

	// LDAP 配置
	if cfg.Auth.LDAP != nil {
		mappings := make([]configUILDAPGroupRoleMapping, 0, len(cfg.Auth.LDAP.GroupRoleMappings))
		for _, m := range cfg.Auth.LDAP.GroupRoleMappings {
			if strings.TrimSpace(m.GroupDN) == "" || strings.TrimSpace(m.Role) == "" {
				continue
			}
			mappings = append(mappings, configUILDAPGroupRoleMapping{
				GroupDN: m.GroupDN,
				Role:    m.Role,
			})
		}
		resp.Auth.LDAP = &configUILDAP{
			Host:              cfg.Auth.LDAP.Host,
			Port:              cfg.Auth.LDAP.Port,
			UseTLS:            cfg.Auth.LDAP.UseTLS,
			BindDN:            cfg.Auth.LDAP.BindDN,
			BindPassword:      cfg.Auth.LDAP.BindPassword,
			BaseDN:            cfg.Auth.LDAP.BaseDN,
			UserFilter:        cfg.Auth.LDAP.UserFilter,
			UsernameAttr:      cfg.Auth.LDAP.UsernameAttr,
			DisplayAttr:       cfg.Auth.LDAP.DisplayAttr,
			EmailAttr:         cfg.Auth.LDAP.EmailAttr,
			GroupRoleMappings: mappings,
		}
	}

	// OIDC 配置
	if cfg.Auth.OIDC != nil {
		scopes := cfg.Auth.OIDC.Scopes
		if scopes == nil {
			scopes = []string{}
		}
		displayName := strings.TrimSpace(cfg.Auth.OIDC.DisplayName)
		if displayName == "" {
			displayName = "OIDC 登录"
		}
		resp.Auth.OIDC = &configUIOIDC{
			IssuerURL:     cfg.Auth.OIDC.IssuerURL,
			ClientID:      cfg.Auth.OIDC.ClientID,
			ClientSecret:  cfg.Auth.OIDC.ClientSecret,
			RedirectURL:   cfg.Auth.OIDC.RedirectURL,
			Scopes:        scopes,
			UsernameClaim: cfg.Auth.OIDC.UsernameClaim,
			DisplayName:   displayName,
		}
	}

	// 始终返回所有钉钉实例（包括 enabled=false 的，凭据应保留显示）
	if cfg.Channels != nil && len(cfg.Channels.DingTalk) > 0 {
		resp.DingTalk = make([]configUIDingTalk, len(cfg.Channels.DingTalk))
		for i, dt := range cfg.Channels.DingTalk {
			routes := make([]configUIConversationRoute, len(dt.ConversationRoutes))
			for j, r := range dt.ConversationRoutes {
				routes[j] = configUIConversationRoute{
					ConversationTitle: r.ConversationTitle,
					EmployeeName:      r.EmployeeName,
					Product:           r.Product,
					Project:           r.Project,
					Workspace:         r.Workspace,
					Region:            r.Region,
				}
			}
			resp.DingTalk[i] = configUIDingTalk{
				Enabled:              dt.Enabled,
				Name:                 dt.Name,
				ClientId:             dt.ClientId,
				ClientSecret:         dt.ClientSecret,
				EmployeeName:         dt.EmployeeName,
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
			}
		}
	} else {
		resp.DingTalk = []configUIDingTalk{}
	}

	// 飞书配置
	if cfg.Channels != nil && len(cfg.Channels.Feishu) > 0 {
		resp.Feishu = make([]configUIFeishu, len(cfg.Channels.Feishu))
		for i, ft := range cfg.Channels.Feishu {
			resp.Feishu[i] = configUIFeishu{
				Enabled:           ft.Enabled,
				Name:              ft.Name,
				AppID:             ft.AppID,
				AppSecret:         ft.AppSecret,
				VerificationToken: ft.VerificationToken,
				EventEncryptKey:   ft.EventEncryptKey,
				EmployeeName:      ft.EmployeeName,
				ConciseReply:      ft.ConciseReply,
				Product:           ft.Product,
				Project:           ft.Project,
				Workspace:         ft.Workspace,
				Region:            ft.Region,
				AllowedUsers:      ft.AllowedUsers,
				AllowedChats:      ft.AllowedChats,
			}
		}
	} else {
		resp.Feishu = []configUIFeishu{}
	}

	// 企业微信配置
	if cfg.Channels != nil && len(cfg.Channels.WeCom) > 0 {
		resp.WeCom = make([]configUIWeCom, len(cfg.Channels.WeCom))
		for i, wc := range cfg.Channels.WeCom {
			resp.WeCom[i] = configUIWeCom{
				Enabled:        wc.Enabled,
				Name:           wc.Name,
				CorpID:         wc.CorpID,
				AgentID:        wc.AgentID,
				Secret:         wc.Secret,
				Token:          wc.Token,
				EncodingAESKey: wc.EncodingAESKey,
				CallbackPort:   wc.CallbackPort,
				CallbackPath:   wc.CallbackPath,
				EmployeeName:   wc.EmployeeName,
				ConciseReply:   wc.ConciseReply,
				Product:        wc.Product,
				Project:        wc.Project,
				Workspace:      wc.Workspace,
				Region:         wc.Region,
				AllowedUsers:   wc.AllowedUsers,
			}
		}
	} else {
		resp.WeCom = []configUIWeCom{}
	}

	// 企业微信群聊机器人配置（独立渠道）
	if cfg.Channels != nil && len(cfg.Channels.WeComBot) > 0 {
		resp.WeComBot = make([]configUIWeComBot, len(cfg.Channels.WeComBot))
		for i, wb := range cfg.Channels.WeComBot {
			resp.WeComBot[i] = configUIWeComBot{
				Enabled:      wb.Enabled,
				Name:         wb.Name,
				BotID:        wb.BotID,
				BotSecret:    wb.BotSecret,
				EmployeeName: wb.EmployeeName,
				ConciseReply: wb.ConciseReply,
				Product:      wb.Product,
				Project:      wb.Project,
				Workspace:    wb.Workspace,
				Region:       wb.Region,
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
			effectiveProduct := config.ResolveScheduledTaskProduct(t.Product, t.Project, t.Workspace, cfg.Global.Product)
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
				Name:         t.Name,
				Enabled:      t.Enabled,
				Cron:         t.Cron,
				Prompt:       t.Prompt,
				EmployeeName: t.EmployeeName,
				ConciseReply: t.ConciseReply,
				Product:      effectiveProduct,
				Project:      t.Project,
				Workspace:    t.Workspace,
				Region:       t.Region,
				Webhooks:     uiWebhooks,
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

	var req configUIResponse
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误: " + err.Error()})
		return
	}

	// 构建 config.Config 结构体
	s.mu.RLock()
	oldGlobalCfg := s.globalConfig
	s.mu.RUnlock()

	bindThread := true
	if req.Global.BindThreadToProcess != nil {
		bindThread = *req.Global.BindThreadToProcess
	}

	cfg := &config.Config{
		Global: config.GlobalConfig{
			AccessKeyId:         req.Global.AccessKeyId,
			AccessKeySecret:     req.Global.AccessKeySecret,
			Endpoint:            req.Global.Endpoint,
			Host:                req.Global.Host,
			Port:                req.Global.Port,
			TimeZone:            req.Global.TimeZone,
			Language:            req.Global.Language,
			BindThreadToProcess: &bindThread,
			Product:             req.Global.Product,
			Project:             req.Global.Project,
			Workspace:           req.Global.Workspace,
			Region:              req.Global.Region,
		},
		Auth: config.AuthConfig{
			Methods: req.Auth.Methods,
			JWT: config.JWTConfig{
				SecretKey: req.Auth.JWTSecretKey,
				ExpiresIn: req.Auth.JWTExpiresIn,
			},
			PasswordSalt: req.Auth.PasswordSalt,
		},
	}

	if req.Auth.Local != nil {
		cfg.Auth.BuiltinUsers = make([]config.UserConfig, len(req.Auth.Local.Users))
		cfg.Auth.Roles = make([]config.RoleConfig, len(req.Auth.Local.Roles))
		for i, u := range req.Auth.Local.Users {
			cfg.Auth.BuiltinUsers[i] = config.UserConfig{Name: u.Name, Password: u.Password}
		}
		for i, r := range req.Auth.Local.Roles {
			cfg.Auth.Roles[i] = config.RoleConfig{Name: r.Name, Users: r.Users, Employees: r.Employees}
		}
	}

	// LDAP 配置
	if req.Auth.LDAP != nil && (req.Auth.LDAP.Host != "" || req.Auth.LDAP.BaseDN != "" || req.Auth.LDAP.BindDN != "") {
		port := req.Auth.LDAP.Port
		if port == 0 {
			if req.Auth.LDAP.UseTLS {
				port = 636
			} else {
				port = 389
			}
		}
		mappings := make([]config.LDAPGroupRoleMapping, 0, len(req.Auth.LDAP.GroupRoleMappings))
		for _, m := range req.Auth.LDAP.GroupRoleMappings {
			groupDN := strings.TrimSpace(m.GroupDN)
			role := strings.TrimSpace(m.Role)
			if groupDN == "" || role == "" {
				continue
			}
			mappings = append(mappings, config.LDAPGroupRoleMapping{
				GroupDN: groupDN,
				Role:    role,
			})
		}
		cfg.Auth.LDAP = &config.LDAPConfig{
			Host:              req.Auth.LDAP.Host,
			Port:              port,
			UseTLS:            req.Auth.LDAP.UseTLS,
			BindDN:            req.Auth.LDAP.BindDN,
			BindPassword:      req.Auth.LDAP.BindPassword,
			BaseDN:            req.Auth.LDAP.BaseDN,
			UserFilter:        req.Auth.LDAP.UserFilter,
			UsernameAttr:      req.Auth.LDAP.UsernameAttr,
			DisplayAttr:       req.Auth.LDAP.DisplayAttr,
			EmailAttr:         req.Auth.LDAP.EmailAttr,
			GroupRoleMappings: mappings,
		}
	}

	// OIDC 配置
	if req.Auth.OIDC != nil && (req.Auth.OIDC.IssuerURL != "" || req.Auth.OIDC.ClientID != "") {
		displayName := strings.TrimSpace(req.Auth.OIDC.DisplayName)
		if displayName == "" {
			displayName = "OIDC 登录"
		}
		cfg.Auth.OIDC = &config.OIDCConfig{
			IssuerURL:     req.Auth.OIDC.IssuerURL,
			ClientID:      req.Auth.OIDC.ClientID,
			ClientSecret:  req.Auth.OIDC.ClientSecret,
			RedirectURL:   req.Auth.OIDC.RedirectURL,
			Scopes:        req.Auth.OIDC.Scopes,
			UsernameClaim: req.Auth.OIDC.UsernameClaim,
			DisplayName:   displayName,
		}
	}

	// 渠道配置：钉钉（多实例列表）
	if len(req.DingTalk) > 0 {
		if cfg.Channels == nil {
			cfg.Channels = &config.ChannelsConfig{}
		}
		cfg.Channels.DingTalk = make([]config.DingTalkConfig, 0, len(req.DingTalk))
		for _, dt := range req.DingTalk {
			// 只保留有实质内容的条目
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
				})
			}
		}
	}

	// 飞书配置
	if len(req.Feishu) > 0 {
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
					Enabled:           ft.Enabled,
					Name:              ft.Name,
					AppID:             ft.AppID,
					AppSecret:         ft.AppSecret,
					VerificationToken: ft.VerificationToken,
					EventEncryptKey:   ft.EventEncryptKey,
					EmployeeName:      ft.EmployeeName,
					ConciseReply:      ft.ConciseReply,
					Product:           ft.Product,
					Project:           ft.Project,
					Workspace:         ft.Workspace,
					Region:            ft.Region,
					AllowedUsers:      allowedUsers,
					AllowedChats:      allowedChats,
				})
			}
		}
	}

	// 企业微信配置
	if len(req.WeCom) > 0 {
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
					Enabled:        wc.Enabled,
					Name:           wc.Name,
					CorpID:         wc.CorpID,
					AgentID:        wc.AgentID,
					Secret:         wc.Secret,
					Token:          wc.Token,
					EncodingAESKey: wc.EncodingAESKey,
					CallbackPort:   wc.CallbackPort,
					CallbackPath:   wc.CallbackPath,
					EmployeeName:   wc.EmployeeName,
					ConciseReply:   wc.ConciseReply,
					Product:        wc.Product,
					Project:        wc.Project,
					Workspace:      wc.Workspace,
					Region:         wc.Region,
					AllowedUsers:   allowedUsers,
				})
			}
		}
	}

	// 企业微信群聊机器人配置（独立渠道）
	if len(req.WeComBot) > 0 {
		if cfg.Channels == nil {
			cfg.Channels = &config.ChannelsConfig{}
		}
		cfg.Channels.WeComBot = make([]config.WeComBotConfig, 0, len(req.WeComBot))
		for _, wb := range req.WeComBot {
			if wb.BotID != "" || wb.BotSecret != "" || wb.EmployeeName != "" {
				cfg.Channels.WeComBot = append(cfg.Channels.WeComBot, config.WeComBotConfig{
					Enabled:      wb.Enabled,
					Name:         wb.Name,
					BotID:        wb.BotID,
					BotSecret:    wb.BotSecret,
					EmployeeName: wb.EmployeeName,
					ConciseReply: wb.ConciseReply,
					Product:      wb.Product,
					Project:      wb.Project,
					Workspace:    wb.Workspace,
					Region:       wb.Region,
				})
			}
		}
	}

	// OpenAI：只要填了任意密钥就保留配置块
	if len(req.OpenAI.APIKeys) > 0 {
		cfg.OpenAI = &config.OpenAICompatConfig{
			Enabled: req.OpenAIEnabled,
			APIKeys: req.OpenAI.APIKeys,
		}
	}
	if cfg.OpenAI == nil && req.OpenAIEnabled {
		cfg.OpenAI = &config.OpenAICompatConfig{Enabled: true}
	}

	// 定时任务配置
	for _, t := range req.ScheduledTasks {
		webhooks := t.effectiveWebhooks()
		hasContent := t.Name != "" || t.EmployeeName != "" || len(webhooks) > 0
		if hasContent {
			taskProduct := config.ResolveScheduledTaskProduct(t.Product, t.Project, t.Workspace, strings.TrimSpace(req.Global.Product))
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
				Name:         t.Name,
				Enabled:      t.Enabled,
				Cron:         t.Cron,
				Prompt:       t.Prompt,
				EmployeeName: t.EmployeeName,
				ConciseReply: t.ConciseReply,
				Product:      taskProduct,
				Project:      t.Project,
				Workspace:    t.Workspace,
				Region:       t.Region,
				Webhooks:     cfgWebhooks,
			})
		}
	}

	// 保存到文件
	if err := config.SaveConfig(s.configPath, cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("配置已保存: %s，开始热重载...", s.configPath)

	oidcChanged := false
	ldapChanged := false
	if oldGlobalCfg != nil {
		oidcChanged = !reflect.DeepEqual(oldGlobalCfg.Auth.OIDC, cfg.Auth.OIDC)
		ldapChanged = !reflect.DeepEqual(oldGlobalCfg.Auth.LDAP, cfg.Auth.LDAP)
	} else {
		oidcChanged = cfg.Auth.OIDC != nil
		ldapChanged = cfg.Auth.LDAP != nil
	}

	// 热重载内存中的配置
	if err := s.reloadConfig(); err != nil {
		log.Printf("热重载失败: %v", err)
		c.JSON(http.StatusOK, gin.H{
			"message": "配置已保存，但热重载失败（" + err.Error() + "），请手动重启服务器",
			"warning": true,
		})
		return
	}

	if oidcChanged {
		c.JSON(http.StatusOK, gin.H{
			"message": "配置已保存并应用。检测到 OIDC 配置变更，需重启服务进程后生效",
			"warning": true,
		})
		return
	}
	if ldapChanged {
		c.JSON(http.StatusOK, gin.H{
			"message": "配置已保存并应用。检测到 LDAP 配置变更，需重启服务进程后生效",
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

	if cfg == nil || cfg.AccessKeyId == "" {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "CMS 凭据未配置，请先在基础设置中填写 AccessKeyId / AccessKeySecret"})
		return
	}

	clientCfg := &config.ClientConfig{
		AccessKeyId:     cfg.AccessKeyId,
		AccessKeySecret: cfg.AccessKeySecret,
		Endpoint:        cfg.Endpoint,
	}
	if globalCfg != nil {
		clientCfg.Product = globalCfg.Global.Product
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
	log.Printf("[trigger-task] task=%q 使用 product=%q 问题=%s（原始 product=%q 全局=%q workspace=%q project=%q）",
		req.Name, taskProduct, promptLog, req.Product, clientCfg.Product, req.Workspace, req.Project)

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

// handleTestOIDC 校验 OIDC 配置：执行 OIDC Discovery 验证 issuerURL 可达且返回合法的 endpoints
func (s *Server) handleTestOIDC(c *gin.Context) {
	var req struct {
		IssuerURL    string `json:"issuerURL"`
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
		RedirectURL  string `json:"redirectURL"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误: " + err.Error()})
		return
	}
	if req.IssuerURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Issuer URL 不能为空"})
		return
	}
	if req.ClientID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Client ID 不能为空"})
		return
	}

	// 使用带超时的 context 执行 OIDC Discovery
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()

	provider, err := oidc.NewProvider(ctx, req.IssuerURL)
	if err != nil {
		log.Printf("[test-oidc] Discovery 失败: %v", err)
		c.JSON(http.StatusOK, gin.H{
			"ok":    false,
			"error": fmt.Sprintf("OIDC Discovery 失败: %v（请检查 Issuer URL 是否正确且可访问）", err),
		})
		return
	}

	// 提取 Discovery 返回的 endpoints 信息
	endpoint := provider.Endpoint()
	details := fmt.Sprintf("Authorization Endpoint: %s\nToken Endpoint: %s", endpoint.AuthURL, endpoint.TokenURL)

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": fmt.Sprintf("OIDC Discovery 成功（Issuer: %s）", req.IssuerURL),
		"details": details,
	})
}

// handleTestLDAP 测试 LDAP 连接：无 BindDN 时仅探测端口联通性，有 BindDN 时执行 Bind 验证
func (s *Server) handleTestLDAP(c *gin.Context) {
	var req struct {
		Host         string `json:"host"`
		Port         int    `json:"port"`
		UseTLS       bool   `json:"useTLS"`
		BindDN       string `json:"bindDN"`
		BindPassword string `json:"bindPassword"`
		BaseDN       string `json:"baseDN"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误: " + err.Error()})
		return
	}
	if req.Host == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "LDAP Host 不能为空"})
		return
	}

	port := req.Port
	if port == 0 {
		if req.UseTLS {
			port = 636
		} else {
			port = 389
		}
	}

	scheme := "ldap"
	if req.UseTLS {
		scheme = "ldaps"
	}
	addr := fmt.Sprintf("%s://%s:%d", scheme, req.Host, port)

	conn, err := ldap.DialURL(
		addr,
		ldap.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}),
	)
	if err != nil {
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("连接超时: %s", addr)})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("连接失败: %v", err)})
		return
	}
	defer conn.Close()

	// 无 BindDN：仅端口联通性测试
	if req.BindDN == "" {
		c.JSON(http.StatusOK, gin.H{
			"ok":      true,
			"message": fmt.Sprintf("端口联通性测试通过（%s）", addr),
		})
		return
	}

	// 有 BindDN：执行 Bind 验证
	if err := conn.Bind(req.BindDN, req.BindPassword); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"ok":    false,
			"error": fmt.Sprintf("Bind 失败: %v（请检查 Bind DN 和密码）", err),
		})
		return
	}

	// 如果填写了 BaseDN，进一步验证该 BaseDN 在目录中存在且可访问，
	// 避免“Bind 成功但登录搜索失败”的假阳性。
	baseDN := strings.TrimSpace(req.BaseDN)
	if baseDN != "" {
		searchReq := ldap.NewSearchRequest(
			baseDN,
			ldap.ScopeBaseObject,
			ldap.NeverDerefAliases,
			1,
			5,
			false,
			"(objectClass=*)",
			[]string{"dn"},
			nil,
		)
		if _, err := conn.Search(searchReq); err != nil {
			if ldapErr, ok := err.(*ldap.Error); ok && ldapErr.ResultCode == ldap.LDAPResultNoSuchObject {
				c.JSON(http.StatusOK, gin.H{
					"ok":    false,
					"error": fmt.Sprintf("Bind 成功，但 Base DN 不存在或不可访问: %q（登录搜索会失败）", baseDN),
				})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"ok":    false,
				"error": fmt.Sprintf("Bind 成功，但 Base DN 校验失败: %v", err),
			})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": fmt.Sprintf("LDAP Bind 成功（%s，DN: %s）", addr, req.BindDN),
	})
}
