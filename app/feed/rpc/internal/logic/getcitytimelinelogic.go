package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetCityTimelineLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetCityTimelineLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetCityTimelineLogic {
	return &GetCityTimelineLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 同城流
func (l *GetCityTimelineLogic) GetCityTimeline(in *feed.GetCityTimelineReq) (*feed.GetCityTimelineResp, error) {
	// todo: add your logic here and delete this line

	return &feed.GetCityTimelineResp{}, nil
}
