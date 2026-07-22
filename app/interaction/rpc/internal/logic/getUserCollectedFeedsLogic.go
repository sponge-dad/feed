package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetUserCollectedFeedsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetUserCollectedFeedsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserCollectedFeedsLogic {
	return &GetUserCollectedFeedsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetUserCollectedFeedsLogic) GetUserCollectedFeeds(in *interaction.GetUserCollectedFeedsReq) (*interaction.GetUserCollectedFeedsResp, error) {
	// todo: add your logic here and delete this line

	return &interaction.GetUserCollectedFeedsResp{}, nil
}
