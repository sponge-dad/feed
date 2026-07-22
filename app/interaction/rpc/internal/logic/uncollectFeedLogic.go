package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type UncollectFeedLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUncollectFeedLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UncollectFeedLogic {
	return &UncollectFeedLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *UncollectFeedLogic) UncollectFeed(in *interaction.UncollectFeedReq) (*interaction.UncollectFeedResp, error) {
	// todo: add your logic here and delete this line

	return &interaction.UncollectFeedResp{}, nil
}
