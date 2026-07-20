// loginHandler.go
//
// 职责：处理用户登录 HTTP 请求。
// 流程：解析 body -> 调用 User.Login RPC -> 返回 user + token。
package handler

import (
	"net/http"

	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	userClient "github.com/sponge-dad/feed/app/user/rpc/userClient"
	"github.com/sponge-dad/feed/common/errorx"
	"github.com/sponge-dad/feed/common/response"

	"github.com/zeromicro/go-zero/rest/httpx"
)

// LoginHandler 处理 POST /api/v1/users/login。
func LoginHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.LoginReq
		if err := httpx.ParseJsonBody(r, &req); err != nil {
			response.Error(r.Context(), w, errorx.ParamError, "请求参数解析失败")
			return
		}

		if req.Username == "" || req.Password == "" {
			response.Error(r.Context(), w, errorx.ParamError, "用户名、密码不能为空")
			return
		}

		resp, err := svcCtx.UserClient.Login(r.Context(), &userClient.LoginReq{
			Username: req.Username,
			Password: req.Password,
		})
		if err != nil {
			writeError(r.Context(), w, err)
			return
		}

		response.Success(r.Context(), w, &types.LoginResp{
			User:  userInfoToUser(resp.User),
			Token: resp.Token,
		})
	}
}
