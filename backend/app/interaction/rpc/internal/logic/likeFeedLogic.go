// likeFeedLogic.go
//
// 职责：点赞帖子。Redis 先行更新（Set/ZSet/Hash），成功后异步发送 MQ 事件落库，
// 详见 docs/design/interaction/02-like.md 与 internal/logic/interactionHelper.go。
package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

type LikeFeedLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewLikeFeedLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LikeFeedLogic {
	return &LikeFeedLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// LikeFeed 点赞帖子。重复点赞幂等返回成功且不重复计数、不重复发事件。
// 帖子存在性由上游（Gateway/Feed 服务）保证，本服务不做跨服务校验。
func (l *LikeFeedLogic) LikeFeed(in *interaction.LikeFeedReq) (*interaction.LikeFeedResp, error) {
	if in.GetUserId() <= 0 || in.GetFeedId() <= 0 {
		return nil, errorx.New(errorx.ParamError)
	}
	h := newInteractHelper(l.ctx, l.svcCtx, kindLike)
	added, err := h.add(in.GetUserId(), in.GetFeedId())
	if err != nil {
		l.Errorf("LikeFeed: redis-first write failed user=%d feed=%d err=%v",
			in.GetUserId(), in.GetFeedId(), err)
		return nil, errorx.New(errorx.ServerError)
	}
	if added {
		h.publish(in.GetUserId(), in.GetFeedId(), h.addAction())
	}
	return &interaction.LikeFeedResp{}, nil
}
