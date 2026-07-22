package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetFeedStatsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetFeedStatsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetFeedStatsLogic {
	return &GetFeedStatsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 查询类
func (l *GetFeedStatsLogic) GetFeedStats(in *interaction.GetFeedStatsReq) (*interaction.GetFeedStatsResp, error) {
	// todo: add your logic here and delete this line

	return &interaction.GetFeedStatsResp{}, nil
}
