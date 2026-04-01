package api

import (
	"testing"

	"sop-chat/internal/config"
)

func TestBuildConfigFromUIPreservesLegacyContextAndAuthProviders(t *testing.T) {
	existing := &config.Config{
		Global: config.GlobalConfig{
			AccessKeyId:     "legacy-ak",
			AccessKeySecret: "legacy-sk",
			Endpoint:        "cms.cn-hangzhou.aliyuncs.com",
			Product:         "cms",
			Workspace:       "legacy-workspace",
			Region:          "cn-shanghai",
		},
		Auth: config.AuthConfig{
			Methods:      []string{"builtin", "ldap"},
			PasswordSalt: "old-salt",
			JWT: config.JWTConfig{
				SecretKey: "old-secret",
				ExpiresIn: "24h",
			},
			BuiltinUsers: []config.UserConfig{{Name: "legacy-admin", Password: "old-hash"}},
			Roles:        []config.RoleConfig{{Name: "legacy-admin", Users: []string{"legacy-admin"}}},
			LDAP:         &config.LDAPConfig{Host: "ldap.example.com", Port: 389},
			OIDC:         &config.OIDCConfig{IssuerURL: "https://issuer.example.com"},
		},
	}

	req := configUIResponse{
		Server: configUIServer{
			Host:     "127.0.0.1",
			Port:     9090,
			TimeZone: "Asia/Shanghai",
			Language: "zh",
		},
		CloudAccounts: []configUICloudAccount{
			{
				ID:              "prod",
				Provider:        "aliyun",
				Aliases:         []string{"prod"},
				AccessKeyId:     "new-ak",
				AccessKeySecret: "new-sk",
				Endpoint:        "cms.cn-shanghai.aliyuncs.com",
			},
		},
		Auth: configUIAuth{
			Methods:      []string{"builtin"},
			JWTSecretKey: "new-secret",
			JWTExpiresIn: "48h",
			PasswordSalt: "new-salt",
			Local: &configUILocal{
				Users: []configUIUser{{Name: "admin", Password: "new-hash"}},
				Roles: []configUIRole{{Name: "admin", Users: []string{"admin"}}},
			},
		},
	}

	cfg, err := buildConfigFromUI(existing, req, configUIFieldPresence{
		Server:        true,
		CloudAccounts: true,
		Auth:          true,
	})
	if err != nil {
		t.Fatalf("buildConfigFromUI returned error: %v", err)
	}

	if cfg.Server.Port != 9090 || cfg.Server.Host != "127.0.0.1" {
		t.Fatalf("expected server settings to be updated, got %+v", cfg.Server)
	}
	if cfg.Global.Product != "cms" || cfg.Global.Workspace != "legacy-workspace" || cfg.Global.Region != "cn-shanghai" {
		t.Fatalf("expected legacy product context to be preserved, got %+v", cfg.Global)
	}
	if cfg.Global.AccessKeyId != "" || cfg.Global.AccessKeySecret != "" || cfg.Global.Endpoint != "" {
		t.Fatalf("expected legacy credentials to be cleared after writing cloudAccounts, got %+v", cfg.Global)
	}
	if len(cfg.CloudAccounts) != 1 || cfg.CloudAccounts[0].ID != "prod" {
		t.Fatalf("expected cloudAccounts to be rebuilt, got %+v", cfg.CloudAccounts)
	}
	if cfg.Auth.LDAP == nil || cfg.Auth.LDAP.Host != "ldap.example.com" {
		t.Fatalf("expected LDAP config to be preserved, got %+v", cfg.Auth.LDAP)
	}
	if cfg.Auth.OIDC == nil || cfg.Auth.OIDC.IssuerURL != "https://issuer.example.com" {
		t.Fatalf("expected OIDC config to be preserved, got %+v", cfg.Auth.OIDC)
	}
	if len(cfg.Auth.BuiltinUsers) != 1 || cfg.Auth.BuiltinUsers[0].Name != "admin" {
		t.Fatalf("expected builtin users to be updated, got %+v", cfg.Auth.BuiltinUsers)
	}
	if cfg.Auth.JWT.SecretKey != "new-secret" || cfg.Auth.JWT.ExpiresIn != "48h" {
		t.Fatalf("expected JWT settings to be updated, got %+v", cfg.Auth.JWT)
	}
}

func TestBuildConfigFromUIPreservesSectionsWhenFieldAbsent(t *testing.T) {
	existing := &config.Config{
		Global: config.GlobalConfig{
			AccessKeyId:     "legacy-ak",
			AccessKeySecret: "legacy-sk",
			Endpoint:        "cms.cn-hangzhou.aliyuncs.com",
		},
		Channels: &config.ChannelsConfig{
			DingTalk: []config.DingTalkConfig{
				{
					ClientId:     "dt-app",
					ClientSecret: "dt-secret",
					EmployeeName: "assistant-prod",
				},
			},
		},
		OpenAI: &config.OpenAICompatConfig{
			Enabled: true,
			APIKeys: []string{"sk-existing"},
		},
	}

	req := configUIResponse{
		Server: configUIServer{
			Host:     "0.0.0.0",
			Port:     8088,
			TimeZone: "Asia/Shanghai",
			Language: "zh",
		},
	}

	cfg, err := buildConfigFromUI(existing, req, configUIFieldPresence{
		Server: true,
	})
	if err != nil {
		t.Fatalf("buildConfigFromUI returned error: %v", err)
	}

	if cfg.Channels == nil || len(cfg.Channels.DingTalk) != 1 || cfg.Channels.DingTalk[0].EmployeeName != "assistant-prod" {
		t.Fatalf("expected existing dingtalk config to be preserved, got %+v", cfg.Channels)
	}
	if cfg.OpenAI == nil || len(cfg.OpenAI.APIKeys) != 1 || cfg.OpenAI.APIKeys[0] != "sk-existing" {
		t.Fatalf("expected existing openai config to be preserved, got %+v", cfg.OpenAI)
	}
	if cfg.Global.AccessKeyId != "legacy-ak" {
		t.Fatalf("expected legacy credentials to stay untouched when cloudAccounts field is absent, got %+v", cfg.Global)
	}
}

func TestBuildConfigFromUIClearsChannelWhenFieldPresentEmpty(t *testing.T) {
	existing := &config.Config{
		Channels: &config.ChannelsConfig{
			DingTalk: []config.DingTalkConfig{
				{
					ClientId:     "dt-app",
					ClientSecret: "dt-secret",
					EmployeeName: "assistant-prod",
				},
			},
		},
	}

	cfg, err := buildConfigFromUI(existing, configUIResponse{}, configUIFieldPresence{
		DingTalk: true,
	})
	if err != nil {
		t.Fatalf("buildConfigFromUI returned error: %v", err)
	}

	if cfg.Channels != nil {
		t.Fatalf("expected empty dingtalk section to clear channels, got %+v", cfg.Channels)
	}
}
