package api

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"sync"

	"sop-chat/internal/auth"
	"sop-chat/internal/client"
	"sop-chat/internal/config"
	"sop-chat/internal/dingtalk"
	"sop-chat/internal/embed"
	"sop-chat/internal/feishu"
	"sop-chat/internal/wecom"
	"sop-chat/pkg/sopchat"

	cmsclient "github.com/alibabacloud-go/cms-20240330/v6/client"
	openapiutil "github.com/alibabacloud-go/darabonba-openapi/v2/utils"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

type Server struct {
	mu             sync.RWMutex
	config         *client.Config
	globalConfig   *config.Config
	configPath     string // 配置文件实际路径，用于配置 UI 读写
	configUIToken  string // 配置 UI 访问令牌（启动时随机生成）
	router         *gin.Engine
	authProvider   auth.Provider
	authModes      []auth.AuthMode
	jwtManager     *auth.JWTManager
	userStore      auth.UserStore
	authMiddleware *auth.AuthMiddleware

	// 钉钉机器人生命周期管理（支持多实例热启停，keyed by clientId）
	dingtalkMu   sync.Mutex
	dingtalkBots map[string]*dingtalk.Bot

	// 飞书机器人生命周期管理（keyed by appId）
	feishuMu   sync.Mutex
	feishuBots map[string]*feishu.Bot

	// 企业微信机器人生命周期管理（keyed by corpId:agentId）
	wecomMu   sync.Mutex
	wecomBots map[string]*wecom.Bot

	// 企业微信长连接群机器人生命周期管理（keyed by botId）
	wecomLongConnMu   sync.Mutex
	wecomLongConnBots map[string]*wecom.LongConnBot
}

// generateConfigUIToken 生成随机的配置 UI 访问令牌
func generateConfigUIToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func NewServer(cfg *client.Config, globalConfig *config.Config, configPath string) (*Server, error) {
	// 创建 Gin 引擎
	router := gin.Default()

	// 配置 CORS
	corsConfig := cors.DefaultConfig()
	corsConfig.AllowAllOrigins = true
	corsConfig.AllowHeaders = []string{"Origin", "Content-Type", "Accept", "Authorization"}
	router.Use(cors.New(corsConfig))

	token, err := generateConfigUIToken()
	if err != nil {
		return nil, fmt.Errorf("生成配置 UI 令牌失败: %w", err)
	}

	server := &Server{
		config:        cfg,
		globalConfig:  globalConfig,
		configPath:    configPath,
		configUIToken: token,
		router:        router,
	}

	// 初始化认证系统
	if err := server.initAuth(); err != nil {
		return nil, err
	}

	// 注册路由
	server.setupRoutes()

	if globalConfig != nil && globalConfig.Channels != nil {
		cmsClientCfg, cmsErr := globalConfig.ToClientConfig()
		if cmsErr != nil {
			log.Printf("警告: 无法获取 CMS 配置，消息渠道机器人未启动: %v", cmsErr)
		} else {
			// 启动钉钉机器人
			if len(globalConfig.Channels.DingTalk) > 0 {
				server.syncDingTalkBots(globalConfig.Channels.DingTalk, cmsClientCfg)
			}
			// 启动飞书机器人
			if len(globalConfig.Channels.Feishu) > 0 {
				server.syncFeishuBots(globalConfig.Channels.Feishu, cmsClientCfg)
			}
			// 启动企业微信应用（HTTP 回调）
			if len(globalConfig.Channels.WeCom) > 0 {
				server.syncWeComBots(globalConfig.Channels.WeCom, cmsClientCfg)
			}
			// 启动企业微信群聊机器人（长连接）：优先从独立 wecomBot 配置读取，
			// 同时兼容旧的 wecom[].botLongConn 嵌套配置
			wecomBotConfigs := server.mergeWeComBotConfigs(globalConfig.Channels)
			if len(wecomBotConfigs) > 0 {
				server.syncWeComLongConnBots(wecomBotConfigs, cmsClientCfg)
			}
		}
	}

	return server, nil
}

// initAuth 初始化认证系统
func (s *Server) initAuth() error {
	authConfig, err := auth.LoadAuthConfig()
	if err != nil {
		return fmt.Errorf("failed to load auth config: %w", err)
	}

	s.authModes = authConfig.Modes

	// 始终创建 JWT 管理器（即使 methods 为空，token 验证中间件仍然生效）
	s.jwtManager = auth.NewJWTManager(authConfig.JWTSecretKey, authConfig.JWTExpiresIn)

	if len(s.authModes) == 0 {
		// 登录功能关闭：不注册任何 Provider，JWT 中间件仍要求 token，
		// 但没有人能通过 /login 拿到 token，所以所有受保护接口均不可访问。
		log.Println("⚠️  auth.methods 为空，登录功能已关闭，所有受保护接口不可访问")
		s.authMiddleware = auth.NewAuthMiddleware(nil)
		return nil
	}

	// 为鉴权链中每个模式创建对应的 Provider
	providers := make([]auth.Provider, 0, len(s.authModes))
	for _, mode := range s.authModes {
		switch mode {
		case auth.AuthModeBuiltin:
			var userStore auth.UserStore
			if authConfig.YAMLConfig != nil && authConfig.YAMLConfig.Local != nil {
				log.Printf("🔍 加载内置用户（统一配置）")
				yamlStore, err := auth.NewYAMLUserStoreFromConfig(authConfig.YAMLConfig)
				if err != nil {
					return fmt.Errorf("加载内置用户失败: %w，请确保 config.yaml 中已配置 auth.builtinUsers 和 auth.roles", err)
				}
				userStore = yamlStore
				s.userStore = userStore
				log.Printf("✅ builtin 认证就绪（统一配置）")
			} else if authConfig.YAMLConfigPath != "" {
				log.Printf("🔍 加载内置用户（YAML 文件: %s）", authConfig.YAMLConfigPath)
				yamlStore, err := auth.NewYAMLUserStore(authConfig.YAMLConfigPath)
				if err != nil {
					return fmt.Errorf("加载 YAML 配置失败 %s: %w", authConfig.YAMLConfigPath, err)
				}
				userStore = yamlStore
				s.userStore = userStore
				log.Printf("✅ builtin 认证就绪（YAML 文件）")
			} else {
				return fmt.Errorf("未找到内置用户配置，请在 config.yaml 中配置 auth.builtinUsers")
			}
			providers = append(providers, auth.NewLocalAuthProvider(userStore, s.jwtManager))

		case auth.AuthModeLDAP:
			// TODO: 实现 LDAP 认证提供者
			return auth.ErrUnsupportedAuthMode

		case auth.AuthModeOIDC:
			// TODO: 实现 OIDC 认证提供者
			return auth.ErrUnsupportedAuthMode

		default:
			return auth.ErrUnsupportedAuthMode
		}
	}

	s.authProvider = auth.NewChainProvider(providers, s.jwtManager)
	s.authMiddleware = auth.NewAuthMiddleware(s.authProvider)

	return nil
}

func (s *Server) setupRoutes() {
	// API 路由组
	api := s.router.Group("/api")
	{
		// 健康检查（无需认证）
		api.GET("/health", s.handleHealth)

		// 系统配置接口（无需认证）
		api.GET("/system/config", s.handleGetSystemConfig)

		// 认证相关接口（无需认证）
		api.POST("/auth/login", s.handleLogin)
		api.POST("/auth/logout", s.handleLogout)

		// 分享相关接口（无需认证，公开访问）
		api.GET("/share/:employeeName/:threadId", s.handleGetSharedThread)
		api.GET("/share/:employeeName/:threadId/messages", s.handleGetSharedThreadMessages)
		api.GET("/share/employee/:employeeName", s.handleGetSharedEmployee)

		// 需要认证的路由组
		protected := api.Group("")
		if s.authMiddleware != nil {
			protected.Use(s.authMiddleware.RequireAuth())
		}
		{
			// 获取当前用户信息
			protected.GET("/auth/me", s.handleGetCurrentUser)

			// 系统信息接口
			protected.GET("/system/account-id", s.handleGetAccountId)

			// 员工相关接口（查询接口所有已认证用户都可以访问）
			protected.GET("/employees", s.handleListEmployees)
			protected.GET("/employees/:name", s.handleGetEmployee)

			// 线程相关接口
			protected.POST("/threads", s.handleCreateThread)
			protected.GET("/threads/:employeeName", s.handleListThreads)
			protected.GET("/threads/:employeeName/:threadId", s.handleGetThread)
			protected.GET("/threads/:employeeName/:threadId/messages", s.handleGetThreadMessages)

			// 聊天接口 (SSE 流式)
			protected.POST("/chat/stream", s.handleChatStream)
		}

		// 需要 admin 角色的路由组
		if s.authMiddleware != nil {
			adminOnly := api.Group("")
			adminOnly.Use(s.authMiddleware.RequireAuth())
			adminOnly.Use(s.authMiddleware.RequireRole("admin"))
			{
				// 员工管理接口（创建、更新）- 仅 admin 可访问
				adminOnly.POST("/employees", s.handleCreateEmployee)
				adminOnly.PUT("/employees/:name", s.handleUpdateEmployee)
			}
		} else {
			// 如果没有认证中间件（disabled 模式），所有用户都可以访问
			protected.POST("/employees", s.handleCreateEmployee)
			protected.PUT("/employees/:name", s.handleUpdateEmployee)
		}
	}

	// OpenAI 兼容接口（/openai/v1/...），使用独立的 API key 认证
	openaiV1 := s.router.Group("/openai/v1")
	openaiV1.Use(s.openAIAuthMiddleware())
	{
		openaiV1.GET("/models", s.handleOpenAIListModels)
		openaiV1.POST("/chat/completions", s.handleOpenAIChatCompletions)
	}

	// 企业微信回调（统一入口，内部按路径分发）
	s.router.Any("/wecom/*path", s.handleWeComCallback)

	// 配置管理 UI（使用独立的 token 认证，与业务认证系统隔离）
	s.router.GET("/config-ui", s.handleConfigUIPage)
	s.router.GET("/config-ui/api/config", s.configUITokenMiddleware(), s.handleGetConfig)
	s.router.POST("/config-ui/api/config", s.configUITokenMiddleware(), s.handleSaveConfig)
	s.router.POST("/config-ui/api/test-ak", s.configUITokenMiddleware(), s.handleTestAK)

	// 静态文件服务（前端资源）
	frontendFS := embed.GetFrontendFS()
	if frontendFS != nil {
		// 创建 HTTP 文件系统
		httpFS := http.FS(frontendFS)

		// 提供静态文件服务（CSS、JS、图片等）
		// assets 目录下的文件
		assetsFS, err := fs.Sub(frontendFS, "assets")
		if err == nil {
			s.router.StaticFS("/assets", http.FS(assetsFS))
		}

		// favicon.svg 文件
		s.router.StaticFileFS("/favicon.svg", "favicon.svg", httpFS)

		// SPA 路由支持：所有非 API 路由都返回 index.html
		s.router.NoRoute(func(c *gin.Context) {
			path := c.Request.URL.Path

			// 如果请求的是 API、OpenAI 或配置 UI 路由，返回 404
			if (len(path) >= 4 && path[:4] == "/api") ||
				(len(path) >= 7 && path[:7] == "/openai") ||
				(len(path) >= 10 && path[:10] == "/config-ui") {
				c.JSON(404, gin.H{"error": "Not found"})
				return
			}

			// 如果请求的是静态资源（assets 或 favicon.svg），尝试直接提供
			if (len(path) >= 7 && path[:7] == "/assets") || path == "/favicon.svg" {
				// 使用 http.FileServer 来提供文件
				httpFile := http.FileServer(httpFS)
				httpFile.ServeHTTP(c.Writer, c.Request)
				return
			}

			// 否则返回 index.html（让前端路由处理）
			indexFile, err := frontendFS.Open("index.html")
			if err != nil {
				c.String(500, "Failed to load index.html")
				return
			}
			defer indexFile.Close()

			// 读取文件内容并写入响应
			data := make([]byte, 0)
			buf := make([]byte, 4096)
			for {
				n, err := indexFile.Read(buf)
				if n > 0 {
					data = append(data, buf[:n]...)
				}
				if err != nil {
					break
				}
			}

			// 设置禁止缓存的 header
			c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
			c.Header("Pragma", "no-cache")
			c.Header("Expires", "0")

			c.Data(http.StatusOK, "text/html; charset=utf-8", data)
		})
	}
}

func (s *Server) Run(addr string) error {
	log.Printf("Server starting on %s", addr)
	return s.router.Run(addr)
}

// GetConfigUIToken 返回配置 UI 访问令牌
func (s *Server) GetConfigUIToken() string {
	return s.configUIToken
}

// syncDingTalkBots 将运行中的机器人与新配置列表对齐：
// 新增或凭据变更的实例会被（重）启动，旧配置中已移除的实例会被停止。
func (s *Server) syncDingTalkBots(newConfigs []config.DingTalkConfig, cmsConfig *config.ClientConfig) {
	s.dingtalkMu.Lock()
	defer s.dingtalkMu.Unlock()

	if s.dingtalkBots == nil {
		s.dingtalkBots = make(map[string]*dingtalk.Bot)
	}

	// 收集新配置中所有启用的 clientId（作为目标集合）
	newEnabled := make(map[string]config.DingTalkConfig)
	for _, dt := range newConfigs {
		if dt.Enabled && dt.ClientId != "" && dt.ClientSecret != "" && dt.EmployeeName != "" {
			newEnabled[dt.ClientId] = dt
		}
	}

	// 停止已不在新配置中（或已禁用）的实例
	for clientId, bot := range s.dingtalkBots {
		if _, ok := newEnabled[clientId]; !ok {
			log.Printf("停止钉钉机器人: clientId=%s", clientId)
			bot.Stop()
			delete(s.dingtalkBots, clientId)
		}
	}

	// 启动或重启有变化的实例
	for clientId, dtCfg := range newEnabled {
		dtCopy := dtCfg // 避免循环变量逃逸
		if existing, ok := s.dingtalkBots[clientId]; ok {
			// 已存在：检查凭据或员工名是否变化
			if !dtCopy.CredsEqual(existing.Config()) {
				log.Printf("重启钉钉机器人（配置变更）: clientId=%s, employee=%s", clientId, dtCopy.EmployeeName)
				existing.Stop()
				delete(s.dingtalkBots, clientId)
			} else {
				// 凭据未变，但其他字段（如 allowedUsers、conciseReply）可能已更新，热更新配置
				existing.UpdateConfig(&dtCopy)
				continue
			}
		}
		log.Printf("启动钉钉机器人: clientId=%s, employee=%s", clientId, dtCopy.EmployeeName)
		bot := dingtalk.NewBot(&dtCopy, cmsConfig)
		if err := bot.Start(); err != nil {
			log.Printf("警告: 钉钉机器人启动失败 (clientId=%s): %v", clientId, err)
			continue
		}
		s.dingtalkBots[clientId] = bot
	}
}

// stopAllDingTalkBots 停止所有运行中的钉钉机器人
func (s *Server) stopAllDingTalkBots() {
	s.dingtalkMu.Lock()
	defer s.dingtalkMu.Unlock()
	for clientId, bot := range s.dingtalkBots {
		bot.Stop()
		delete(s.dingtalkBots, clientId)
	}
}

// syncFeishuBots 将运行中的飞书机器人与新配置列表对齐
func (s *Server) syncFeishuBots(newConfigs []config.FeishuConfig, cmsConfig *config.ClientConfig) {
	s.feishuMu.Lock()
	defer s.feishuMu.Unlock()

	if s.feishuBots == nil {
		s.feishuBots = make(map[string]*feishu.Bot)
	}

	newEnabled := make(map[string]config.FeishuConfig)
	for _, ft := range newConfigs {
		if ft.Enabled && ft.AppID != "" && ft.AppSecret != "" && ft.EmployeeName != "" {
			newEnabled[ft.AppID] = ft
		}
	}

	for appID, bot := range s.feishuBots {
		if _, ok := newEnabled[appID]; !ok {
			log.Printf("停止飞书机器人: appId=%s", appID)
			bot.Stop()
			delete(s.feishuBots, appID)
		}
	}

	for appID, ftCfg := range newEnabled {
		ftCopy := ftCfg
		if existing, ok := s.feishuBots[appID]; ok {
			existingCfg := existing.Config()
			if existingCfg.AppSecret != ftCopy.AppSecret || existingCfg.EmployeeName != ftCopy.EmployeeName {
				log.Printf("重启飞书机器人（配置变更）: appId=%s", appID)
				existing.Stop()
				delete(s.feishuBots, appID)
			} else {
				existing.UpdateConfig(&ftCopy)
				// 注册/更新 WeCom 回调路由
				s.registerFeishuRoutes(appID, existing)
				continue
			}
		}
		log.Printf("启动飞书机器人: appId=%s, employee=%s", appID, ftCopy.EmployeeName)
		bot := feishu.NewBot(&ftCopy, cmsConfig)
		if err := bot.Start(); err != nil {
			log.Printf("警告: 飞书机器人启动失败 (appId=%s): %v", appID, err)
			continue
		}
		s.feishuBots[appID] = bot
		s.registerFeishuRoutes(appID, bot)
	}
}

// registerFeishuRoutes 飞书使用 WebSocket，无需注册 HTTP 路由（占位，供将来扩展）
func (s *Server) registerFeishuRoutes(_ string, _ *feishu.Bot) {}

// stopAllFeishuBots 停止所有运行中的飞书机器人
func (s *Server) stopAllFeishuBots() {
	s.feishuMu.Lock()
	defer s.feishuMu.Unlock()
	for appID, bot := range s.feishuBots {
		bot.Stop()
		delete(s.feishuBots, appID)
	}
}

// syncWeComBots 将运行中的企业微信机器人与新配置列表对齐
func (s *Server) syncWeComBots(newConfigs []config.WeComConfig, cmsConfig *config.ClientConfig) {
	s.wecomMu.Lock()
	defer s.wecomMu.Unlock()

	if s.wecomBots == nil {
		s.wecomBots = make(map[string]*wecom.Bot)
	}

	newEnabled := make(map[string]config.WeComConfig)
	for _, wc := range newConfigs {
		if wc.Enabled && wc.CorpID != "" && wc.Secret != "" && wc.Token != "" &&
			wc.EncodingAESKey != "" && wc.AgentID != 0 && wc.EmployeeName != "" {
			key := fmt.Sprintf("%s:%d", wc.CorpID, wc.AgentID)
			newEnabled[key] = wc
		}
	}

	for key, bot := range s.wecomBots {
		if _, ok := newEnabled[key]; !ok {
			log.Printf("停止企业微信机器人: %s", key)
			delete(s.wecomBots, key)
		}
		_ = bot
	}

	for key, wcCfg := range newEnabled {
		wcCopy := wcCfg
		if existing, ok := s.wecomBots[key]; ok {
			existingCfg := existing.Config()
			if !existingCfg.CredsEqual(&wcCopy) {
				log.Printf("重建企业微信机器人（配置变更）: %s", key)
				delete(s.wecomBots, key)
			} else {
				existing.UpdateConfig(&wcCopy)
				s.registerWeComRoutes(key, existing)
				continue
			}
		}
		log.Printf("启动企业微信机器人: corpId=%s agentId=%d employee=%s", wcCopy.CorpID, wcCopy.AgentID, wcCopy.EmployeeName)
		bot, err := wecom.NewBot(&wcCopy, cmsConfig)
		if err != nil {
			log.Printf("警告: 企业微信机器人初始化失败 (%s): %v", key, err)
			continue
		}
		s.wecomBots[key] = bot
		s.registerWeComRoutes(key, bot)
	}
}

// registerWeComRoutes 记录企业微信回调路由（路由通过 handleWeComCallback 分发）
func (s *Server) registerWeComRoutes(key string, bot *wecom.Bot) {
	cfg := bot.Config()
	path := cfg.CallbackPath
	if path == "" {
		path = "/wecom/callback"
	}
	log.Printf("[WeCom] 回调路由已就绪: %s (key=%s)", path, key)
}

// handleWeComCallback 统一处理企业微信回调，根据路径分发到对应机器人
func (s *Server) handleWeComCallback(c *gin.Context) {
	reqPath := c.Request.URL.Path
	s.wecomMu.Lock()
	var matched *wecom.Bot
	for _, bot := range s.wecomBots {
		cfg := bot.Config()
		p := cfg.CallbackPath
		if p == "" {
			p = "/wecom/callback"
		}
		if p == reqPath {
			matched = bot
			break
		}
	}
	s.wecomMu.Unlock()

	if matched == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no wecom bot configured for this path"})
		return
	}
	matched.HandleCallback(c.Writer, c.Request)
}

// stopAllWeComBots 停止所有运行中的企业微信机器人
func (s *Server) stopAllWeComBots() {
	s.wecomMu.Lock()
	defer s.wecomMu.Unlock()
	for key := range s.wecomBots {
		delete(s.wecomBots, key)
	}
}

// syncWeComLongConnBots 将运行中的企业微信长连接群机器人与新配置列表对齐
func (s *Server) syncWeComLongConnBots(newConfigs []config.WeComBotConfig, cmsConfig *config.ClientConfig) {
	s.wecomLongConnMu.Lock()
	defer s.wecomLongConnMu.Unlock()

	if s.wecomLongConnBots == nil {
		s.wecomLongConnBots = make(map[string]*wecom.LongConnBot)
	}

	// 收集新配置中所有启用的长连接 bot（keyed by botId）
	newEnabled := make(map[string]config.WeComBotConfig)
	for _, wb := range newConfigs {
		if wb.Enabled && wb.BotID != "" && wb.BotSecret != "" && wb.EmployeeName != "" {
			newEnabled[wb.BotID] = wb
		}
	}

	// 停止已不在新配置中的实例
	for botID, bot := range s.wecomLongConnBots {
		if _, ok := newEnabled[botID]; !ok {
			log.Printf("停止企业微信长连接机器人: botId=%s", botID)
			bot.Stop()
			delete(s.wecomLongConnBots, botID)
		}
	}

	// 启动或重启有变化的实例
	for botID, wbCfg := range newEnabled {
		wbCopy := wbCfg
		if existing, ok := s.wecomLongConnBots[botID]; ok {
			existingCfg := existing.Config()
			if existingCfg.CredsEqual(&wbCopy) {
				existing.UpdateConfig(&wbCopy)
				continue
			}
			log.Printf("重启企业微信长连接机器人（配置变更）: botId=%s", botID)
			existing.Stop()
			delete(s.wecomLongConnBots, botID)
		}
		log.Printf("启动企业微信长连接机器人: botId=%s employee=%s", botID, wbCopy.EmployeeName)
		bot := wecom.NewLongConnBot(&wbCopy, cmsConfig)
		bot.Start()
		s.wecomLongConnBots[botID] = bot
	}
}

// mergeWeComBotConfigs 合并独立 wecomBot 配置和旧的 wecom[].botLongConn 嵌套配置
// 优先使用独立 wecomBot 配置，旧嵌套配置作为兼容回退
func (s *Server) mergeWeComBotConfigs(channels *config.ChannelsConfig) []config.WeComBotConfig {
	if channels == nil {
		return nil
	}

	// 收集独立 wecomBot 配置中已有的 botId
	seen := make(map[string]bool)
	result := make([]config.WeComBotConfig, 0, len(channels.WeComBot))
	for _, wb := range channels.WeComBot {
		result = append(result, wb)
		if wb.BotID != "" {
			seen[wb.BotID] = true
		}
	}

	// 兼容旧的 wecom[].botLongConn 嵌套配置：如果 botId 未在独立配置中出现，则转换并追加
	for _, wc := range channels.WeCom {
		if wc.BotLongConn != nil && wc.BotLongConn.Enabled && wc.BotLongConn.BotID != "" {
			if !seen[wc.BotLongConn.BotID] {
				result = append(result, config.WeComBotConfig{
					Enabled:              wc.BotLongConn.Enabled,
					BotID:                wc.BotLongConn.BotID,
					BotSecret:            wc.BotLongConn.BotSecret,
					EmployeeName:         wc.EmployeeName,
					ConciseReply:         wc.ConciseReply,
					URL:                  wc.BotLongConn.URL,
					PingIntervalSec:      wc.BotLongConn.PingIntervalSec,
					ReconnectDelaySec:    wc.BotLongConn.ReconnectDelaySec,
					MaxReconnectDelaySec: wc.BotLongConn.MaxReconnectDelaySec,
				})
				seen[wc.BotLongConn.BotID] = true
			}
		}
	}

	return result
}

// stopAllWeComLongConnBots 停止所有运行中的企业微信长连接群机器人
func (s *Server) stopAllWeComLongConnBots() {
	s.wecomLongConnMu.Lock()
	defer s.wecomLongConnMu.Unlock()
	for botID, bot := range s.wecomLongConnBots {
		bot.Stop()
		delete(s.wecomLongConnBots, botID)
	}
}

// reloadConfig 热重载配置：从文件重新读取并更新内存中的配置，无需重启服务
func (s *Server) reloadConfig() error {
	if s.configPath == "" {
		return fmt.Errorf("未设置配置文件路径，无法热重载")
	}

	newGlobalConfig, _, err := config.LoadConfig(s.configPath)
	if err != nil {
		return fmt.Errorf("重新加载配置文件失败: %w", err)
	}

	// 重建客户端配置（global.accessKeyId / accessKeySecret / endpoint）
	// 凭据未填写时允许继续（部分功能不可用），不阻断热重载
	newClientConfig, err := newGlobalConfig.ToClientConfig()
	if err != nil {
		log.Printf("提示: 凭据未配置，部分功能不可用（%v）", err)
		newClientConfig = &config.ClientConfig{}
	}

	// 热更新用户存储（auth.users / auth.roles）
	if s.userStore != nil {
		type yamlReloader interface {
			ReloadFromYAMLConfig(*auth.YAMLConfig) error
		}
		if r, ok := s.userStore.(yamlReloader); ok {
			newYAMLConfig := auth.ConvertYAMLConfig(newGlobalConfig.GetYAMLConfig())
			if err := r.ReloadFromYAMLConfig(newYAMLConfig); err != nil {
				log.Printf("警告: 热更新用户配置失败: %v", err)
			}
		}
	}

	// 渠道机器人热同步
	var newDTConfigs []config.DingTalkConfig
	var newFTConfigs []config.FeishuConfig
	var newWCConfigs []config.WeComConfig
	if newGlobalConfig.Channels != nil {
		newDTConfigs = newGlobalConfig.Channels.DingTalk
		newFTConfigs = newGlobalConfig.Channels.Feishu
		newWCConfigs = newGlobalConfig.Channels.WeCom
	}
	s.syncDingTalkBots(newDTConfigs, newClientConfig)
	s.syncFeishuBots(newFTConfigs, newClientConfig)
	s.syncWeComBots(newWCConfigs, newClientConfig)
	wecomBotConfigs := s.mergeWeComBotConfigs(newGlobalConfig.Channels)
	s.syncWeComLongConnBots(wecomBotConfigs, newClientConfig)

	// 原子替换配置指针
	s.mu.Lock()
	s.globalConfig = newGlobalConfig
	s.config = &client.Config{
		AccessKeyId:     newClientConfig.AccessKeyId,
		AccessKeySecret: newClientConfig.AccessKeySecret,
		Endpoint:        newClientConfig.Endpoint,
	}
	s.mu.Unlock()

	log.Printf("✅ 配置热重载完成")
	return nil
}

// createClient 为每个请求创建一个新的客户端实例
func (s *Server) createClient() (*sopchat.Client, error) {
	s.mu.RLock()
	cfg := s.config
	s.mu.RUnlock()
	if cfg == nil || cfg.AccessKeyId == "" {
		return nil, fmt.Errorf("凭据未配置，请先通过配置 UI 设置 global.accessKeyId 和 global.accessKeySecret")
	}
	return client.NewCMSClient(cfg)
}

// createCMSClient 创建 SDK 的 CMS 客户端（用于直接调用 SDK）
func (s *Server) createCMSClient() (*cmsclient.Client, error) {
	s.mu.RLock()
	cfg := s.config
	s.mu.RUnlock()
	if cfg == nil || cfg.AccessKeyId == "" {
		return nil, fmt.Errorf("凭据未配置，请先通过配置 UI 设置 global.accessKeyId 和 global.accessKeySecret")
	}
	cmsConfig := &openapiutil.Config{
		AccessKeyId:     tea.String(cfg.AccessKeyId),
		AccessKeySecret: tea.String(cfg.AccessKeySecret),
		Endpoint:        tea.String(cfg.Endpoint),
	}
	return cmsclient.NewClient(cmsConfig)
}

// handleHealth 健康检查
func (s *Server) handleHealth(c *gin.Context) {
	c.JSON(200, gin.H{
		"status":  "ok",
		"service": "sop-chat-api",
	})
}
