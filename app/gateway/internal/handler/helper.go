// helper.go
//
// 职责：Gateway handler 层共享工具函数。
package handler

import (
	"context"
	"net/http"

	"github.com/sponge-dad/feed/app/gateway/internal/types"
	userClient "github.com/sponge-dad/feed/app/user/rpc/userClient"
	"github.com/sponge-dad/feed/common/errorx"
	"github.com/sponge-dad/feed/common/response"
)

// userInfoToUser 将 user.proto 的 UserInfo 转换为 HTTP 层对外展示的类型。
func userInfoToUser(info *userClient.UserInfo) *types.User {
	if info == nil {
		return nil
	}
	return &types.User{
		ID:       info.Id,
		Username: info.Username,
		Nickname: info.Nickname,
		Avatar:   info.Avatar,
		Bio:      info.Bio,
		CityName: info.CityName,
	}
}

// writeError 统一处理 RPC 返回的错误，如果是业务错误则透传 code/message，否则返回服务器错误。
func writeError(ctx context.Context, w http.ResponseWriter, err error) {
	if codeErr, ok := err.(*errorx.CodeError); ok {
		response.Error(ctx, w, codeErr.Code, codeErr.Message)
		return
	}
	response.Error(ctx, w, errorx.ServerError, "服务器内部错误")
}
