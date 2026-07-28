// getUserInteractionStatusLogic.go
//
// 职责：查询当前用户对单条帖子的点赞/收藏状态。
// Set 未命中时回源 MySQL 全量重建（详见 docs/design/interaction/04-stats.md §3.2）。
package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetUserInteractionStatusLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetUserInteractionStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserInteractionStatusLogic {
	return &GetUserInteractionStatusLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetUserInteractionStatus 查询用户对帖子的互动状态（is_liked / is_collected）。
func (l *GetUserInteractionStatusLogic) GetUserInteractionStatus(in *interaction.GetUserInteractionStatusReq) (*interaction.GetUserInteractionStatusResp, error) {
	if in.GetUserId() <= 0 || in.GetFeedId() <= 0 {
		return nil, errorx.New(errorx.ParamError)
	}
	liked, err := newInteractHelper(l.ctx, l.svcCtx, kindLike).isMember(in.GetUserId(), in.GetFeedId())
	if err != nil {
		l.Errorf("GetUserInteractionStatus: query like status failed user=%d feed=%d err=%v",
			in.GetUserId(), in.GetFeedId(), err)
		return nil, errorx.New(errorx.ServerError)
	}
	collected, err := newInteractHelper(l.ctx, l.svcCtx, kindCollect).isMember(in.GetUserId(), in.GetFeedId())
	if err != nil {
		l.Errorf("GetUserInteractionStatus: query collect status failed user=%d feed=%d err=%v",
			in.GetUserId(), in.GetFeedId(), err)
		return nil, errorx.New(errorx.ServerError)
	}
	return &interaction.GetUserInteractionStatusResp{
		Status: &interaction.UserInteractionStatus{
			FeedId:      in.GetFeedId(),
			IsLiked:     liked,
			IsCollected: collected,
		},
	}, nil
}
