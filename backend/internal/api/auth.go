package api

import (
	"crypto/subtle"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"sop-chat/internal/auth"

	"github.com/gin-gonic/gin"
)

// LoginRequest 登录请求
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// LoginResponse 登录响应
type LoginResponse struct {
	Token    string         `json:"token"`
	User     *auth.User     `json:"user"`
}

// handleLogin 处理登录请求
func (s *Server) handleLogin(c *gin.Context) {
	// methods 为空时登录功能关闭
	if len(s.authModes) == 0 {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "认证未配置，请在管理后台设置 auth.methods",
		})
		return
	}

	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  "Invalid request parameters",
			"detail": err.Error(),
		})
		return
	}

	// 使用认证链验证用户
	user, err := s.authProvider.Authenticate(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":  "Authentication failed",
			"detail": err.Error(),
		})
		return
	}

	// 生成 JWT token
	token, err := s.jwtManager.GenerateToken(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "Failed to generate token",
			"detail": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, LoginResponse{
		Token: token,
		User:  user,
	})
}

// handleGetCurrentUser 获取当前用户信息
func (s *Server) handleGetCurrentUser(c *gin.Context) {
	user, exists := auth.GetUserFromContext(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Not authenticated",
		})
		return
	}

	c.JSON(http.StatusOK, user)
}

// handleLogout 处理登出请求（客户端删除 token 即可）
func (s *Server) handleLogout(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"message": "Logout successful",
	})
}

const oidcStateCookieName = "oidc_state"

func isSecureRequest(c *gin.Context) bool {
	if c.Request != nil && c.Request.TLS != nil {
		return true
	}
	return strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
}

// handleOIDCLogin 发起 OIDC 登录：生成 state，重定向到 OIDC Provider
func (s *Server) handleOIDCLogin(c *gin.Context) {
	if s.oidcProvider == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "OIDC 认证未启用"})
		return
	}

	// 生成随机 state 防止 CSRF
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成 state 失败"})
		return
	}
	state := base64.RawURLEncoding.EncodeToString(b)

	// 将 state 绑定到浏览器会话，防止登录 CSRF / 账号混淆。
	secure := isSecureRequest(c)
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(oidcStateCookieName, state, 300, "/api/auth/oidc", "", secure, true)

	authURL := s.oidcProvider.GetAuthorizationURL(state)
	c.Redirect(http.StatusFound, authURL)
}

// handleOIDCCallback 处理 OIDC 回调：校验 state，换取 token，重定向回前端
func (s *Server) handleOIDCCallback(c *gin.Context) {
	if s.oidcProvider == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "OIDC 认证未启用"})
		return
	}

	// 检查 OIDC Provider 返回的错误
	if errMsg := c.Query("error"); errMsg != "" {
		desc := c.Query("error_description")
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  fmt.Sprintf("OIDC 认证失败: %s", errMsg),
			"detail": desc,
		})
		return
	}

	// 校验 state
	state := c.Query("state")
	cookieState, err := c.Cookie(oidcStateCookieName)
	if state == "" || err != nil || len(state) != len(cookieState) ||
		subtle.ConstantTimeCompare([]byte(state), []byte(cookieState)) != 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 state 参数，请重新登录"})
		return
	}
	// 单次使用后清除，避免重放。
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(oidcStateCookieName, "", -1, "/api/auth/oidc", "", isSecureRequest(c), true)

	// 用 authorization code 换取用户信息
	code := c.Query("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 authorization code"})
		return
	}

	user, err := s.oidcProvider.HandleCallback(c.Request.Context(), code)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "OIDC 认证失败",
			"detail": err.Error(),
		})
		return
	}

	// 生成本地 JWT
	token, err := s.jwtManager.GenerateToken(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "生成 token 失败",
			"detail": err.Error(),
		})
		return
	}

	// 将 user 序列化为 JSON 并 URL encode
	userJSON, _ := json.Marshal(user)

	// 返回一个中间页面：直接写 localStorage 后跳转，避免 React 生命周期竞态
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, fmt.Sprintf(`<!DOCTYPE html><html><body><script>
localStorage.setItem("auth_token",%q);
localStorage.setItem("auth_user",%q);
window.location.replace("/#/");
</script></body></html>`, token, string(userJSON)))
}
