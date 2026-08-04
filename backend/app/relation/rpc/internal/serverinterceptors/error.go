// Package serverinterceptors 存放 RPC 服务端统一拦截器。
package serverinterceptors

import (
	"context"

	"github.com/sponge-dad/feed/common/errorx"

	"google.golang.org/grpc"
)

// ErrorInterceptor 统一将 logic 层的业务错误（errorx.CodeError）转换为 gRPC status error，
// 使得 Gateway 等调用方可以从 gRPC error 中还原原始业务码。
func ErrorInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler) (resp any, err error) {
	resp, err = handler(ctx, req)
	if err != nil {
		if codeErr, ok := errorx.TryParse(err); ok {
			return resp, errorx.ToGRPCError(codeErr)
		}
	}
	return resp, err
}
