package auth

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware 认证中间件
type AuthMiddleware struct {
	provider Provider
}

// NewAuthMiddleware 创建认证中间件
func NewAuthMiddleware(provider Provider) *AuthMiddleware {
	return &AuthMiddleware{provider: provider}
}

// RequireAuth 要求认证的中间件
func (m *AuthMiddleware) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// provider 为 nil 表示 methods 为空，登录功能已关闭
		if m.provider == nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "认证未配置，请在管理后台设置 auth.methods",
			})
			c.Abort()
			return
		}

		// 从请求头获取 token
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Missing authentication information",
			})
			c.Abort()
			return
		}

		// 解析 Bearer token
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Invalid authentication format, Bearer token required",
			})
			c.Abort()
			return
		}
		
		token := parts[1]
		
		// 验证 token
		user, err := m.provider.ValidateToken(c.Request.Context(), token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Authentication failed",
				"detail": err.Error(),
			})
			c.Abort()
			return
		}
		
		// 将用户信息存储到上下文中
		c.Set("user", user)
		c.Set("username", user.Username)
		
		c.Next()
	}
}

// OptionalAuth 可选认证的中间件（如果提供了 token 则验证，否则继续）
func (m *AuthMiddleware) OptionalAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.Next()
			return
		}
		
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.Next()
			return
		}
		
		token := parts[1]
		user, err := m.provider.ValidateToken(c.Request.Context(), token)
		if err == nil {
			c.Set("user", user)
			c.Set("username", user.Username)
		}
		
		c.Next()
	}
}

// RequireRole 要求指定角色的中间件
func (m *AuthMiddleware) RequireRole(requiredRole string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 先确保用户已认证
		user, exists := GetUserFromContext(c)
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Unauthenticated request",
			})
			c.Abort()
			return
		}
		
		// 检查用户是否具有所需的角色
		hasRole := false
		for _, role := range user.Roles {
			if role == requiredRole {
				hasRole = true
				break
			}
		}
		
		if !hasRole {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "Insufficient permissions",
				"detail": fmt.Sprintf("Requires %s role to perform this operation", requiredRole),
			})
			c.Abort()
			return
		}
		
		c.Next()
	}
}

// RequireAnyRole 要求任一角色的中间件
func (m *AuthMiddleware) RequireAnyRole(requiredRoles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 先确保用户已认证
		user, exists := GetUserFromContext(c)
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Unauthenticated request",
			})
			c.Abort()
			return
		}
		
		// 检查用户是否具有任一所需的角色
		hasRole := false
		for _, userRole := range user.Roles {
			for _, requiredRole := range requiredRoles {
				if userRole == requiredRole {
					hasRole = true
					break
				}
			}
			if hasRole {
				break
			}
		}
		
		if !hasRole {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "Insufficient permissions",
				"detail": fmt.Sprintf("Requires one of the following roles to perform this operation: %v", requiredRoles),
			})
			c.Abort()
			return
		}
		
		c.Next()
	}
}

// GetUserFromContext 从上下文获取用户信息
func GetUserFromContext(c *gin.Context) (*User, bool) {
	user, exists := c.Get("user")
	if !exists {
		return nil, false
	}
	
	u, ok := user.(*User)
	return u, ok
}
