package api

import (
	"net/http"

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
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  "Invalid request parameters",
			"detail": err.Error(),
		})
		return
	}

	// 使用认证提供者验证用户
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
