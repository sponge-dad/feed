// auth_handler_test.go
//
// Gateway JWT 鉴权链路测试。对应基线 U-LOG-04（HTTP 401 空体）、U-ME-03
// （claim 类型兼容）与 R-P1-*（user_id 精度）。
//
// 说明：使用 httptest 启动最小路由。受保护路由复用 go-zero 的
// rest/handler.Authorize —— 这正是 routes.go 中 rest.WithJwt 在框架内部
// 实际挂载的同一个中间件，因此对 401 行为的断言与真实网关一致。
// register/login 按 routes.go 的分组方式注册在无 JWT 分组。
package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/logx"
	gzhandler "github.com/zeromicro/go-zero/rest/handler"

	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/common/jwtx"
)

// gwTestSecret 测试专用 Secret，与任何环境配置无关。
const gwTestSecret = "gateway-auth-unit-test-secret"

func init() {
	// 关闭 go-zero 中间件对 401 请求的错误日志输出，避免测试日志噪音。
	logx.Disable()
}

// authProbeResp 受保护接口回显的最小响应体。
type authProbeResp struct {
	Code   int   `json:"code"`
	UserID int64 `json:"user_id"`
}

// newAuthTestServer 构造最小网关路由：
//   - /api/v1/users/me       挂 go-zero JWT 中间件（同 rest.WithJwt），回显 ctx 中 user_id；
//   - /api/v1/users/register 与 /api/v1/users/login 无 JWT，模拟公开路由分组。
func newAuthTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	authorize := gzhandler.Authorize(gwTestSecret)
	protected := authorize(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证 user_id 能从 JWT claim 经 context 传递到 Handler/Logic
		uid := middleware.MustGetUserID(r.Context())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"code":0,"user_id":%d}`, uid)
	}))

	public := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"code":0}`)
	})

	mux := http.NewServeMux()
	mux.Handle("/api/v1/users/me", protected)
	mux.Handle("/api/v1/users/register", public)
	mux.Handle("/api/v1/users/login", public)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// doGet 携带可选 Authorization 头请求 path，返回状态码与响应体。
func doGet(t *testing.T, srv *httptest.Server, path, authHeader string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	require.NoError(t, err)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, body
}

// signToken 用指定 secret 与 claims 签发 HS256 token（测试自定义 claim 场景用）。
func signToken(t *testing.T, secret string, claims jwt.MapClaims) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	require.NoError(t, err)
	return token
}

// Baseline: U-LOG-04（JWT 中间件拦截：HTTP 401 且空响应体，不走统一业务响应结构）
func TestAuthMiddleware_InvalidTokens_Return401WithEmptyBody(t *testing.T) {
	srv := newAuthTestServer(t)
	validExp := time.Now().Add(time.Hour).Unix()

	cases := []struct {
		name       string
		authHeader string
	}{
		{"未携带Authorization头", ""},
		{"Authorization格式错误", "NotBearer garbage"},
		{"伪造的随机Token", "Bearer this.is.forged"},
		{"错误Secret签名", "Bearer " + signToken(t, "wrong-secret",
			jwt.MapClaims{"user_id": "10001", "exp": validExp})},
		{"Token已过期", "Bearer " + signToken(t, gwTestSecret,
			jwt.MapClaims{"user_id": "10001", "exp": time.Now().Add(-time.Hour).Unix()})},
		{"nbf尚未生效", "Bearer " + signToken(t, gwTestSecret,
			jwt.MapClaims{"user_id": "10001", "exp": validExp, "nbf": time.Now().Add(time.Hour).Unix()})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := doGet(t, srv, "/api/v1/users/me", tc.authHeader)
			assert.Equal(t, http.StatusUnauthorized, status, "JWT 中间件必须返回 HTTP 401")
			assert.Empty(t, body, "401 响应不经过统一业务响应结构，响应体应为空")
		})
	}
}

// Baseline: U-ME-01 / U-ME-03（合法 Token：user_id 从 claim 经 context 传递到 Handler）
func TestAuthMiddleware_ValidToken_PropagatesUserIDToHandler(t *testing.T) {
	srv := newAuthTestServer(t)

	// 使用项目自身的 jwtx.Manager 签发（user_id 为字符串 claim）
	manager := jwtx.NewManager(gwTestSecret, 1)
	const uid = int64(1953558574948356097) // 超过 float64 53 位精度的 Snowflake ID
	token, err := manager.Generate(uid, "alice")
	require.NoError(t, err)

	status, body := doGet(t, srv, "/api/v1/users/me", "Bearer "+token)
	assert.Equal(t, http.StatusOK, status)

	var got authProbeResp
	require.NoError(t, json.Unmarshal(body, &got))
	assert.Equal(t, 0, got.Code)
	assert.Equal(t, uid, got.UserID, "字符串形式的 user_id claim 必须无精度损失地传递到 Handler")
}

// Baseline: U-ME-03（claim 类型兼容矩阵：string/number 可解析，缺失/异常类型返回 0——行为基线）
func TestAuthMiddleware_UserIDClaimTypes_CurrentBehaviorBaseline(t *testing.T) {
	srv := newAuthTestServer(t)
	validExp := time.Now().Add(time.Hour).Unix()

	cases := []struct {
		name    string
		claims  jwt.MapClaims
		wantUID int64
	}{
		{"字符串user_id正常解析", jwt.MapClaims{"user_id": "10001", "exp": validExp}, 10001},
		// 行为基线（与基线文档 U-ME-03 "float64 可解析" 预期不一致）：
		// go-zero JWT 中间件以 json.Number 解码数字 claim，而
		// middleware.UserIDFromContext 的类型断言仅覆盖 string/float64/int64/int，
		// json.Number 不匹配任何分支 → 返回 0。已记录到实施报告。
		{"数字user_id当前解析为0（json.Number未兼容，行为基线）", jwt.MapClaims{"user_id": 10002, "exp": validExp}, 0},
		{"缺少user_id时返回0（中间件不校验claim存在性，行为基线）", jwt.MapClaims{"username": "x", "exp": validExp}, 0},
		{"user_id类型异常(bool)返回0（行为基线）", jwt.MapClaims{"user_id": true, "exp": validExp}, 0},
		{"user_id为非数字字符串返回0（行为基线）", jwt.MapClaims{"user_id": "abc", "exp": validExp}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token := signToken(t, gwTestSecret, tc.claims)
			status, body := doGet(t, srv, "/api/v1/users/me", "Bearer "+token)

			// 签名合法的 Token 一律放行（HTTP 200），user_id 解析交给业务层
			require.Equal(t, http.StatusOK, status,
				"签名合法即通过中间件；user_id 缺失/类型异常不在中间件层拦截（行为基线）")
			var got authProbeResp
			require.NoError(t, json.Unmarshal(body, &got))
			assert.Equal(t, tc.wantUID, got.UserID)
		})
	}
}

// Baseline: U-REG-06 派生（公开路由分组：register/login 无 Token 仍可访问）
func TestPublicRoutes_WithoutToken_StillAccessible(t *testing.T) {
	srv := newAuthTestServer(t)
	for _, path := range []string{"/api/v1/users/register", "/api/v1/users/login"} {
		t.Run(path, func(t *testing.T) {
			status, body := doGet(t, srv, path, "")
			assert.Equal(t, http.StatusOK, status, "公开路由不得被 JWT 中间件拦截")
			assert.JSONEq(t, `{"code":0}`, string(body))
		})
	}
}

// Baseline: U-LOG-04 派生（受保护接口无 Token → 401；与业务错误的 HTTP 200 + code 区分）
func TestProtectedRoute_NoToken_Returns401NotBusinessCode(t *testing.T) {
	srv := newAuthTestServer(t)

	status, body := doGet(t, srv, "/api/v1/users/me", "")
	assert.Equal(t, http.StatusUnauthorized, status)
	// 401 由中间件直接写出，不包含统一响应结构的 code 字段
	assert.NotContains(t, string(body), `"code"`)
}
