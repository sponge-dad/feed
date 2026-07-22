package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type BatchGetFeedStatsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewBatchGetFeedStatsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BatchGetFeedStatsLogic {
	return &BatchGetFeedStatsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *BatchGetFeedStatsLogic) BatchGetFeedStats(in *interaction.BatchGetFeedStatsReq) (*interaction.BatchGetFeedStatsResp, error) {
	// todo: add your logic here and delete this line

	return &interaction.BatchGetFeedStatsResp{}, nil
}
