package auth

import (
	"context"
	"fmt"
)

// LocalAuthProvider 本地认证提供者
type LocalAuthProvider struct {
	userStore UserStore
	jwtMgr    *JWTManager
}

// NewLocalAuthProvider 创建本地认证提供者
func NewLocalAuthProvider(userStore UserStore, jwtMgr *JWTManager) *LocalAuthProvider {
	return &LocalAuthProvider{
		userStore: userStore,
		jwtMgr:    jwtMgr,
	}
}

// Authenticate 验证用户凭据
func (p *LocalAuthProvider) Authenticate(ctx context.Context, username, password string) (*User, error) {
	// 验证密码
	valid, err := p.userStore.ValidatePassword(username, password)
	if err != nil {
		return nil, fmt.Errorf("认证失败: %w", err)
	}
	
	if !valid {
		return nil, fmt.Errorf("用户名或密码错误")
	}
	
	// 获取用户信息
	storedUser, err := p.userStore.GetUser(username)
	if err != nil {
		return nil, fmt.Errorf("获取用户信息失败: %w", err)
	}
	
	return &User{
		Username: storedUser.Username,
		Email:    storedUser.Email,
		Roles:    storedUser.Roles,
	}, nil
}

// GetUser 根据用户名获取用户信息
func (p *LocalAuthProvider) GetUser(ctx context.Context, username string) (*User, error) {
	storedUser, err := p.userStore.GetUser(username)
	if err != nil {
		return nil, fmt.Errorf("获取用户信息失败: %w", err)
	}
	
	return &User{
		Username: storedUser.Username,
		Email:    storedUser.Email,
		Roles:    storedUser.Roles,
	}, nil
}

// ValidateToken 验证 token（通过 JWT 管理器）
func (p *LocalAuthProvider) ValidateToken(ctx context.Context, token string) (*User, error) {
	claims, err := p.jwtMgr.ValidateToken(token)
	if err != nil {
		return nil, fmt.Errorf("token 验证失败: %w", err)
	}
	
	return &User{
		Username: claims.Username,
		Email:    claims.Email,
		Roles:    claims.Roles,
	}, nil
}
