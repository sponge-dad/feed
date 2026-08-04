// Package jwtx 单元测试：覆盖签发、解析、过期、篡改、算法混淆与并发安全。
//
// 基线编号说明：docs/api-test-baseline.md 未为 common/jwtx 单独编号，
// 本文件按模块前缀补充 J-JWT-01 ~ J-JWT-10（详见 docs/test-implementation-report.md）。
// 关联基线：U-ME-02（过期 Token）、U-ME-03（伪造/篡改 Token）、R-P1-*（user_id 精度）。
package jwtx

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testSecret 测试专用密钥，禁止与任何环境配置一致。
const testSecret = "unit-test-only-secret-do-not-use"

func newTestManager(expireHour int) *Manager {
	return NewManager(testSecret, expireHour)
}

// Baseline: J-JWT-01
func TestGenerateAndParse_ValidToken_ReturnsCorrectClaims(t *testing.T) {
	m := newTestManager(24)
	before := time.Now()

	token, err := m.Generate(10001, "alice")
	require.NoError(t, err)
	require.NotEmpty(t, token)

	claims, err := m.Parse(token)
	require.NoError(t, err)

	assert.Equal(t, int64(10001), claims.UserID)
	assert.Equal(t, "alice", claims.Username)
	// 过期时间 = 签发时刻 + 24h（允许 5s 误差）
	assert.WithinDuration(t, before.Add(24*time.Hour), claims.ExpiresAt.Time, 5*time.Second)
	assert.WithinDuration(t, before, claims.IssuedAt.Time, 5*time.Second)
	assert.WithinDuration(t, before, claims.NotBefore.Time, 5*time.Second)
}

// Baseline: J-JWT-02（user_id 必须以字符串序列化，防止 Snowflake ID 精度丢失）
func TestGenerate_UserIDSerializedAsString_InPayload(t *testing.T) {
	m := newTestManager(1)
	const uid = int64(9223372036854775807)

	token, err := m.Generate(uid, "bob")
	require.NoError(t, err)

	parts := strings.Split(token, ".")
	require.Len(t, parts, 3)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(payload, &raw))

	s, ok := raw["user_id"].(string)
	require.True(t, ok, "user_id 应为 JSON string，实际类型 %T", raw["user_id"])
	assert.Equal(t, "9223372036854775807", s)
	assert.Equal(t, "bob", raw["username"])
}

// Baseline: J-JWT-03 / U-ME-03（错误 Secret 必须解析失败）
func TestParse_WrongSecret_Fails(t *testing.T) {
	token, err := newTestManager(1).Generate(1, "alice")
	require.NoError(t, err)

	other := NewManager("another-unit-test-secret", 1)
	claims, err := other.Parse(token)
	assert.Error(t, err)
	assert.Nil(t, claims)
	assert.ErrorIs(t, err, jwt.ErrTokenSignatureInvalid)
}

// Baseline: J-JWT-04 / U-ME-02（过期 Token 必须解析失败）
func TestParse_ExpiredToken_Fails(t *testing.T) {
	m := newTestManager(-1) // 过期时间为 1 小时前
	token, err := m.Generate(1, "alice")
	require.NoError(t, err)

	claims, err := m.Parse(token)
	assert.Nil(t, claims)
	assert.ErrorIs(t, err, jwt.ErrTokenExpired)
}

// Baseline: J-JWT-05 / U-ME-03（篡改 payload 必须解析失败）
func TestParse_TamperedToken_Fails(t *testing.T) {
	m := newTestManager(1)
	token, err := m.Generate(10001, "alice")
	require.NoError(t, err)

	parts := strings.Split(token, ".")
	require.Len(t, parts, 3)
	// 篡改 payload：把 user_id 改成别人的
	forged := base64.RawURLEncoding.EncodeToString(
		[]byte(`{"user_id":"999","username":"hacker","exp":` + fmt.Sprint(time.Now().Add(time.Hour).Unix()) + `}`))
	tampered := parts[0] + "." + forged + "." + parts[2]

	claims, err := m.Parse(tampered)
	assert.Nil(t, claims)
	assert.ErrorIs(t, err, jwt.ErrTokenSignatureInvalid)
}

// Baseline: J-JWT-06
func TestParse_EmptyToken_Fails(t *testing.T) {
	claims, err := newTestManager(1).Parse("")
	assert.Nil(t, claims)
	assert.Error(t, err)
}

// Baseline: J-JWT-07（alg=none 算法混淆攻击必须被拒绝）
func TestParse_NoneAlgorithm_Rejected(t *testing.T) {
	claims := Claims{
		UserID:   10001,
		Username: "alice",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	noneToken := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	tokenStr, err := noneToken.SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	got, err := newTestManager(1).Parse(tokenStr)
	assert.Nil(t, got)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected signing method")
}

// Baseline: J-JWT-08（nbf 尚未生效的 Token 必须解析失败）
func TestParse_NotBeforeInFuture_Fails(t *testing.T) {
	claims := Claims{
		UserID:   10001,
		Username: "alice",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(2 * time.Hour)),
			NotBefore: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString([]byte(testSecret))
	require.NoError(t, err)

	got, err := newTestManager(1).Parse(tokenStr)
	assert.Nil(t, got)
	assert.ErrorIs(t, err, jwt.ErrTokenNotValidYet)
}

// Baseline: J-JWT-09（边界 user_id 无精度损失）
func TestGenerateAndParse_BoundaryUserIDs_NoPrecisionLoss(t *testing.T) {
	m := newTestManager(1)
	cases := []struct {
		name string
		uid  int64
	}{
		{"最小合法用户ID", 1},
		{"典型Snowflake ID（超过53位精度）", 1953558574948356097},
		{"int64最大值", 9223372036854775807},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token, err := m.Generate(tc.uid, "u")
			require.NoError(t, err)
			claims, err := m.Parse(token)
			require.NoError(t, err)
			assert.Equal(t, tc.uid, claims.UserID)
		})
	}
}

// Baseline: J-JWT-10（并发签发与解析，用 go test -race 验证）
func TestGenerateAndParse_Concurrent_RaceFree(t *testing.T) {
	m := newTestManager(1)
	const workers = 32

	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(uid int64) {
			defer wg.Done()
			token, err := m.Generate(uid, fmt.Sprintf("user-%d", uid))
			if err != nil {
				errCh <- err
				return
			}
			claims, err := m.Parse(token)
			if err != nil {
				errCh <- err
				return
			}
			if claims.UserID != uid {
				errCh <- errors.New("claims user_id mismatch")
			}
		}(int64(i + 1))
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("并发签发/解析失败: %v", err)
	}
}
