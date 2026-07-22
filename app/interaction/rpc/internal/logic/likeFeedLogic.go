package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type LikeFeedLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewLikeFeedLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LikeFeedLogic {
	return &LikeFeedLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 动作类
func (l *LikeFeedLogic) LikeFeed(in *interaction.LikeFeedReq) (*interaction.LikeFeedResp, error) {
	// todo: add your logic here and delete this line

	return &interaction.LikeFeedResp{}, nil
}
