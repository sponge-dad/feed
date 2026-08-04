package handler

import (
	"context"
	"net/http"

	"github.com/sponge-dad/feed/common/response"
)

// writeError 统一处理 RPC 或 Logic 返回的错误。
// 实际逻辑收敛到 response.ErrorFrom：业务错误透传 code/message，
// 其它错误按服务器内部错误返回。
func writeError(ctx context.Context, w http.ResponseWriter, err error) {
	response.ErrorFrom(ctx, w, err)
}
