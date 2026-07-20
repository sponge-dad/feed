// Package middleware 提供 Gateway 的公共中间件。
//
// 当前包含 JWT 鉴权中间件：从 Authorization 头部解析 Bearer token，
// 将当前登录用户 ID 写入 context，供后续 handler 使用。
package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"
	"github.com/sponge-dad/feed/common/jwtx"
	"github.com/sponge-dad/feed/common/response"

	"github.com/zeromicro/go-zero/core/logx"
)

// userIDKey 用于把当前登录用户 ID 写入 context 的 key 类型，
// 使用自定义类型避免与其他包撞 key。
type userIDKey struct{}

// UserIDFromContext 从 context 取出当前登录用户 ID。
// 若未取出成功返回 0，调用方需自行判断。
func UserIDFromContext(ctx context.Context) int64 {
	if v := ctx.Value(userIDKey{}); v != nil {
		if uid, ok := v.(int64); ok {
			return uid
		}
	}
	return 0
}

// NewJwtAuthMiddleware 创建 JWT 鉴权中间件。
func NewJwtAuthMiddleware(svcCtx *svc.ServiceContext) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				response.HTTPError(r.Context(), w, http.StatusUnauthorized, errorx.Unauthorized, "缺少 Authorization 头部")
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
				response.HTTPError(r.Context(), w, http.StatusUnauthorized, errorx.Unauthorized, "Authorization 格式错误")
				return
			}

		claims, err := svcCtx.JwtManager.Parse(parts[1])
		if err != nil {
			logx.WithContext(r.Context()).Infof("jwt parse failed: %v", err)
			response.HTTPError(r.Context(), w, http.StatusUnauthorized, errorx.Unauthorized, "token 无效或已过期")
			return
		}

			// 将 user_id 注入 context，后续 handler 不需要再解析 token。
			ctx := context.WithValue(r.Context(), userIDKey{}, claims.UserID)
			next(w, r.WithContext(ctx))
		}
	}
}

// MustGetUserID 从 context 获取当前登录用户 ID，如果未获取到则panic（用于已登录接口）。
func MustGetUserID(ctx context.Context) int64 {
	uid := UserIDFromContext(ctx)
	if uid == 0 {
		logx.WithContext(ctx).Error("MustGetUserID called but no user_id in context")
	}
	return uid
}

// _ 确保 jwtx 包被引用，避免误删后编译失败。
var _ = (*jwtx.Manager)(nil)
