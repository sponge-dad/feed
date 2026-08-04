// collectFeedLogic.go
//
// 职责：收藏帖子。流程与点赞同构（Redis 先行 + MQ 异步落库），
// 详见 docs/design/interaction/03-collect.md 与 internal/logic/interactionHelper.go。
package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

type CollectFeedLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCollectFeedLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CollectFeedLogic {
	return &CollectFeedLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// CollectFeed 收藏帖子。重复收藏幂等返回成功且不重复计数、不重复发事件。
func (l *CollectFeedLogic) CollectFeed(in *interaction.CollectFeedReq) (*interaction.CollectFeedResp, error) {
	if in.GetUserId() <= 0 || in.GetFeedId() <= 0 {
		return nil, errorx.New(errorx.ParamError)
	}
	h := newInteractHelper(l.ctx, l.svcCtx, kindCollect)
	added, err := h.add(in.GetUserId(), in.GetFeedId())
	if err != nil {
		l.Errorf("CollectFeed: redis-first write failed user=%d feed=%d err=%v",
			in.GetUserId(), in.GetFeedId(), err)
		return nil, errorx.New(errorx.ServerError)
	}
	if added {
		h.publish(in.GetUserId(), in.GetFeedId(), h.addAction())
	}
	return &interaction.CollectFeedResp{}, nil
}
