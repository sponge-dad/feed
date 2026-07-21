package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type CreateFeedLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateFeedLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateFeedLogic {
	return &CreateFeedLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 创建帖子
func (l *CreateFeedLogic) CreateFeed(in *feed.CreateFeedReq) (*feed.CreateFeedResp, error) {
	// todo: add your logic here and delete this line

	return &feed.CreateFeedResp{}, nil
}
