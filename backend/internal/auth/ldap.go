package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-ldap/ldap/v3"
)

// LDAPProvider LDAP 认证提供者
type LDAPProvider struct {
	cfg              *LDAPConfig
	roles            []RoleConfig
	userAttr         string
	mailAttr         string
	filter           string
	groupRoleMapping map[string][]string
}

// NewLDAPProvider 创建 LDAP 认证提供者
func NewLDAPProvider(cfg *LDAPConfig, roles []RoleConfig) (*LDAPProvider, error) {
	if cfg == nil {
		return nil, fmt.Errorf("LDAP 配置为空")
	}
	if cfg.Host == "" || cfg.BaseDN == "" {
		return nil, fmt.Errorf("LDAP 配置不完整: host/baseDN 为必填项")
	}

	port := cfg.Port
	if port == 0 {
		if cfg.UseTLS {
			port = 636
		} else {
			port = 389
		}
	}
	cfg.Port = port

	userAttr := cfg.UsernameAttr
	if userAttr == "" {
		userAttr = "uid"
	}
	mailAttr := cfg.EmailAttr
	if mailAttr == "" {
		mailAttr = "mail"
	}
	filter := cfg.UserFilter
	if filter == "" {
		filter = fmt.Sprintf("(%s={username})", userAttr)
	}
	groupRoleMapping := make(map[string][]string)
	for _, m := range cfg.GroupRoleMappings {
		groupDN := normalizeDN(m.GroupDN)
		role := strings.TrimSpace(m.Role)
		if groupDN == "" || role == "" {
			continue
		}
		groupRoleMapping[groupDN] = append(groupRoleMapping[groupDN], role)
	}

	return &LDAPProvider{
		cfg:              cfg,
		roles:            roles,
		userAttr:         userAttr,
		mailAttr:         mailAttr,
		filter:           filter,
		groupRoleMapping: groupRoleMapping,
	}, nil
}

func (p *LDAPProvider) dial() (*ldap.Conn, error) {
	scheme := "ldap"
	if p.cfg.UseTLS {
		scheme = "ldaps"
	}
	addr := fmt.Sprintf("%s://%s:%d", scheme, p.cfg.Host, p.cfg.Port)
	conn, err := ldap.DialURL(addr)
	if err != nil {
		return nil, fmt.Errorf("连接 LDAP 失败: %w", err)
	}
	return conn, nil
}

func (p *LDAPProvider) bindAsSearchUser(conn *ldap.Conn) error {
	if p.cfg.BindDN == "" {
		return nil
	}
	if err := conn.Bind(p.cfg.BindDN, p.cfg.BindPassword); err != nil {
		return fmt.Errorf("LDAP 查询账号绑定失败: %w", err)
	}
	return nil
}

func (p *LDAPProvider) searchUser(conn *ldap.Conn, username string) (*ldap.Entry, error) {
	escaped := ldap.EscapeFilter(username)
	filter := p.filter
	filter = replaceUsernameToken(filter, escaped)

	req := ldap.NewSearchRequest(
		p.cfg.BaseDN,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		1,
		10,
		false,
		filter,
		[]string{p.userAttr, p.mailAttr, "memberOf"},
		nil,
	)

	resp, err := conn.Search(req)
	if err != nil {
		if ldapErr, ok := err.(*ldap.Error); ok && ldapErr.ResultCode == ldap.LDAPResultNoSuchObject {
			return nil, fmt.Errorf("LDAP 用户搜索失败: Base DN 不存在或不可访问 (baseDN=%q, filter=%q)", p.cfg.BaseDN, filter)
		}
		return nil, fmt.Errorf("LDAP 用户搜索失败: %w", err)
	}
	if len(resp.Entries) == 0 {
		return nil, fmt.Errorf("用户不存在")
	}
	return resp.Entries[0], nil
}

func replaceUsernameToken(filter, escaped string) string {
	return strings.ReplaceAll(filter, "{username}", escaped)
}

// Authenticate 验证用户名密码
func (p *LDAPProvider) Authenticate(ctx context.Context, username, password string) (*User, error) {
	_ = ctx
	if password == "" {
		return nil, fmt.Errorf("用户名或密码错误")
	}

	conn, err := p.dial()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := p.bindAsSearchUser(conn); err != nil {
		return nil, err
	}

	entry, err := p.searchUser(conn, username)
	if err != nil {
		return nil, err
	}

	// 使用用户 DN + 密码校验凭据
	if err := conn.Bind(entry.DN, password); err != nil {
		return nil, fmt.Errorf("用户名或密码错误")
	}

	normalizedUsername := entry.GetAttributeValue(p.userAttr)
	if normalizedUsername == "" {
		normalizedUsername = username
	}

	return &User{
		Username: normalizedUsername,
		Email:    entry.GetAttributeValue(p.mailAttr),
		Roles:    p.resolveRoles(normalizedUsername, entry.GetAttributeValues("memberOf")),
	}, nil
}

// GetUser 根据用户名获取 LDAP 用户信息（用于兼容接口）
func (p *LDAPProvider) GetUser(ctx context.Context, username string) (*User, error) {
	_ = ctx
	conn, err := p.dial()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := p.bindAsSearchUser(conn); err != nil {
		return nil, err
	}
	entry, err := p.searchUser(conn, username)
	if err != nil {
		return nil, err
	}

	normalizedUsername := entry.GetAttributeValue(p.userAttr)
	if normalizedUsername == "" {
		normalizedUsername = username
	}
	return &User{
		Username: normalizedUsername,
		Email:    entry.GetAttributeValue(p.mailAttr),
		Roles:    p.resolveRoles(normalizedUsername, entry.GetAttributeValues("memberOf")),
	}, nil
}

// ValidateToken LDAP provider 不直接验证 JWT（由 ChainProvider 统一处理）
func (p *LDAPProvider) ValidateToken(ctx context.Context, token string) (*User, error) {
	_ = ctx
	_ = token
	return nil, fmt.Errorf("LDAP provider 不直接验证本地 JWT")
}

func (p *LDAPProvider) resolveRoles(username string, groupDNs []string) []string {
	var resolved []string
	seen := map[string]bool{}
	for _, role := range p.roles {
		for _, u := range role.Users {
			if u == username {
				if !seen[role.Name] {
					resolved = append(resolved, role.Name)
					seen[role.Name] = true
				}
				break
			}
		}
	}
	for _, groupDN := range groupDNs {
		key := normalizeDN(groupDN)
		for _, role := range p.groupRoleMapping[key] {
			if !seen[role] {
				resolved = append(resolved, role)
				seen[role] = true
			}
		}
	}
	return resolved
}

func normalizeDN(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
