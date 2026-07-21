package middleware

import (
	"context"
	"strconv"
)

// userIDKey 使用自定义类型作为 context key，避免与其他包撞 key。
type userIDKey struct{}

// UserIDFromContext 从 context 取出当前登录用户 ID。
// 优先读取本包注入的 key，其次读取 go-zero JWT 中间件注入的 "user_id" claim。
// 若未登录返回 0。
func UserIDFromContext(ctx context.Context) int64 {
	if v := ctx.Value(userIDKey{}); v != nil {
		if uid, ok := v.(int64); ok {
			return uid
		}
	}

	// go-zero 内置 JWT 中间件将 claim 以 claim 名作为 key 写入 context。
	// 本系统 token 的 claim 名为 "user_id"，为避免 Snowflake ID 精度丢失，
	// 签发时以字符串形式存储，因此这里需要兼容 string 和 number 两种类型。
	if v := ctx.Value("user_id"); v != nil {
		switch uid := v.(type) {
		case string:
			if id, err := strconv.ParseInt(uid, 10, 64); err == nil {
				return id
			}
		case float64:
			return int64(uid)
		case int64:
			return uid
		case int:
			return int64(uid)
		}
	}
	return 0
}

// WithUserID 将用户 ID 写入 context，供 Logic 层使用。
func WithUserID(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, userIDKey{}, userID)
}

// MustGetUserID 从 context 获取当前登录用户 ID。
// 调用方通常为已登录接口，context 中理论上一定有值。
func MustGetUserID(ctx context.Context) int64 {
	return UserIDFromContext(ctx)
}
