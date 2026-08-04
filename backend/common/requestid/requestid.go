// Package requestid 统一管理请求标识在 context 中的存取。
//
// request_id 由 Gateway 中间件生成并写入 context（见 ../../docs/design/agent/02-request-trace.md），
// 经 gRPC metadata 透传下游，并最终作为响应字段返回给客户端，
// 用于把一次请求在全链路日志与 Trace 中串起来。
package requestid

import "context"

// ctxKey 是 request_id 在 context 中的私有 key 类型。
// 用自定义类型而非裸字符串，避免与其它包的 context key 冲突。
type ctxKey struct{}

// WithRequestID 将 request_id 写入 context。
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// FromContext 从 context 取出 request_id；未设置时返回空串。
func FromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKey{}).(string); ok {
		return v
	}
	return ""
}
