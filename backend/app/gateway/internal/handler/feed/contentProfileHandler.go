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

// ContentProfileHandler 查询内容画像（JWT 鉴权，分级返回）。
// 见 http-api.md #7；content-profile 仅供前端展示内容理解结果。
func ContentProfileHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.GetContentProfileReq
		if err := httpx.Parse(r, &req); err != nil {
			response.Error(r.Context(), w, errorx.ParamError, "请求参数解析失败")
			return
		}

		l := feed.NewContentProfileLogic(r.Context(), svcCtx)
		resp, err := l.ContentProfile(&req)
		if err != nil {
			response.ErrorFrom(r.Context(), w, err)
		} else {
			response.Success(r.Context(), w, resp)
		}
	}
}
