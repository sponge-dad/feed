package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"

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

func (l *CollectFeedLogic) CollectFeed(in *interaction.CollectFeedReq) (*interaction.CollectFeedResp, error) {
	// todo: add your logic here and delete this line

	return &interaction.CollectFeedResp{}, nil
}
