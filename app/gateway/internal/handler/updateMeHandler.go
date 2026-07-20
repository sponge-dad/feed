// updateMeHandler.go
//
// 职责：更新当前登录用户信息。
// 流程：从 JWT 解析 user_id -> 解析 body -> 调用 User.UpdateUser RPC -> 返回最新用户信息。
package handler

import (
	"net/http"

	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	userClient "github.com/sponge-dad/feed/app/user/rpc/userClient"
	"github.com/sponge-dad/feed/common/errorx"
	"github.com/sponge-dad/feed/common/response"

	"github.com/zeromicro/go-zero/rest/httpx"
)

// UpdateMeHandler 处理 PATCH /api/v1/users/me。
func UpdateMeHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		meID := middleware.UserIDFromContext(r.Context())
		if meID == 0 {
			response.HTTPError(r.Context(), w, http.StatusUnauthorized, errorx.Unauthorized, "未登录")
			return
		}

		var req types.UpdateUserReq
		if err := httpx.ParseJsonBody(r, &req); err != nil {
			response.Error(r.Context(), w, errorx.ParamError, "请求参数解析失败")
			return
		}

		resp, err := svcCtx.UserClient.UpdateUser(r.Context(), &userClient.UpdateUserReq{
			UserId:   meID,
			Nickname: req.Nickname,
			Avatar:   req.Avatar,
			Bio:      req.Bio,
			CityCode: req.CityCode,
			CityName: req.CityName,
		})
		if err != nil {
			writeError(r.Context(), w, err)
			return
		}

		response.Success(r.Context(), w, &types.UpdateUserResp{
			User: userInfoToUser(resp.User),
		})
	}
}
