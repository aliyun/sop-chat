package auth

import (
	"fmt"
	"log"
	"os"
	"time"

	"sop-chat/internal/config"
)

// Config 认证配置
type Config struct {
	Modes          []AuthMode    // 鉴权链（有序），为空表示登录关闭
	JWTSecretKey   string        // JWT 密钥
	JWTExpiresIn   time.Duration // JWT 过期时间
	YAMLConfigPath string        // YAML 配置文件路径（兼容旧配置）
	YAMLConfig     *YAMLConfig   // YAML 配置（统一配置模式下使用）
}

// parseAuthMode 解析认证模式字符串，兼容历史值 "local" → AuthModeBuiltin
func parseAuthMode(s string) (AuthMode, error) {
	switch AuthMode(s) {
	case AuthModeBuiltin:
		return AuthModeBuiltin, nil
	case "local": // 向后兼容
		return AuthModeBuiltin, nil
	case AuthModeLDAP:
		return AuthModeLDAP, nil
	case AuthModeOIDC:
		return AuthModeOIDC, nil
	default:
		return "", fmt.Errorf("无效的认证模式: %q，支持的值: builtin, ldap, oidc", s)
	}
}

// LoadAuthConfig 从统一配置文件加载认证配置。
// main.go 在调用 NewServer 之前会确保 config.yaml 已存在（不存在则自动创建默认文件），
// 因此此处只需读取统一配置，不再有旧版 auth.yaml 回退逻辑。
func LoadAuthConfig() (*Config, error) {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config.yaml"
	}

	unifiedConfig, actualPath, err := config.LoadConfig(configPath)
	if err != nil {
		// 极端情况：文件刚被创建但尚未写入（或权限问题），使用内置默认值
		log.Printf("⚠️  无法读取配置文件 (%v)，使用内置默认认证配置（登录关闭）", err)
		return &Config{
			Modes:        []AuthMode{},
			JWTSecretKey: "default-secret-key-change-in-production",
			JWTExpiresIn: 24 * time.Hour,
		}, nil
	}

	authConfigData := unifiedConfig.GetAuthConfig()

	// 解析鉴权链
	modes := make([]AuthMode, 0, len(authConfigData.Methods))
	for _, m := range authConfigData.Methods {
		mode, err := parseAuthMode(m)
		if err != nil {
			return nil, err
		}
		modes = append(modes, mode)
	}

	// JWT 配置
	jwtSecretKey := authConfigData.JWT.SecretKey
	if jwtSecretKey == "" {
		jwtSecretKey = os.Getenv("JWT_SECRET_KEY")
		if jwtSecretKey == "" {
			jwtSecretKey = "default-secret-key-change-in-production"
		}
	}

	jwtExpiresInStr := authConfigData.JWT.ExpiresIn
	if jwtExpiresInStr == "" {
		jwtExpiresInStr = os.Getenv("JWT_EXPIRES_IN")
		if jwtExpiresInStr == "" {
			jwtExpiresInStr = "24h"
		}
	}

	jwtExpiresIn, err := time.ParseDuration(jwtExpiresInStr)
	if err != nil {
		return nil, fmt.Errorf("无效的 JWT 过期时间格式: %w", err)
	}

	authConfig := &Config{
		Modes:          modes,
		JWTSecretKey:   jwtSecretKey,
		JWTExpiresIn:   jwtExpiresIn,
		YAMLConfigPath: "",
	}
	authConfig.YAMLConfig = convertYAMLConfig(unifiedConfig.GetYAMLConfig())

	log.Printf("📄 使用统一配置文件: %s", actualPath)
	if len(modes) == 0 {
		log.Printf("⚠️  auth.methods 为空，登录功能已关闭，请通过配置 UI 设置")
	} else {
		log.Printf("✅ 认证链: %v", modes)
	}
	return authConfig, nil
}

// ConvertYAMLConfig 将 config 包的配置转换为 auth 包的配置（供外部包使用）
func ConvertYAMLConfig(cfg *config.YAMLConfigForAuth) *YAMLConfig {
	return convertYAMLConfig(cfg)
}

// convertYAMLConfig 将 config 包的配置转换为 auth 包的配置
func convertYAMLConfig(cfg *config.YAMLConfigForAuth) *YAMLConfig {
	if cfg == nil {
		return nil
	}

	result := &YAMLConfig{}

	if cfg.Local != nil {
		result.Local = &LocalConfig{
			PasswordSalt: cfg.Local.PasswordSalt,
			Users:        make([]UserConfig, len(cfg.Local.Users)),
			Roles:        make([]RoleConfig, len(cfg.Local.Roles)),
		}
		for i, u := range cfg.Local.Users {
			result.Local.Users[i] = UserConfig{
				Name:     u.Name,
				Password: u.Password,
			}
		}
		for i, r := range cfg.Local.Roles {
			result.Local.Roles[i] = RoleConfig{
				Name:  r.Name,
				Users: r.Users,
			}
		}
	}

	if cfg.LDAP != nil {
		result.LDAP = &LDAPConfig{
			Host:         cfg.LDAP.Host,
			Port:         cfg.LDAP.Port,
			UseTLS:       cfg.LDAP.UseTLS,
			BindDN:       cfg.LDAP.BindDN,
			BindPassword: cfg.LDAP.BindPassword,
			BaseDN:       cfg.LDAP.BaseDN,
			UserFilter:   cfg.LDAP.UserFilter,
			UsernameAttr: cfg.LDAP.UsernameAttr,
			DisplayAttr:  cfg.LDAP.DisplayAttr,
			EmailAttr:    cfg.LDAP.EmailAttr,
		}
	}

	if cfg.OIDC != nil {
		result.OIDC = &OIDCConfig{
			IssuerURL:     cfg.OIDC.IssuerURL,
			ClientID:      cfg.OIDC.ClientID,
			ClientSecret:  cfg.OIDC.ClientSecret,
			RedirectURL:   cfg.OIDC.RedirectURL,
			Scopes:        cfg.OIDC.Scopes,
			UsernameClaim: cfg.OIDC.UsernameClaim,
		}
	}

	return result
}
