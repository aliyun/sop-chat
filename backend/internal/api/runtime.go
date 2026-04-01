package api

import (
	"fmt"
	"time"

	"sop-chat/internal/config"
)

type employeeRuntime struct {
	CloudAccountID string
	ClientConfig   *config.ClientConfig
	Context        config.ProductContext
}

func (s *Server) resolveEmployeeRuntime(employeeName, explicitCloudAccountID, message string) (*employeeRuntime, []string, error) {
	s.mu.RLock()
	globalCfg := s.globalConfig
	legacyCfg := s.config
	s.mu.RUnlock()

	if globalCfg != nil {
		clientCfg, options, err := resolveClientConfigForMessage(globalCfg, explicitCloudAccountID, message)
		if err != nil {
			return nil, options, err
		}

		ctx := globalCfg.GetLegacyProductContext()
		ref := findConfiguredEmployeeRef(globalCfg, employeeName, clientCfg.CloudAccountID)
		if ref == nil && explicitCloudAccountID == "" {
			if uniqueRef, ok := findUniqueConfiguredEmployeeRefByName(globalCfg, employeeName); ok {
				if uniqueRef.CloudAccountID != clientCfg.CloudAccountID {
					if switched, switchErr := globalCfg.ResolveClientConfig(uniqueRef.CloudAccountID); switchErr == nil {
						clientCfg = switched
					}
				}
				ref = uniqueRef
			}
		}
		if ref != nil {
			ctx = config.NewProductContext(ref.Product, ref.Project, ref.Workspace, ref.Region)
		}

		return &employeeRuntime{
			CloudAccountID: clientCfg.CloudAccountID,
			ClientConfig:   clientCfg,
			Context:        ctx,
		}, nil, nil
	}

	if legacyCfg != nil && legacyCfg.AccessKeyId != "" {
		return &employeeRuntime{
			CloudAccountID: config.NormalizeCloudAccountID(explicitCloudAccountID),
			ClientConfig: &config.ClientConfig{
				CloudAccountID:  config.NormalizeCloudAccountID(explicitCloudAccountID),
				AccessKeyId:     legacyCfg.AccessKeyId,
				AccessKeySecret: legacyCfg.AccessKeySecret,
				Endpoint:        legacyCfg.Endpoint,
			},
			Context: config.NewProductContext("", "", "", ""),
		}, nil, nil
	}

	return nil, nil, fmt.Errorf("credentials are not configured")
}

func buildEmployeeChatVariables(timeZone, language string, ctx config.ProductContext) map[string]interface{} {
	variables := map[string]interface{}{
		"timeStamp": fmt.Sprintf("%d", time.Now().Unix()),
		"timeZone":  timeZone,
		"language":  language,
	}

	if config.IsSlsProduct(ctx.Product) {
		variables["skill"] = "sop"
		if ctx.Project != "" {
			variables["project"] = ctx.Project
		}
		return variables
	}

	if ctx.Workspace != "" {
		variables["workspace"] = ctx.Workspace
	}
	if ctx.Region != "" {
		variables["region"] = ctx.Region
	}
	now := time.Now()
	variables["fromTime"] = now.Add(-15 * time.Minute).Unix()
	variables["toTime"] = now.Unix()
	return variables
}
