package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetRecommendTimelineLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetRecommendTimelineLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetRecommendTimelineLogic {
	return &GetRecommendTimelineLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 推荐流
func (l *GetRecommendTimelineLogic) GetRecommendTimeline(in *feed.GetRecommendTimelineReq) (*feed.GetRecommendTimelineResp, error) {
	// todo: add your logic here and delete this line

	return &feed.GetRecommendTimelineResp{}, nil
}
