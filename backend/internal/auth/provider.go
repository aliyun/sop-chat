package auth

import (
	"context"
	"errors"
	"fmt"
)

var (
	ErrUnsupportedAuthMode = errors.New("unsupported authentication mode")
)

// AuthMode 认证模式
type AuthMode string

const (
	AuthModeBuiltin AuthMode = "builtin" // 内置账号密码（builtinUsers/roles）
	AuthModeLDAP    AuthMode = "ldap"    // LDAP 认证（未来支持）
	AuthModeOIDC    AuthMode = "oidc"    // OpenID Connect（未来支持）
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

	// ValidateToken 验证 token
	ValidateToken(ctx context.Context, token string) (*User, error)
}

// ChainProvider 鉴权链提供者：依序尝试每个 Provider，第一个成功即返回。
// ValidateToken 直接使用共享的 JWTManager，与具体 Provider 解耦。
type ChainProvider struct {
	providers  []Provider
	jwtManager *JWTManager
}

// NewChainProvider 创建鉴权链提供者
func NewChainProvider(providers []Provider, jwtManager *JWTManager) *ChainProvider {
	return &ChainProvider{providers: providers, jwtManager: jwtManager}
}

func (c *ChainProvider) Authenticate(ctx context.Context, username, password string) (*User, error) {
	var lastErr error
	for _, p := range c.providers {
		user, err := p.Authenticate(ctx, username, password)
		if err == nil {
			return user, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no auth providers configured")
}

func (c *ChainProvider) GetUser(ctx context.Context, username string) (*User, error) {
	for _, p := range c.providers {
		user, err := p.GetUser(ctx, username)
		if err == nil {
			return user, nil
		}
	}
	return nil, fmt.Errorf("user not found: %s", username)
}

func (c *ChainProvider) ValidateToken(ctx context.Context, token string) (*User, error) {
	claims, err := c.jwtManager.ValidateToken(token)
	if err != nil {
		return nil, fmt.Errorf("token 验证失败: %w", err)
	}
	return &User{
		Username: claims.Username,
		Email:    claims.Email,
		Roles:    claims.Roles,
	}, nil
}
