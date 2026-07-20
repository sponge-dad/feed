// Package response 统一 HTTP 响应结构。
//
// 所有对外接口返回统一包裹：
//
//	{
//	  "code": 0,
//	  "message": "success",
//	  "data": {...},
//	  "request_id": "xxx"
//	}
//
// 约定见 ../../design/api-spec/README.md。
package response

import (
	"context"
	"net/http"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// Body 统一响应体
type Body struct {
	Code      int         `json:"code"`
	Message   string      `json:"message"`
	Data      interface{} `json:"data"`
	RequestID string      `json:"request_id"`
}

// requestIDKey 从 context 取 traceID / requestID 的 key。
// go-zero 默认把 trace id 放在 context 里，可通过 logx.WithContext 取到。
func requestID(ctx context.Context) string {
	// go-zero 会在 ctx 中注入 trace，这里取 traceID 作为 request_id
	traceID := logx.WithContext(ctx)
	_ = traceID
	// 简化处理：优先从 ctx 值获取自定义 request_id，没有则返回空
	if v := ctx.Value("request_id"); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// Success 返回成功响应
func Success(ctx context.Context, w http.ResponseWriter, data interface{}) {
	httpx.OkJsonCtx(ctx, w, &Body{
		Code:      0,
		Message:   "success",
		Data:      data,
		RequestID: requestID(ctx),
	})
}

// Error 返回业务错误响应（HTTP 状态码仍为 200，业务结果看 code）
func Error(ctx context.Context, w http.ResponseWriter, code int, message string) {
	httpx.OkJsonCtx(ctx, w, &Body{
		Code:      code,
		Message:   message,
		Data:      nil,
		RequestID: requestID(ctx),
	})
}

// HTTPError 返回带 HTTP 状态码的错误（如 401/403/500）
func HTTPError(ctx context.Context, w http.ResponseWriter, httpStatus, code int, message string) {
	w.Header().Set(httpx.ContentType, httpx.JsonContentType)
	w.WriteHeader(httpStatus)
	httpx.WriteJsonCtx(ctx, w, httpStatus, &Body{
		Code:      code,
		Message:   message,
		Data:      nil,
		RequestID: requestID(ctx),
	})
}
