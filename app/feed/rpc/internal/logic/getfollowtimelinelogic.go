package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetFollowTimelineLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetFollowTimelineLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetFollowTimelineLogic {
	return &GetFollowTimelineLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 关注流
func (l *GetFollowTimelineLogic) GetFollowTimeline(in *feed.GetFollowTimelineReq) (*feed.GetFollowTimelineResp, error) {
	// todo: add your logic here and delete this line

	return &feed.GetFollowTimelineResp{}, nil
}
