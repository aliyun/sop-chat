package api

import (
	"log"
	"net/http"

	"sop-chat/pkg/sopchat"

	"github.com/gin-gonic/gin"
)

// handleListEmployees 列出所有数字员工
func (s *Server) handleListEmployees(c *gin.Context) {
	client, err := s.createClient()
	if err != nil {
		log.Printf("Failed to create client: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "Failed to create client",
			"detail": err.Error(),
		})
		return
	}

	employees, err := client.ListEmployees()
	if err != nil {
		log.Printf("Failed to list employees: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "Failed to list employees",
			"detail": err.Error(),
		})
		return
	}

	// 转换为简化的响应格式
	result := make([]gin.H, 0, len(employees))
	for _, emp := range employees {
		item := gin.H{}
		if emp.Name != nil {
			item["name"] = *emp.Name
		}
		if emp.DisplayName != nil {
			item["displayName"] = *emp.DisplayName
		}
		if emp.Description != nil {
			item["description"] = *emp.Description
		}
		result = append(result, item)
	}

	c.JSON(http.StatusOK, gin.H{
		"employees": result,
	})
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

	client, err := s.createClient()
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

	log.Printf("✅ Employee created successfully: %s", req.Name)
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

	log.Printf("✅ Employee updated successfully: %s", name)
	c.JSON(http.StatusOK, result)
}
