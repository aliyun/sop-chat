package api

import (
	"log"
	"net/http"
	"sort"
	"strings"

	"sop-chat/internal/config"
	"sop-chat/pkg/sopchat"

	cmsclient "github.com/alibabacloud-go/cms-20240330/v6/client"
	"github.com/gin-gonic/gin"
)

type employeeListItem struct {
	Name           string `json:"name"`
	DisplayName    string `json:"displayName,omitempty"`
	Description    string `json:"description,omitempty"`
	CloudAccountID string `json:"cloudAccountId"`
	Product        string `json:"product,omitempty"`
	EmployeeType   string `json:"employeeType,omitempty"`
}

type configuredEmployeeRef struct {
	CloudAccountID string
	EmployeeName   string
	Product        string
	Project        string
	Workspace      string
	Region         string
}

func employeeProductFromType(employeeType string) string {
	normalized := strings.TrimSpace(strings.ToLower(employeeType))
	switch {
	case normalized == "":
		return ""
	case strings.Contains(normalized, "sls"), strings.Contains(normalized, "sop"):
		return "sls"
	case strings.Contains(normalized, "cms"):
		return "cms"
	default:
		return ""
	}
}

func employeeListItemFromSDK(cloudAccountID string, emp *cmsclient.ListDigitalEmployeesResponseBodyDigitalEmployees) employeeListItem {
	item := employeeListItem{
		CloudAccountID: cloudAccountID,
	}
	if emp == nil {
		return item
	}
	if emp.Name != nil {
		item.Name = *emp.Name
	}
	if emp.DisplayName != nil {
		item.DisplayName = *emp.DisplayName
	}
	if emp.Description != nil {
		item.Description = *emp.Description
	}
	if emp.EmployeeType != nil {
		item.EmployeeType = *emp.EmployeeType
		item.Product = employeeProductFromType(*emp.EmployeeType)
	}
	return item
}

func employeeListItemFromDetail(cloudAccountID, productHint string, body *cmsclient.GetDigitalEmployeeResponseBody) employeeListItem {
	item := employeeListItem{
		CloudAccountID: cloudAccountID,
		Product:        strings.TrimSpace(strings.ToLower(productHint)),
	}
	if body == nil {
		return item
	}
	if body.Name != nil {
		item.Name = *body.Name
	}
	if body.DisplayName != nil {
		item.DisplayName = *body.DisplayName
	}
	if body.Description != nil {
		item.Description = *body.Description
	}
	if body.EmployeeType != nil {
		item.EmployeeType = *body.EmployeeType
		if item.Product == "" {
			item.Product = employeeProductFromType(*body.EmployeeType)
		}
	}
	return item
}

func collectConfiguredEmployeeRefs(globalCfg *config.Config, requestedCloudAccountID string) []configuredEmployeeRef {
	if globalCfg == nil || globalCfg.Channels == nil {
		return nil
	}

	targetAccountID := ""
	if strings.TrimSpace(requestedCloudAccountID) != "" {
		targetAccountID = config.NormalizeCloudAccountID(requestedCloudAccountID)
	}

	result := make([]configuredEmployeeRef, 0)
	seen := make(map[string]struct{})
	legacyDefaults := globalCfg.GetLegacyProductContext()
	addRoutes := func(base config.ProductContext, routes []config.CloudAccountRoute) {
		for _, route := range routes {
			cloudAccountID := config.NormalizeCloudAccountID(route.CloudAccountID)
			employeeName := strings.TrimSpace(route.EmployeeName)
			if cloudAccountID == "" || employeeName == "" {
				continue
			}
			if targetAccountID != "" && cloudAccountID != targetAccountID {
				continue
			}
			key := cloudAccountID + "\x00" + employeeName
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			ctx := config.MergeProductContext(base, route.Product, route.Project, route.Workspace, route.Region)
			result = append(result, configuredEmployeeRef{
				CloudAccountID: cloudAccountID,
				EmployeeName:   employeeName,
				Product:        ctx.Product,
				Project:        ctx.Project,
				Workspace:      ctx.Workspace,
				Region:         ctx.Region,
			})
		}
	}

	for _, dt := range globalCfg.Channels.DingTalk {
		addRoutes(config.MergeProductContext(legacyDefaults, dt.Product, dt.Project, dt.Workspace, dt.Region), dt.CloudAccountRoutes)
	}
	for _, ft := range globalCfg.Channels.Feishu {
		addRoutes(config.MergeProductContext(legacyDefaults, ft.Product, ft.Project, ft.Workspace, ft.Region), ft.CloudAccountRoutes)
	}
	for _, wc := range globalCfg.Channels.WeCom {
		addRoutes(config.MergeProductContext(legacyDefaults, wc.Product, wc.Project, wc.Workspace, wc.Region), wc.CloudAccountRoutes)
	}
	for _, wb := range globalCfg.Channels.WeComBot {
		addRoutes(config.MergeProductContext(legacyDefaults, wb.Product, wb.Project, wb.Workspace, wb.Region), wb.CloudAccountRoutes)
	}

	return result
}

func findConfiguredEmployeeRef(globalCfg *config.Config, employeeName, requestedCloudAccountID string) *configuredEmployeeRef {
	normalizedName := strings.TrimSpace(employeeName)
	if normalizedName == "" {
		return nil
	}

	refs := collectConfiguredEmployeeRefs(globalCfg, requestedCloudAccountID)
	for i := range refs {
		if refs[i].EmployeeName == normalizedName {
			return &refs[i]
		}
	}
	if strings.TrimSpace(requestedCloudAccountID) == "" {
		return nil
	}
	refs = collectConfiguredEmployeeRefs(globalCfg, "")
	for i := range refs {
		if refs[i].EmployeeName == normalizedName &&
			refs[i].CloudAccountID == config.NormalizeCloudAccountID(requestedCloudAccountID) {
			return &refs[i]
		}
	}
	return nil
}

func findUniqueConfiguredEmployeeRefByName(globalCfg *config.Config, employeeName string) (*configuredEmployeeRef, bool) {
	normalizedName := strings.TrimSpace(employeeName)
	if normalizedName == "" {
		return nil, false
	}

	refs := collectConfiguredEmployeeRefs(globalCfg, "")
	var match *configuredEmployeeRef
	for i := range refs {
		if refs[i].EmployeeName != normalizedName {
			continue
		}
		if match != nil && match.CloudAccountID != refs[i].CloudAccountID {
			return nil, false
		}
		match = &refs[i]
	}
	return match, match != nil
}

// handleListEmployees 列出配置中允许在前台展示的数字员工。
// 当前仅展示 cloudAccountRoutes 中显式绑定过的员工，避免把账号下所有员工都暴露到登录后首页。
func (s *Server) handleListEmployees(c *gin.Context) {
	requestedCloudAccountID := strings.TrimSpace(c.Query("cloudAccountId"))
	namePrefix := strings.TrimSpace(c.Query("namePrefix"))

	s.mu.RLock()
	globalCfg := s.globalConfig
	s.mu.RUnlock()

	refs := collectConfiguredEmployeeRefs(globalCfg, requestedCloudAccountID)
	if len(refs) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"employees": []employeeListItem{},
		})
		return
	}

	items := make([]employeeListItem, 0)
	warnings := make([]string, 0)
	clients := make(map[string]*sopchat.Client)

	for _, ref := range refs {
		client, ok := clients[ref.CloudAccountID]
		if !ok {
			var err error
			client, err = s.createClientForCloudAccount(ref.CloudAccountID)
			if err != nil {
				log.Printf("Failed to create client for cloud account %q: %v", ref.CloudAccountID, err)
				warnings = append(warnings, ref.CloudAccountID+": "+err.Error())
				continue
			}
			clients[ref.CloudAccountID] = client
		}

		response, err := client.GetEmployee(ref.EmployeeName)
		if err != nil {
			log.Printf("Failed to get employee %q for cloud account %q: %v", ref.EmployeeName, ref.CloudAccountID, err)
			warnings = append(warnings, ref.CloudAccountID+"/"+ref.EmployeeName+": "+err.Error())
			continue
		}
		if response == nil || response.Body == nil {
			warnings = append(warnings, ref.CloudAccountID+"/"+ref.EmployeeName+": empty employee response")
			continue
		}

		item := employeeListItemFromDetail(ref.CloudAccountID, ref.Product, response.Body)
		if item.Name == "" {
			continue
		}
		if namePrefix != "" && !strings.HasPrefix(item.Name, namePrefix) {
			continue
		}
		items = append(items, item)
	}

	if len(items) == 0 && len(warnings) > 0 {
		log.Printf("Configured employee list is empty: %s", strings.Join(warnings, " | "))
	}

	sort.SliceStable(items, func(i, j int) bool {
		leftName := strings.ToLower(strings.TrimSpace(items[i].DisplayName))
		if leftName == "" {
			leftName = strings.ToLower(items[i].Name)
		}
		rightName := strings.ToLower(strings.TrimSpace(items[j].DisplayName))
		if rightName == "" {
			rightName = strings.ToLower(items[j].Name)
		}
		if leftName != rightName {
			return leftName < rightName
		}
		if items[i].CloudAccountID != items[j].CloudAccountID {
			return items[i].CloudAccountID < items[j].CloudAccountID
		}
		return items[i].Name < items[j].Name
	})

	resp := gin.H{
		"employees": items,
	}
	if len(warnings) > 0 {
		resp["warnings"] = warnings
	}
	c.JSON(http.StatusOK, resp)
}

// handleGetEmployee 获取指定员工的详细信息
func (s *Server) handleGetEmployee(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Employee name cannot be empty",
		})
		return
	}

	requestedCloudAccountID := strings.TrimSpace(c.Query("cloudAccountId"))

	client, err := s.createClientForCloudAccount(requestedCloudAccountID)
	if err != nil {
		log.Printf("Failed to create client: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "Failed to create client",
			"detail": err.Error(),
		})
		return
	}

	response, err := client.GetEmployee(name)
	if err != nil {
		log.Printf("Failed to get employee info: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "Failed to get employee info",
			"detail": err.Error(),
		})
		return
	}

	if response.Body == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Employee not found",
		})
		return
	}

	body := response.Body
	result := gin.H{}
	if body.Name != nil {
		result["name"] = *body.Name
	}
	if body.DisplayName != nil {
		result["displayName"] = *body.DisplayName
	}
	if body.Description != nil {
		result["description"] = *body.Description
	}
	if body.DefaultRule != nil {
		result["defaultRule"] = *body.DefaultRule
	}
	if body.RoleArn != nil {
		result["roleArn"] = *body.RoleArn
	}
	if body.EmployeeType != nil {
		result["employeeType"] = *body.EmployeeType
	}
	if body.RegionId != nil {
		result["regionId"] = *body.RegionId
	}
	if body.CreateTime != nil {
		result["createTime"] = *body.CreateTime
	}
	if body.UpdateTime != nil {
		result["updateTime"] = *body.UpdateTime
	}
	if body.Knowledges != nil {
		result["knowledges"] = body.Knowledges
	}
	s.mu.RLock()
	globalCfg := s.globalConfig
	s.mu.RUnlock()
	if globalCfg != nil {
		if resolved, err := globalCfg.ResolveClientConfig(requestedCloudAccountID); err == nil && resolved != nil {
			result["cloudAccountId"] = resolved.CloudAccountID
		}
		if ref := findConfiguredEmployeeRef(globalCfg, name, requestedCloudAccountID); ref != nil {
			result["product"] = ref.Product
			result["project"] = ref.Project
			result["workspace"] = ref.Workspace
			result["region"] = ref.Region
		}
	}

	c.JSON(http.StatusOK, result)
}

// handleCreateEmployee 创建数字员工
func (s *Server) handleCreateEmployee(c *gin.Context) {
	var req struct {
		Name          string                   `json:"name" binding:"required"`
		DisplayName   string                   `json:"displayName" binding:"required"`
		Description   string                   `json:"description"`
		DefaultRule   string                   `json:"defaultRule" binding:"required"`
		RoleArn       string                   `json:"roleArn" binding:"required"`
		SopKnowledges []map[string]interface{} `json:"sopKnowledges"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  "Request parameter error",
			"detail": err.Error(),
		})
		return
	}

	requestedCloudAccountID := strings.TrimSpace(c.Query("cloudAccountId"))

	client, err := s.createClientForCloudAccount(requestedCloudAccountID)
	if err != nil {
		log.Printf("Failed to create client: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "Failed to create client",
			"detail": err.Error(),
		})
		return
	}

	config := &sopchat.EmployeeConfig{
		Name:          req.Name,
		DisplayName:   req.DisplayName,
		Description:   req.Description,
		DefaultRule:   req.DefaultRule,
		RoleArn:       req.RoleArn,
		SopKnowledges: req.SopKnowledges,
	}

	response, err := client.CreateEmployee(config)
	if err != nil {
		log.Printf("Failed to create employee: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "Failed to create employee",
			"detail": err.Error(),
		})
		return
	}

	result := gin.H{
		"success": true,
	}
	if response.Body != nil && response.Body.RequestId != nil {
		result["requestId"] = *response.Body.RequestId
	}

	log.Printf("Employee created successfully: %s", req.Name)
	c.JSON(http.StatusCreated, result)
}

// handleUpdateEmployee 更新数字员工
func (s *Server) handleUpdateEmployee(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Employee name cannot be empty",
		})
		return
	}

	var req struct {
		DisplayName   string                   `json:"displayName"`
		Description   string                   `json:"description"`
		DefaultRule   string                   `json:"defaultRule"`
		RoleArn       string                   `json:"roleArn" binding:"required"` // RoleArn 是必需的，即使不修改
		SopKnowledges []map[string]interface{} `json:"sopKnowledges"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  "Request parameter error",
			"detail": err.Error(),
		})
		return
	}

	client, err := s.createClient()
	if err != nil {
		log.Printf("Failed to create client: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "Failed to create client",
			"detail": err.Error(),
		})
		return
	}

	config := &sopchat.EmployeeConfig{
		Name:          name,
		DisplayName:   req.DisplayName,
		Description:   req.Description,
		DefaultRule:   req.DefaultRule,
		RoleArn:       req.RoleArn,
		SopKnowledges: req.SopKnowledges,
	}

	response, err := client.UpdateEmployee(config)
	if err != nil {
		log.Printf("Failed to update employee: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "Failed to update employee",
			"detail": err.Error(),
		})
		return
	}

	result := gin.H{
		"success": true,
	}
	if response.Body != nil && response.Body.RequestId != nil {
		result["requestId"] = *response.Body.RequestId
	}

	log.Printf("Employee updated successfully: %s", name)
	c.JSON(http.StatusOK, result)
}
