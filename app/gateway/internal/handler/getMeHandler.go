// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package handler

import (
	"net/http"

	"github.com/sponge-dad/feed/app/gateway/internal/logic"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/common/response"
)

func getMeHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := logic.NewGetMeLogic(r.Context(), svcCtx)
		resp, err := l.GetMe()
		if err != nil {
			writeError(r.Context(), w, err)
		} else {
			response.Success(r.Context(), w, resp)
		}
	}
}
