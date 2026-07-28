// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package feed

import (
	"net/http"

	"github.com/sponge-dad/feed/app/gateway/internal/logic/feed"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	"github.com/sponge-dad/feed/common/errorx"
	"github.com/sponge-dad/feed/common/response"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func DeleteFeedHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.DeleteFeedReq
		if err := httpx.Parse(r, &req); err != nil {
			response.Error(r.Context(), w, errorx.ParamError, "请求参数解析失败")
			return
		}

		l := feed.NewDeleteFeedLogic(r.Context(), svcCtx)
		resp, err := l.DeleteFeed(&req)
		if err != nil {
			response.ErrorFrom(r.Context(), w, err)
		} else {
			response.Success(r.Context(), w, resp)
		}
	}
}
