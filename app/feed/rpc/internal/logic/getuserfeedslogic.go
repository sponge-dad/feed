package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetUserFeedsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetUserFeedsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserFeedsLogic {
	return &GetUserFeedsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 个人主页帖子列表
func (l *GetUserFeedsLogic) GetUserFeeds(in *feed.GetUserFeedsReq) (*feed.GetUserFeedsResp, error) {
	// todo: add your logic here and delete this line

	return &feed.GetUserFeedsResp{}, nil
}
