package api

import (
	"testing"

	"sop-chat/internal/config"
)

func TestResolveClientConfigForMessageSupportsAliasReference(t *testing.T) {
	globalCfg := &config.Config{
		Global: config.GlobalConfig{
			Product: "cms",
		},
		CloudAccounts: []config.CloudAccountConfig{
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

	resolved, options, err := resolveClientConfigForMessage(globalCfg, "subscription-uat", "帮我看一下 waf 告警")
	if err != nil {
		t.Fatalf("resolveClientConfigForMessage returned error: %v", err)
	}
	if len(options) != 0 {
		t.Fatalf("expected no confirmation options, got %v", options)
	}
	if resolved.CloudAccountID != "uat" {
		t.Fatalf("expected alias reference to resolve to uat, got %q", resolved.CloudAccountID)
	}
}

func TestCollectConfiguredEmployeeRefsOnlyUsesCloudAccountRoutes(t *testing.T) {
	globalCfg := &config.Config{
		Global: config.GlobalConfig{
			Product: "cms",
		},
		Channels: &config.ChannelsConfig{
			DingTalk: []config.DingTalkConfig{
				{
					EmployeeName: "should-not-be-listed",
					Project:      "default-project",
					CloudAccountRoutes: []config.CloudAccountRoute{
						{CloudAccountID: "prod", EmployeeName: "assistant-prod", Workspace: "workspace-prod", Region: "cn-shanghai"},
						{CloudAccountID: "uat", EmployeeName: "assistant-uat", Product: "sls"},
					},
				},
			},
			Feishu: []config.FeishuConfig{
				{
					EmployeeName: "also-should-not-be-listed",
					CloudAccountRoutes: []config.CloudAccountRoute{
						{CloudAccountID: "prod", EmployeeName: "assistant-prod", Product: "cms"},
					},
				},
			},
		},
	}

	refs := collectConfiguredEmployeeRefs(globalCfg, "")
	if len(refs) != 2 {
		t.Fatalf("expected 2 unique configured employee refs, got %d", len(refs))
	}
	if refs[0].CloudAccountID != "prod" || refs[0].EmployeeName != "assistant-prod" {
		t.Fatalf("unexpected first ref: %+v", refs[0])
	}
	if refs[0].Product != "cms" || refs[0].Workspace != "workspace-prod" {
		t.Fatalf("expected prod route to inherit effective cms context, got %+v", refs[0])
	}
	if refs[1].CloudAccountID != "uat" || refs[1].EmployeeName != "assistant-uat" {
		t.Fatalf("unexpected second ref: %+v", refs[1])
	}

	refs = collectConfiguredEmployeeRefs(globalCfg, "prod")
	if len(refs) != 1 {
		t.Fatalf("expected prod filter to keep 1 ref, got %d", len(refs))
	}
	if refs[0].CloudAccountID != "prod" || refs[0].EmployeeName != "assistant-prod" {
		t.Fatalf("unexpected filtered ref: %+v", refs[0])
	}
}

func TestFindUniqueConfiguredEmployeeRefByName(t *testing.T) {
	globalCfg := &config.Config{
		Channels: &config.ChannelsConfig{
			DingTalk: []config.DingTalkConfig{
				{
					CloudAccountRoutes: []config.CloudAccountRoute{
						{CloudAccountID: "uat", EmployeeName: "assistant-uat", Product: "sls"},
					},
				},
			},
		},
	}

	ref, ok := findUniqueConfiguredEmployeeRefByName(globalCfg, "assistant-uat")
	if !ok || ref == nil {
		t.Fatalf("expected unique configured employee ref to be found")
	}
	if ref.CloudAccountID != "uat" {
		t.Fatalf("expected uat cloud account, got %+v", ref)
	}
}
