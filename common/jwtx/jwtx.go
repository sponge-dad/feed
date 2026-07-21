// Package jwtx JWT 生成与解析工具。
//
// 网关登录态校验、用户身份透传均基于此。
// go-zero 自带 JWT 中间件，本包用于 User 服务签发 token 及需要手动解析的场景。
package jwtx

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims 自定义 JWT 载荷
type Claims struct {
	// UserID 在 JSON 中以字符串形式存储，避免 64 位整数经过 float64 时精度丢失。
	// go-zero 内置 JWT 中间件使用 jwt.MapClaims 解析，number 类型会转为 float64，
	// 而 Snowflake ID 超过 53 位有效数字，解析后精度会受损，导致 Gateway 取到的用户 ID 错误。
	UserID   int64  `json:"user_id,string"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// Manager JWT 管理器
type Manager struct {
	secret     []byte
	expireHour int
}

// NewManager 创建 JWT 管理器。secret 为签名密钥，expireHour 为过期小时数。
func NewManager(secret string, expireHour int) *Manager {
	return &Manager{
		secret:     []byte(secret),
		expireHour: expireHour,
	}
}

// Generate 为指定用户签发 token
func (m *Manager) Generate(userID int64, username string) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:   userID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(m.expireHour) * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(m.secret)
}

// Parse 解析并校验 token，返回 Claims
func (m *Manager) Parse(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return m.secret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}
