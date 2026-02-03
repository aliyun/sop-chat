package auth

import (
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWTClaims JWT 声明
type JWTClaims struct {
	Username string   `json:"username"`
	Email    string   `json:"email"`
	Roles    []string `json:"roles"`
	jwt.RegisteredClaims
}

// JWTManager JWT 管理器
type JWTManager struct {
	secretKey []byte
	expiresIn time.Duration
}

// NewJWTManager 创建 JWT 管理器
func NewJWTManager(secretKey string, expiresIn time.Duration) *JWTManager {
	if secretKey == "" {
		// 如果没有提供密钥，从环境变量读取或生成默认密钥
		secretKey = os.Getenv("JWT_SECRET_KEY")
		if secretKey == "" {
			secretKey = "default-secret-key-change-in-production"
		}
	}
	
	return &JWTManager{
		secretKey: []byte(secretKey),
		expiresIn: expiresIn,
	}
}

// GenerateToken 生成 JWT token
func (m *JWTManager) GenerateToken(user *User) (string, error) {
	claims := &JWTClaims{
		Username: user.Username,
		Email:    user.Email,
		Roles:    user.Roles,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(m.expiresIn)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}
	
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(m.secretKey)
}

// ValidateToken 验证 JWT token
func (m *JWTManager) ValidateToken(tokenString string) (*JWTClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		// 验证签名算法
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("意外的签名方法: %v", token.Header["alg"])
		}
		return m.secretKey, nil
	})
	
	if err != nil {
		return nil, err
	}
	
	if claims, ok := token.Claims.(*JWTClaims); ok && token.Valid {
		return claims, nil
	}
	
	return nil, fmt.Errorf("无效的 token")
}
