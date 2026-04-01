package config

import (
	"strings"
	"testing"
)

func TestResolveClientConfigUsesFirstCloudAccountWhenDefaultMissing(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{
			AccessKeyId:     "global-ak",
			AccessKeySecret: "global-sk",
			Endpoint:        "cms.cn-hangzhou.aliyuncs.com",
		},
		CloudAccounts: []CloudAccountConfig{
			{
				ID:              "prod",
				AccessKeyId:     "prod-ak",
				AccessKeySecret: "prod-sk",
				Endpoint:        "cms.cn-shanghai.aliyuncs.com",
			},
			{
				ID:              "uat",
				AccessKeyId:     "uat-ak",
				AccessKeySecret: "uat-sk",
				Endpoint:        "cms.cn-beijing.aliyuncs.com",
			},
		},
	}

	resolved, err := cfg.ResolveClientConfig(DefaultCloudAccountID)
	if err != nil {
		t.Fatalf("ResolveClientConfig(default) returned error: %v", err)
	}

	if resolved.CloudAccountID != "prod" {
		t.Fatalf("expected first cloud account to be used, got %q", resolved.CloudAccountID)
	}
	if resolved.AccessKeyId != "prod-ak" {
		t.Fatalf("expected prod access key, got %q", resolved.AccessKeyId)
	}
	if resolved.AccessKeySecret != "prod-sk" {
		t.Fatalf("expected prod access secret, got %q", resolved.AccessKeySecret)
	}
	if resolved.Endpoint != "cms.cn-shanghai.aliyuncs.com" {
		t.Fatalf("expected prod endpoint, got %q", resolved.Endpoint)
	}
}

func TestResolveMessageCloudAccountIDMatchesAlias(t *testing.T) {
	cfg := &Config{
		CloudAccounts: []CloudAccountConfig{
			{
				ID:              "prod",
				Aliases:         []string{"prod", "生产环境", "subscription-prod"},
				AccessKeyId:     "prod-ak",
				AccessKeySecret: "prod-sk",
				Endpoint:        "cms.cn-shanghai.aliyuncs.com",
			},
			{
				ID:              "uat",
				Aliases:         []string{"uat", "测试环境", "subscription-uat"},
				AccessKeyId:     "uat-ak",
				AccessKeySecret: "uat-sk",
				Endpoint:        "cms.cn-shanghai.aliyuncs.com",
			},
		},
	}

	accountID, matched, ambiguous := cfg.ResolveMessageCloudAccountID("帮我看下 subscription-uat 的 WAF 告警", "prod")
	if !matched {
		t.Fatalf("expected alias to be matched")
	}
	if len(ambiguous) != 0 {
		t.Fatalf("expected no ambiguous results, got %v", ambiguous)
	}
	if accountID != "uat" {
		t.Fatalf("expected uat to be selected, got %q", accountID)
	}
}

func TestMatchCloudAccountIDsByTextDoesNotMatchInsideWords(t *testing.T) {
	cfg := &Config{
		CloudAccounts: []CloudAccountConfig{
			{
				ID:              "prod",
				Aliases:         []string{"prod"},
				AccessKeyId:     "prod-ak",
				AccessKeySecret: "prod-sk",
				Endpoint:        "cms.cn-shanghai.aliyuncs.com",
			},
		},
	}

	matches := cfg.MatchCloudAccountIDsByText("帮我看下 product 维度的监控", nil)
	if len(matches) != 0 {
		t.Fatalf("expected no matches for product text, got %v", matches)
	}

	matches = cfg.MatchCloudAccountIDsByText("帮我看下 prod 环境的监控", nil)
	if len(matches) != 1 || matches[0] != "prod" {
		t.Fatalf("expected prod to match as a standalone token, got %v", matches)
	}
}

func TestFindCloudAccountRouteMatchesNormalizedID(t *testing.T) {
	routes := []CloudAccountRoute{
		{
			CloudAccountID: "default",
			EmployeeName:   "employee-default",
		},
		{
			CloudAccountID: "uat",
			EmployeeName:   "employee-uat",
		},
	}

	route := FindCloudAccountRoute(routes, "uat")
	if route == nil {
		t.Fatalf("expected route to be found")
	}
	if route.EmployeeName != "employee-uat" {
		t.Fatalf("expected employee-uat, got %q", route.EmployeeName)
	}
}

func TestApplyCompatibilityDefaultsMigratesLegacyGlobal(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{
			Host:            "127.0.0.1",
			Port:            9090,
			TimeZone:        "Asia/Shanghai",
			Language:        "zh",
			AccessKeyId:     "legacy-ak",
			AccessKeySecret: "legacy-sk",
			Endpoint:        "cms.cn-shanghai.aliyuncs.com",
		},
	}

	cfg.applyCompatibilityDefaults()

	if cfg.Server.Host != "127.0.0.1" || cfg.Server.Port != 9090 {
		t.Fatalf("expected server settings to migrate from legacy global, got %+v", cfg.Server)
	}
	if len(cfg.CloudAccounts) != 1 {
		t.Fatalf("expected one migrated cloud account, got %d", len(cfg.CloudAccounts))
	}
	if cfg.CloudAccounts[0].ID != DefaultCloudAccountID {
		t.Fatalf("expected migrated default cloud account, got %q", cfg.CloudAccounts[0].ID)
	}
	if cfg.CloudAccounts[0].AccessKeyId != "legacy-ak" || cfg.CloudAccounts[0].Endpoint != "cms.cn-shanghai.aliyuncs.com" {
		t.Fatalf("unexpected migrated cloud account: %+v", cfg.CloudAccounts[0])
	}
}

func TestResolveProductUsesWorkspaceAndProjectHints(t *testing.T) {
	if got := ResolveProduct("", "", "workspace-a"); got != "cms" {
		t.Fatalf("expected workspace to imply cms, got %q", got)
	}
	if got := ResolveProduct("", "project-a", ""); got != "sls" {
		t.Fatalf("expected project to imply sls, got %q", got)
	}
	if got := ResolveProduct("cms", "", ""); got != "cms" {
		t.Fatalf("expected explicit cms to be kept, got %q", got)
	}
}

func TestApplyReplyStyleInstruction(t *testing.T) {
	full := ApplyReplyStyleInstruction("请分析今天的告警", false, "sls")
	if full == "请分析今天的告警" {
		t.Fatalf("expected full SOP instruction to be appended for sls when conciseReply=false")
	}
	if !strings.Contains(full, "SOP") {
		t.Fatalf("expected SOP guidance in full reply instruction, got %q", full)
	}

	concise := ApplyReplyStyleInstruction("请分析今天的告警", true, "sls")
	if !strings.Contains(concise, "简洁") {
		t.Fatalf("expected concise instruction to be appended, got %q", concise)
	}

	cms := ApplyReplyStyleInstruction("请分析今天的告警", false, "cms")
	if cms != "请分析今天的告警" {
		t.Fatalf("expected cms non-concise message to remain unchanged, got %q", cms)
	}
}
