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

// ReportBehaviorsHandler 行为埋点批量上报（JWT 鉴权）。
// 见 docs/design/agent/03-behavior-event.md §2.1
func ReportBehaviorsHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ReportBehaviorsReq
		if err := httpx.Parse(r, &req); err != nil {
			response.Error(r.Context(), w, errorx.ParamError, "请求参数解析失败")
			return
		}

		ctx := feed.WithClientMeta(r.Context(), r)
		l := feed.NewReportBehaviorsLogic(ctx, svcCtx)
		resp, err := l.ReportBehaviors(&req)
		if err != nil {
			response.ErrorFrom(r.Context(), w, err)
		} else {
			response.Success(r.Context(), w, resp)
		}
	}
}
