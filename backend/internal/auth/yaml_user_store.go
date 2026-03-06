package auth

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"sync"
)

// YAMLUserStore 基于 YAML 配置的用户存储
type YAMLUserStore struct {
	config *YAMLConfig
	users  map[string]*StoredUser
	mu     sync.RWMutex
}

// NewYAMLUserStore 创建基于 YAML 配置的用户存储
func NewYAMLUserStore(configPath string) (*YAMLUserStore, error) {
	// 加载 YAML 配置
	yamlConfig, actualPath, err := LoadYAMLConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("加载 YAML 配置失败: %w", err)
	}

	log.Printf("📄 加载 YAML 用户配置: %s", actualPath)

	store := &YAMLUserStore{
		config: yamlConfig,
		users:  make(map[string]*StoredUser),
	}

	// 从 YAML 配置加载用户
	if err := store.loadFromConfig(); err != nil {
		return nil, fmt.Errorf("从配置加载用户失败: %w", err)
	}

	log.Printf("✅ 已从 YAML 配置加载 %d 个用户", len(store.users))
	return store, nil
}

// NewYAMLUserStoreFromConfig 从 YAMLConfig 直接创建用户存储（用于统一配置）
func NewYAMLUserStoreFromConfig(yamlConfig *YAMLConfig) (*YAMLUserStore, error) {
	store := &YAMLUserStore{
		config: yamlConfig,
		users:  make(map[string]*StoredUser),
	}

	// 从 YAML 配置加载用户
	if err := store.loadFromConfig(); err != nil {
		return nil, fmt.Errorf("从配置加载用户失败: %w", err)
	}

	log.Printf("✅ 已从统一配置加载 %d 个用户", len(store.users))
	return store, nil
}

// loadFromConfig 从 YAML 配置加载用户
func (s *YAMLUserStore) loadFromConfig() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.config.Local == nil {
		return fmt.Errorf("本地认证配置不存在")
	}

	// 创建角色到用户的映射
	roleToUsers := make(map[string][]string)
	for _, role := range s.config.Local.Roles {
		roleToUsers[role.Name] = role.Users
	}

	// 加载用户
	for _, userConfig := range s.config.Local.Users {
		// 确定用户的角色列表
		rolesMap := make(map[string]bool)

		// 遍历所有角色配置，查找包含该用户的角色
		for roleName, roleUsers := range roleToUsers {
			for _, roleUser := range roleUsers {
				if roleUser == userConfig.Name {
					rolesMap[roleName] = true
					break
				}
			}
		}

		// 转换为角色列表
		roles := make([]string, 0, len(rolesMap))
		for role := range rolesMap {
			roles = append(roles, role)
		}

		// 如果没有角色，默认添加 "user" 角色
		if len(roles) == 0 {
			roles = []string{"user"}
		}

		// 创建存储的用户对象
		storedUser := &StoredUser{
			Username:     userConfig.Name,
			PasswordHash: userConfig.Password, // 存储 MD5 哈希
			Email:        fmt.Sprintf("%s@localhost", userConfig.Name),
			Roles:        roles,
			CreatedAt:    getCurrentTime(),
			UpdatedAt:    getCurrentTime(),
		}

		s.users[userConfig.Name] = storedUser
		log.Printf("  - 加载用户: %s (角色: %v)", userConfig.Name, roles)
	}

	return nil
}

// CreateUser 创建用户（YAML 存储不支持动态创建，返回错误）
func (s *YAMLUserStore) CreateUser(username, password, email string) error {
	return fmt.Errorf("YAML 配置存储不支持动态创建用户，请修改 auth.yaml 配置文件")
}

// GetUser 获取用户
func (s *YAMLUserStore) GetUser(username string) (*StoredUser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	user, exists := s.users[username]
	if !exists {
		return nil, fmt.Errorf("用户 %s 不存在", username)
	}

	// 返回副本，避免并发修改
	userCopy := *user
	return &userCopy, nil
}

// UpdateUser 更新用户信息（YAML 存储不支持动态更新，返回错误）
func (s *YAMLUserStore) UpdateUser(username string, updates map[string]interface{}) error {
	return fmt.Errorf("YAML 配置存储不支持动态更新用户，请修改 auth.yaml 配置文件")
}

// DeleteUser 删除用户（YAML 存储不支持动态删除，返回错误）
func (s *YAMLUserStore) DeleteUser(username string) error {
	return fmt.Errorf("YAML 配置存储不支持动态删除用户，请修改 auth.yaml 配置文件")
}

// ListUsers 列出所有用户
func (s *YAMLUserStore) ListUsers() ([]*StoredUser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	users := make([]*StoredUser, 0, len(s.users))
	for _, user := range s.users {
		userCopy := *user
		users = append(users, &userCopy)
	}

	return users, nil
}

// ReloadFromYAMLConfig 从新的 YAMLConfig 重新加载用户（支持热更新，无需重启）
func (s *YAMLUserStore) ReloadFromYAMLConfig(yamlConfig *YAMLConfig) error {
	if yamlConfig == nil || yamlConfig.Local == nil {
		return fmt.Errorf("本地认证配置不存在")
	}

	// 在临时 store 中预加载新用户，验证配置合法性
	tempStore := &YAMLUserStore{
		config: yamlConfig,
		users:  make(map[string]*StoredUser),
	}
	if err := tempStore.loadFromConfig(); err != nil {
		return fmt.Errorf("重新加载用户失败: %w", err)
	}

	// 原子地替换配置和用户映射
	s.mu.Lock()
	s.config = yamlConfig
	s.users = tempStore.users
	s.mu.Unlock()

	log.Printf("✅ 热更新完成，已加载 %d 个用户", len(tempStore.users))
	return nil
}

// ValidatePassword 验证密码
// 哈希公式：MD5(passwordSalt + plaintext)，salt 为空时退化为 MD5(plaintext)
func (s *YAMLUserStore) ValidatePassword(username, password string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	user, exists := s.users[username]
	if !exists {
		return false, fmt.Errorf("用户 %s 不存在", username)
	}

	// 计算输入密码的 MD5 哈希（含 salt）
	salt := ""
	if s.config.Local != nil {
		salt = s.config.Local.PasswordSalt
	}
	hash := md5.Sum([]byte(salt + password))
	inputHash := strings.ToLower(hex.EncodeToString(hash[:]))
	storedHash := strings.ToLower(user.PasswordHash)

	return inputHash == storedHash, nil
}
