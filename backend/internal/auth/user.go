package auth

import (
	"time"
)

// UserStore 用户存储接口
type UserStore interface {
	CreateUser(username, password, email string) error
	GetUser(username string) (*StoredUser, error)
	UpdateUser(username string, updates map[string]interface{}) error
	DeleteUser(username string) error
	ListUsers() ([]*StoredUser, error)
	ValidatePassword(username, password string) (bool, error)
}

// StoredUser 存储的用户信息
type StoredUser struct {
	Username     string   `json:"username"`
	PasswordHash string   `json:"passwordHash"`
	Email        string   `json:"email"`
	Roles        []string `json:"roles"`
	CreatedAt    string   `json:"createdAt"`
	UpdatedAt    string   `json:"updatedAt"`
}

// getCurrentTime 获取当前时间字符串
func getCurrentTime() string {
	return time.Now().Format(time.RFC3339)
}
