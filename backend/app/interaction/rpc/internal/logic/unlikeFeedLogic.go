// unlikeFeedLogic.go
//
// 职责：取消点赞。Redis 先行更新（含冷 key 先重建，防止取消丢失），
// 成功后异步发送 MQ 事件落库，详见 docs/design/interaction/02-like.md §2。
package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnlikeFeedLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUnlikeFeedLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnlikeFeedLogic {
	return &UnlikeFeedLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// UnlikeFeed 取消点赞。未点赞时幂等返回成功，不产生计数变化、不发事件。
func (l *UnlikeFeedLogic) UnlikeFeed(in *interaction.UnlikeFeedReq) (*interaction.UnlikeFeedResp, error) {
	if in.GetUserId() <= 0 || in.GetFeedId() <= 0 {
		return nil, errorx.New(errorx.ParamError)
	}
	h := newInteractHelper(l.ctx, l.svcCtx, kindLike)
	removed, err := h.remove(in.GetUserId(), in.GetFeedId())
	if err != nil {
		l.Errorf("UnlikeFeed: redis-first write failed user=%d feed=%d err=%v",
			in.GetUserId(), in.GetFeedId(), err)
		return nil, errorx.New(errorx.ServerError)
	}
	if removed {
		h.publish(in.GetUserId(), in.GetFeedId(), h.removeAction())
	}
	return &interaction.UnlikeFeedResp{}, nil
}
