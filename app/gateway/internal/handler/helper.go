package handler

import (
	"context"
	"net/http"

	"github.com/sponge-dad/feed/common/errorx"
	"github.com/sponge-dad/feed/common/response"
)

// writeError 统一处理 RPC 或 Logic 返回的错误。
// 若是业务错误（errorx.CodeError 或可从 gRPC error 还原的 CodeError）则透传
// code/message，否则返回服务器内部错误。
func writeError(ctx context.Context, w http.ResponseWriter, err error) {
	if codeErr, ok := errorx.TryParse(err); ok {
		response.Error(ctx, w, codeErr.Code, codeErr.Message)
		return
	}
	response.Error(ctx, w, errorx.ServerError, "服务器内部错误")
}
