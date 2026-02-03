package api

import (
	"log"
	"net/http"

	"sop-chat/pkg/sopchat"

	"github.com/gin-gonic/gin"
)

// handleGetAccountId 获取当前阿里云账号ID
func (s *Server) handleGetAccountId(c *gin.Context) {
	accountId, err := sopchat.GetAccountId(s.config.AccessKeyId, s.config.AccessKeySecret)
	if err != nil {
		log.Printf("Failed to get account ID: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "Failed to get account ID",
			"detail": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"accountId": accountId,
	})
}

// handleGetSystemConfig 获取系统配置（语言、时区等）
func (s *Server) handleGetSystemConfig(c *gin.Context) {
	language := "zh"
	timeZone := "Asia/Shanghai"
	
	if s.globalConfig != nil {
		language = s.globalConfig.GetLanguage()
		timeZone = s.globalConfig.GetTimeZone()
	}
	
	c.JSON(http.StatusOK, gin.H{
		"language": language,
		"timeZone": timeZone,
	})
}
