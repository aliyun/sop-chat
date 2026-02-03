package api

import (
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"sop-chat/internal/auth"
	"sop-chat/internal/client"
	"sop-chat/internal/config"
	"sop-chat/internal/embed"
	"sop-chat/pkg/sopchat"

	cmsclient "github.com/alibabacloud-go/cms-20240330/v6/client"
	openapiutil "github.com/alibabacloud-go/darabonba-openapi/v2/utils"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

type Server struct {
	config         *client.Config
	globalConfig   *config.Config // 添加全局配置的引用
	router         *gin.Engine
	authProvider   auth.Provider
	authMode       auth.AuthMode
	jwtManager     *auth.JWTManager
	userStore      auth.UserStore
	authMiddleware *auth.AuthMiddleware
}

func NewServer(config *client.Config, globalConfig *config.Config) (*Server, error) {
	// 创建 Gin 引擎
	router := gin.Default()

	// 配置 CORS
	corsConfig := cors.DefaultConfig()
	corsConfig.AllowAllOrigins = true
	corsConfig.AllowHeaders = []string{"Origin", "Content-Type", "Accept", "Authorization"}
	router.Use(cors.New(corsConfig))

	server := &Server{
		config:       config,
		globalConfig: globalConfig,
		router:       router,
	}

	// 初始化认证系统
	if err := server.initAuth(); err != nil {
		return nil, err
	}

	// 注册路由
	server.setupRoutes()

	return server, nil
}

// initAuth 初始化认证系统
func (s *Server) initAuth() error {
	// 加载认证配置
	authConfig, err := auth.LoadAuthConfig()
	if err != nil {
		return fmt.Errorf("failed to load auth config: %w", err)
	}

	s.authMode = authConfig.Mode

	// 如果认证模式是 disabled，跳过初始化
	if s.authMode == auth.AuthModeDisabled {
		log.Println("Authentication disabled (development mode)")
		return nil
	}

	// 创建 JWT 管理器
	s.jwtManager = auth.NewJWTManager(authConfig.JWTSecretKey, authConfig.JWTExpiresIn)

	// 根据认证模式创建相应的提供者
	switch s.authMode {
	case auth.AuthModeLocal:
		// Only use YAML configuration for user storage
		var userStore auth.UserStore

		// Use unified config YAML configuration
		if authConfig.YAMLConfig != nil && authConfig.YAMLConfig.Local != nil {
			// Create user storage from unified config
			log.Printf("🔍 Loading users from unified config")
			yamlStore, err := auth.NewYAMLUserStoreFromConfig(authConfig.YAMLConfig)
			if err != nil {
				return fmt.Errorf("failed to load users from unified config: %w. Please ensure local.user and local.roles are properly configured in config.yaml", err)
			}
			userStore = yamlStore
			log.Printf("✅ Local authentication mode enabled (unified config)")
		} else if authConfig.YAMLConfigPath != "" {
			// Load from legacy YAML config file
			log.Printf("🔍 Attempting to load YAML config file: %s", authConfig.YAMLConfigPath)
			yamlStore, err := auth.NewYAMLUserStore(authConfig.YAMLConfigPath)
			if err != nil {
				return fmt.Errorf("failed to load YAML config from %s: %w. Please ensure the config file exists and is properly formatted", authConfig.YAMLConfigPath, err)
			}
			userStore = yamlStore
			log.Printf("✅ Local authentication mode enabled (YAML config)")
		} else {
			// No configuration provided
			return fmt.Errorf("no user configuration found. Please configure local.user and local.roles in config.yaml")
		}

		s.userStore = userStore

		// Create local authentication provider
		s.authProvider = auth.NewLocalAuthProvider(userStore, s.jwtManager)

	case auth.AuthModeLDAP:
		// TODO: 实现 LDAP 认证提供者
		return auth.ErrUnsupportedAuthMode

	case auth.AuthModeOIDC:
		// TODO: 实现 OIDC 认证提供者
		return auth.ErrUnsupportedAuthMode

	default:
		return auth.ErrUnsupportedAuthMode
	}

	// 创建认证中间件
	s.authMiddleware = auth.NewAuthMiddleware(s.authProvider, s.authMode)

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

			// 如果请求的是 API 路由，返回 404
			if len(path) >= 4 && path[:4] == "/api" {
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

// createClient 为每个请求创建一个新的客户端实例
func (s *Server) createClient() (*sopchat.Client, error) {
	return client.NewCMSClient(s.config)
}

// createCMSClient 创建 SDK 的 CMS 客户端（用于直接调用 SDK）
func (s *Server) createCMSClient() (*cmsclient.Client, error) {
	cmsConfig := &openapiutil.Config{
		AccessKeyId:     tea.String(s.config.AccessKeyId),
		AccessKeySecret: tea.String(s.config.AccessKeySecret),
		Endpoint:        tea.String(s.config.Endpoint),
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
