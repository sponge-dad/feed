package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnlikeFeedLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUnlikeFeedLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnlikeFeedLogic {
	return &UnlikeFeedLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *UnlikeFeedLogic) UnlikeFeed(in *interaction.UnlikeFeedReq) (*interaction.UnlikeFeedResp, error) {
	// todo: add your logic here and delete this line

	return &interaction.UnlikeFeedResp{}, nil
}
