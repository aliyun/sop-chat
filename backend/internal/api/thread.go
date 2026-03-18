package api

import (
	"log"
	"net/http"

	"sop-chat/internal/auth"
	"sop-chat/pkg/sopchat"

	"github.com/gin-gonic/gin"
)

// CreateThreadRequest 创建线程请求
type CreateThreadRequest struct {
	EmployeeName string                 `json:"employeeName" binding:"required"`
	Title        string                 `json:"title"`
	Attributes   map[string]interface{} `json:"attributes"`
}

// handleCreateThread 创建会话线程
func (s *Server) handleCreateThread(c *gin.Context) {
	var req CreateThreadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request parameters",
			"detail": err.Error(),
		})
		return
	}

	client, err := s.createClient()
	if err != nil {
		log.Printf("Failed to create client: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to create client",
			"detail": err.Error(),
		})
		return
	}

	attributes := req.Attributes
	if attributes == nil {
		attributes = make(map[string]interface{})
	}

	// 将登录用户名写入 user attribute，用于后续按用户过滤线程
	if user, exists := auth.GetUserFromContext(c); exists {
		attributes["user"] = user.Username
	}

	config := &sopchat.ThreadConfig{
		EmployeeName: req.EmployeeName,
		Title:        req.Title,
		Attributes:   attributes,
	}

	response, err := client.CreateThread(config)
	if err != nil {
		log.Printf("Failed to create thread: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to create thread",
			"detail": err.Error(),
		})
		return
	}

	if response.Body == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Create thread response is empty",
		})
		return
	}

	result := gin.H{}
	if response.Body.ThreadId != nil {
		result["threadId"] = *response.Body.ThreadId
	}
	if response.Body.RequestId != nil {
		result["requestId"] = *response.Body.RequestId
	}

	c.JSON(http.StatusOK, result)
}

// handleListThreads 列出员工的所有线程
func (s *Server) handleListThreads(c *gin.Context) {
	employeeName := c.Param("employeeName")
	if employeeName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Employee name cannot be empty",
		})
		return
	}

	client, err := s.createClient()
	if err != nil {
		log.Printf("Failed to create client: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to create client",
			"detail": err.Error(),
		})
		return
	}

	// 按登录用户的 user attribute 过滤线程
	var filters []sopchat.ThreadFilter
	if user, exists := auth.GetUserFromContext(c); exists {
		filters = append(filters, sopchat.ThreadFilter{
			Key:   "user",
			Value: user.Username,
		})
	}

	response, err := client.ListThreads(employeeName, filters)
	if err != nil {
		log.Printf("Failed to list threads: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to list threads",
			"detail": err.Error(),
		})
		return
	}

	if response.Body == nil || response.Body.Threads == nil {
		c.JSON(http.StatusOK, gin.H{
			"threads": []gin.H{},
		})
		return
	}

	threads := make([]gin.H, 0, len(response.Body.Threads))
	for _, thread := range response.Body.Threads {
		item := gin.H{}
		if thread.ThreadId != nil {
			item["threadId"] = *thread.ThreadId
		}
		if thread.Title != nil {
			item["title"] = *thread.Title
		}
		if thread.CreateTime != nil {
			item["createTime"] = *thread.CreateTime
		}
		if thread.Status != nil {
			item["status"] = *thread.Status
		}
		threads = append(threads, item)
	}

	c.JSON(http.StatusOK, gin.H{
		"threads": threads,
	})
}

// handleGetThread 获取线程详细信息
func (s *Server) handleGetThread(c *gin.Context) {
	employeeName := c.Param("employeeName")
	threadId := c.Param("threadId")

	if employeeName == "" || threadId == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Employee name and thread ID cannot be empty",
		})
		return
	}

	client, err := s.createClient()
	if err != nil {
		log.Printf("Failed to create client: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to create client",
			"detail": err.Error(),
		})
		return
	}

	response, err := client.GetThread(employeeName, threadId)
	if err != nil {
		log.Printf("Failed to get thread info: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to get thread info",
			"detail": err.Error(),
		})
		return
	}

	if response.Body == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Thread not found",
		})
		return
	}

	body := response.Body
	result := gin.H{}
	if body.ThreadId != nil {
		result["threadId"] = *body.ThreadId
	}
	if body.Title != nil {
		result["title"] = *body.Title
	}
	if body.CreateTime != nil {
		result["createTime"] = *body.CreateTime
	}
	if body.Status != nil {
		result["status"] = *body.Status
	}

	c.JSON(http.StatusOK, result)
}

// handleGetThreadMessages 获取线程的消息历史
func (s *Server) handleGetThreadMessages(c *gin.Context) {
	employeeName := c.Param("employeeName")
	threadId := c.Param("threadId")

	if employeeName == "" || threadId == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Employee name and thread ID cannot be empty",
		})
		return
	}

	client, err := s.createClient()
	if err != nil {
		log.Printf("Failed to create client: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to create client",
			"detail": err.Error(),
		})
		return
	}

	response, err := client.GetThreadData(employeeName, threadId)
	if err != nil {
		log.Printf("Failed to get thread messages: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to get thread messages",
			"detail": err.Error(),
		})
		return
	}

	if response.Body == nil || response.Body.Data == nil || len(response.Body.Data) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"messages": []gin.H{},
		})
		return
	}

	// 收集所有数据记录中的消息
	messages := make([]gin.H, 0)
	for _, data := range response.Body.Data {
		if data.Messages == nil {
			continue
		}
		for _, msg := range data.Messages {
			item := gin.H{}
			if msg.Role != nil {
				item["role"] = *msg.Role
			}
			if msg.Contents != nil && len(msg.Contents) > 0 {
				contents := make([]gin.H, 0, len(msg.Contents))
				for _, content := range msg.Contents {
					// content 是 map[string]interface{}
					contentItem := gin.H{}
					if contentType, ok := content["type"].(string); ok {
						contentItem["type"] = contentType
					}
					if contentValue, ok := content["value"].(string); ok {
						contentItem["value"] = contentValue
					}
					contents = append(contents, contentItem)
				}
				item["contents"] = contents
			}
			// 添加 Tools 字段（工具调用）
			if msg.Tools != nil && len(msg.Tools) > 0 {
				item["tools"] = msg.Tools
			}
			messages = append(messages, item)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"messages": messages,
	})
}

// handleGetSharedThread 获取分享的线程详细信息（公开访问，无需认证）
func (s *Server) handleGetSharedThread(c *gin.Context) {
	// 复用 handleGetThread 的逻辑，但不需要认证
	s.handleGetThread(c)
}

// handleGetSharedThreadMessages 获取分享的线程消息（公开访问，无需认证）
func (s *Server) handleGetSharedThreadMessages(c *gin.Context) {
	// 复用 handleGetThreadMessages 的逻辑，但不需要认证
	s.handleGetThreadMessages(c)
}

// handleGetSharedEmployee 获取分享的员工信息（公开访问，无需认证）
func (s *Server) handleGetSharedEmployee(c *gin.Context) {
	employeeName := c.Param("employeeName")
	// 如果路由是 /share/employee/:employeeName，参数名就是 employeeName
	if employeeName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Employee name cannot be empty",
		})
		return
	}

	client, err := s.createClient()
	if err != nil {
		log.Printf("Failed to create client: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to create client",
			"detail": err.Error(),
		})
		return
	}

	response, err := client.GetEmployee(employeeName)
	if err != nil {
		log.Printf("Failed to get employee info: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to get employee info",
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

	c.JSON(http.StatusOK, result)
}
