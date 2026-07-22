package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetUserLikedFeedsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetUserLikedFeedsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserLikedFeedsLogic {
	return &GetUserLikedFeedsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 列表类
func (l *GetUserLikedFeedsLogic) GetUserLikedFeeds(in *interaction.GetUserLikedFeedsReq) (*interaction.GetUserLikedFeedsResp, error) {
	// todo: add your logic here and delete this line

	return &interaction.GetUserLikedFeedsResp{}, nil
}
