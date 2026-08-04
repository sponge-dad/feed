// uncollectFeedLogic.go
//
// 职责：取消收藏。流程与取消点赞同构（Redis 先行 + MQ 异步落库），
// 详见 docs/design/interaction/03-collect.md。
package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

type UncollectFeedLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUncollectFeedLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UncollectFeedLogic {
	return &UncollectFeedLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// UncollectFeed 取消收藏。未收藏时幂等返回成功，不产生计数变化、不发事件。
func (l *UncollectFeedLogic) UncollectFeed(in *interaction.UncollectFeedReq) (*interaction.UncollectFeedResp, error) {
	if in.GetUserId() <= 0 || in.GetFeedId() <= 0 {
		return nil, errorx.New(errorx.ParamError)
	}
	h := newInteractHelper(l.ctx, l.svcCtx, kindCollect)
	removed, err := h.remove(in.GetUserId(), in.GetFeedId())
	if err != nil {
		l.Errorf("UncollectFeed: redis-first write failed user=%d feed=%d err=%v",
			in.GetUserId(), in.GetFeedId(), err)
		return nil, errorx.New(errorx.ServerError)
	}
	if removed {
		h.publish(in.GetUserId(), in.GetFeedId(), h.removeAction())
	}
	return &interaction.UncollectFeedResp{}, nil
}
