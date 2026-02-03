package auth

import (
	"context"
	"errors"
)

var (
	ErrUnsupportedAuthMode = errors.New("unsupported authentication mode")
)

// AuthMode 认证模式
type AuthMode string

const (
	AuthModeLocal     AuthMode = "local"      // 本地账号密码
	AuthModeLDAP      AuthMode = "ldap"       // LDAP 认证（未来支持）
	AuthModeOIDC      AuthMode = "oidc"       // OpenID Connect（未来支持）
	AuthModeDisabled  AuthMode = "disabled"   // 禁用认证（开发模式）
)

// User 用户信息
type User struct {
	Username string
	Email    string
	Roles    []string
}

// AuthResult 认证结果
type AuthResult struct {
	User  *User
	Token string // JWT token
}

// Provider 认证提供者接口
type Provider interface {
	// Authenticate 验证用户凭据
	Authenticate(ctx context.Context, username, password string) (*User, error)
	
	// GetUser 根据用户名获取用户信息
	GetUser(ctx context.Context, username string) (*User, error)
	
	// ValidateToken 验证 token（可选，某些提供者可能不需要）
	ValidateToken(ctx context.Context, token string) (*User, error)
}
