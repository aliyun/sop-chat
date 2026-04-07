package auth

import (
	"context"
	"fmt"
	"log"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// OIDCProvider OIDC 认证提供者
type OIDCProvider struct {
	oauth2Config *oauth2.Config
	oidcProvider *oidc.Provider
	verifier     *oidc.IDTokenVerifier
	roles        []RoleConfig
	usernameClaim string
}

// NewOIDCProvider 创建 OIDC 认证提供者
// 会执行 OIDC Discovery（请求 issuerURL/.well-known/openid-configuration）
func NewOIDCProvider(ctx context.Context, cfg *OIDCConfig, roles []RoleConfig) (*OIDCProvider, error) {
	if cfg == nil {
		return nil, fmt.Errorf("OIDC 配置为空")
	}
	if cfg.IssuerURL == "" || cfg.ClientID == "" || cfg.ClientSecret == "" || cfg.RedirectURL == "" {
		return nil, fmt.Errorf("OIDC 配置不完整: issuerURL, clientId, clientSecret, redirectURL 均为必填项")
	}

	provider, err := oidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("OIDC Discovery 失败 (%s): %w", cfg.IssuerURL, err)
	}

	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	}

	oauth2Cfg := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       scopes,
	}

	verifier := provider.Verifier(&oidc.Config{
		ClientID: cfg.ClientID,
	})

	usernameClaim := cfg.UsernameClaim
	if usernameClaim == "" {
		usernameClaim = "preferred_username"
	}

	log.Printf("✅ OIDC 认证就绪 (issuer=%s, clientId=%s)", cfg.IssuerURL, cfg.ClientID)

	return &OIDCProvider{
		oauth2Config:  oauth2Cfg,
		oidcProvider:  provider,
		verifier:      verifier,
		roles:         roles,
		usernameClaim: usernameClaim,
	}, nil
}

// GetAuthorizationURL 生成 OIDC 授权 URL
func (p *OIDCProvider) GetAuthorizationURL(state string) string {
	return p.oauth2Config.AuthCodeURL(state)
}

// HandleCallback 处理 OIDC 回调：用 authorization code 换取 token，验证 ID Token，提取用户信息
func (p *OIDCProvider) HandleCallback(ctx context.Context, code string) (*User, error) {
	// 用 code 换取 OAuth2 token
	oauth2Token, err := p.oauth2Config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("token 交换失败: %w", err)
	}

	// 从 OAuth2 token 中提取 ID Token
	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("OAuth2 响应中缺少 id_token")
	}

	// 验证 ID Token（签名、issuer、audience、expiry）
	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("ID Token 验证失败: %w", err)
	}

	// 提取 claims
	var claims map[string]interface{}
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("解析 ID Token claims 失败: %w", err)
	}

	// 提取用户名
	username := extractStringClaim(claims, p.usernameClaim)
	if username == "" {
		// fallback 到 sub
		username = extractStringClaim(claims, "sub")
	}
	if username == "" {
		return nil, fmt.Errorf("无法从 ID Token 中提取用户名 (claim: %s)", p.usernameClaim)
	}

	// 提取 email
	email := extractStringClaim(claims, "email")

	// 从 roles 配置中查找该用户的角色
	userRoles := p.resolveRoles(username)

	return &User{
		Username: username,
		Email:    email,
		Roles:    userRoles,
	}, nil
}

// Authenticate OIDC 不支持用户名密码认证，用户需通过浏览器重定向流程登录
func (p *OIDCProvider) Authenticate(ctx context.Context, username, password string) (*User, error) {
	return nil, fmt.Errorf("OIDC 认证不支持用户名密码登录，请使用 SSO 登录")
}

// GetUser 根据用户名获取用户信息（从 roles 配置中查找角色）
func (p *OIDCProvider) GetUser(ctx context.Context, username string) (*User, error) {
	return &User{
		Username: username,
		Roles:    p.resolveRoles(username),
	}, nil
}

// ValidateToken OIDC provider 不直接验证 JWT（由 ChainProvider 统一处理）
func (p *OIDCProvider) ValidateToken(ctx context.Context, token string) (*User, error) {
	return nil, fmt.Errorf("OIDC provider 不直接验证本地 JWT")
}

// resolveRoles 从 roles 配置中查找用户的角色
func (p *OIDCProvider) resolveRoles(username string) []string {
	var roles []string
	for _, role := range p.roles {
		for _, u := range role.Users {
			if u == username {
				roles = append(roles, role.Name)
				break
			}
		}
	}
	return roles
}

// extractStringClaim 从 claims map 中提取字符串值
func extractStringClaim(claims map[string]interface{}, key string) string {
	if v, ok := claims[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
