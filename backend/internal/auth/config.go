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
	Mode           AuthMode      // 认证模式
	JWTSecretKey   string        // JWT 密钥
	JWTExpiresIn   time.Duration // JWT 过期时间
	DataDir        string        // 数据目录（用于存储用户数据）
	YAMLConfigPath string        // YAML 配置文件路径（兼容旧配置）
	YAMLConfig     *YAMLConfig   // YAML 配置（统一配置模式下使用）
}

// LoadAuthConfig 从统一配置文件或环境变量加载认证配置
func LoadAuthConfig() (*Config, error) {
	// 首先尝试从统一配置文件加载
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config.yaml"
	}
	
	unifiedConfig, actualPath, err := config.LoadConfig(configPath)
	if err == nil {
		// 成功加载统一配置，转换为认证配置
		authConfigData := unifiedConfig.GetAuthConfig()
		
		// 认证模式
		modeStr := authConfigData.Method
		if modeStr == "" {
			modeStr = os.Getenv("AUTH_MODE")
			if modeStr == "" {
				modeStr = "local"
			}
		}
		mode := AuthMode(modeStr)

		// 验证认证模式
		switch mode {
		case AuthModeLocal, AuthModeLDAP, AuthModeOIDC, AuthModeDisabled:
			// 有效模式
		default:
			return nil, fmt.Errorf("无效的认证模式: %s，支持的模式: local, ldap, oidc, disabled", modeStr)
		}

		// JWT 配置
		jwtSecretKey := authConfigData.JWTSecretKey
		if jwtSecretKey == "" {
			jwtSecretKey = os.Getenv("JWT_SECRET_KEY")
			if jwtSecretKey == "" {
				jwtSecretKey = "default-secret-key-change-in-production"
			}
		}

		jwtExpiresInStr := authConfigData.JWTExpiresIn
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

		// 数据目录
		dataDir := authConfigData.DataDir
		if dataDir == "" {
			dataDir = os.Getenv("AUTH_DATA_DIR")
			if dataDir == "" {
				dataDir = "./data"
			}
		}

		authConfig := &Config{
			Mode:           mode,
			JWTSecretKey:   jwtSecretKey,
			JWTExpiresIn:   jwtExpiresIn,
			DataDir:        dataDir,
			YAMLConfigPath: "",
		}

		// 转换 YAML 配置（用于从统一配置加载用户）
		if yamlConfigForAuth := unifiedConfig.GetYAMLConfig(); yamlConfigForAuth != nil {
			authConfig.YAMLConfig = convertYAMLConfig(yamlConfigForAuth)
		}

		log.Printf("📄 使用统一配置文件: %s", actualPath)
		log.Printf("✅ 从统一配置读取认证模式: %s", authConfig.Mode)
		return authConfig, nil
	}

	// 如果统一配置文件不存在，回退到旧的 YAML 配置方式
	log.Printf("⚠️  统一配置文件不存在，回退到旧的配置方式")
	
	// 读取 YAML 配置文件路径
	yamlConfigPath := os.Getenv("AUTH_YAML_PATH")
	if yamlConfigPath == "" {
		yamlConfigPath = "backend/auth.yaml"
	}

	// 尝试加载 YAML 配置以确定认证模式
	var mode AuthMode
	var modeStr string

	// 首先尝试从 YAML 文件读取认证模式
	yamlConfig, actualPath, err := LoadYAMLConfig(yamlConfigPath)
	if err == nil && yamlConfig.Method != "" {
		modeStr = yamlConfig.Method
		mode = AuthMode(modeStr)
		log.Printf("📄 使用 YAML 配置文件: %s", actualPath)
		log.Printf("✅ 从 YAML 配置读取认证模式: %s", modeStr)
		// 更新为实际找到的路径
		yamlConfigPath = actualPath
	} else {
		// 如果 YAML 文件不存在或无法读取，从环境变量读取
		if err != nil {
			log.Printf("⚠️  无法加载 YAML 配置文件 (%v)，将使用环境变量配置", err)
		}
		modeStr = os.Getenv("AUTH_MODE")
		if modeStr == "" {
			modeStr = "local"
		}
		mode = AuthMode(modeStr)
		log.Printf("✅ 从环境变量读取认证模式: %s", modeStr)
	}

	// 验证认证模式
	switch mode {
	case AuthModeLocal, AuthModeLDAP, AuthModeOIDC, AuthModeDisabled:
		// 有效模式
	default:
		return nil, fmt.Errorf("无效的认证模式: %s，支持的模式: local, ldap, oidc, disabled", modeStr)
	}

	// JWT 配置
	jwtSecretKey := os.Getenv("JWT_SECRET_KEY")
	if jwtSecretKey == "" {
		jwtSecretKey = "default-secret-key-change-in-production"
	}

	jwtExpiresInStr := os.Getenv("JWT_EXPIRES_IN")
	if jwtExpiresInStr == "" {
		jwtExpiresInStr = "24h"
	}

	jwtExpiresIn, err := time.ParseDuration(jwtExpiresInStr)
	if err != nil {
		return nil, fmt.Errorf("无效的 JWT 过期时间格式: %w", err)
	}

	// 数据目录
	dataDir := os.Getenv("AUTH_DATA_DIR")
	if dataDir == "" {
		dataDir = "./data"
	}

	return &Config{
		Mode:           mode,
		JWTSecretKey:   jwtSecretKey,
		JWTExpiresIn:   jwtExpiresIn,
		DataDir:        dataDir,
		YAMLConfigPath: yamlConfigPath,
	}, nil
}

// convertYAMLConfig 将 config 包的配置转换为 auth 包的配置
func convertYAMLConfig(cfg *config.YAMLConfigForAuth) *YAMLConfig {
	if cfg == nil {
		return nil
	}
	
	result := &YAMLConfig{
		Method: cfg.Method,
	}
	
	if cfg.Local != nil {
		result.Local = &LocalConfig{
			Users: make([]UserConfig, len(cfg.Local.Users)),
			Roles: make([]RoleConfig, len(cfg.Local.Roles)),
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
		result.LDAP = &LDAPConfig{}
	}
	
	if cfg.OIDC != nil {
		result.OIDC = &OIDCConfig{}
	}
	
	return result
}
