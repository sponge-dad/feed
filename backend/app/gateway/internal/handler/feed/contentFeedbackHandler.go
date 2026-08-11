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

// ContentProfileFeedbackHandler 创作者纠错反馈（JWT 鉴权，作者本人，只记录不改画像）。
// 见 http-api.md #8。
func ContentProfileFeedbackHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.SubmitProfileFeedbackReq
		if err := httpx.Parse(r, &req); err != nil {
			response.Error(r.Context(), w, errorx.ParamError, "请求参数解析失败")
			return
		}

		l := feed.NewContentFeedbackLogic(r.Context(), svcCtx)
		resp, err := l.SubmitProfileFeedback(&req)
		if err != nil {
			response.ErrorFrom(r.Context(), w, err)
		} else {
			response.Success(r.Context(), w, resp)
		}
	}
}
