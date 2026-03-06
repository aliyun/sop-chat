package api

import (
	"log"
	"net/http"

	"sop-chat/internal/config"
	"sop-chat/internal/embed"

	"github.com/gin-gonic/gin"
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
	Global        configUIGlobal     `json:"global"`
	Auth          configUIAuth       `json:"auth"`
	DingTalk      []configUIDingTalk `json:"dingtalk"`
	OpenAIEnabled bool               `json:"openaiEnabled"`
	OpenAI        configUIOpenAI     `json:"openai"`
}

type configUIGlobal struct {
	AccessKeyId     string `json:"accessKeyId"`
	AccessKeySecret string `json:"accessKeySecret"`
	Endpoint        string `json:"endpoint"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	TimeZone        string `json:"timeZone"`
	Language        string `json:"language"`
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
}

type configUIDingTalk struct {
	Enabled              bool                         `json:"enabled"`
	Name                 string                       `json:"name"`
	ClientId             string                       `json:"clientId"`
	ClientSecret         string                       `json:"clientSecret"`
	EmployeeName         string                       `json:"employeeName"`
	ConciseReply         bool                         `json:"conciseReply"`
	AllowedGroupUsers    []string                     `json:"allowedGroupUsers"`
	AllowedDirectUsers   []string                     `json:"allowedDirectUsers"`
	AllowedConversations []string                     `json:"allowedConversations"`
	ConversationRoutes   []configUIConversationRoute  `json:"conversationRoutes"`
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
			DingTalk: []configUIDingTalk{},
			OpenAI:   configUIOpenAI{APIKeys: []string{}},
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
			local.Roles[i] = configUIRole{Name: r.Name, Users: users}
		}
		resp.Auth.Local = local
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
				}
			}
			resp.DingTalk[i] = configUIDingTalk{
				Enabled:              dt.Enabled,
				Name:                 dt.Name,
				ClientId:             dt.ClientId,
				ClientSecret:         dt.ClientSecret,
				EmployeeName:         dt.EmployeeName,
				ConciseReply:         dt.ConciseReply,
				AllowedGroupUsers:    dt.AllowedGroupUsers,
				AllowedDirectUsers:   dt.AllowedDirectUsers,
				AllowedConversations: dt.AllowedConversations,
				ConversationRoutes:   routes,
			}
		}
	} else {
		resp.DingTalk = []configUIDingTalk{}
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
	cfg := &config.Config{
		Global: config.GlobalConfig{
			AccessKeyId:     req.Global.AccessKeyId,
			AccessKeySecret: req.Global.AccessKeySecret,
			Endpoint:        req.Global.Endpoint,
			Host:            req.Global.Host,
			Port:            req.Global.Port,
			TimeZone:        req.Global.TimeZone,
			Language:        req.Global.Language,
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
			cfg.Auth.Roles[i] = config.RoleConfig{Name: r.Name, Users: r.Users}
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
					AllowedGroupUsers:    dt.AllowedGroupUsers,
					AllowedDirectUsers:   dt.AllowedDirectUsers,
					AllowedConversations: dt.AllowedConversations,
					ConversationRoutes:   routes,
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
