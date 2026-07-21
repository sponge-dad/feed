package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type BatchGetFeedsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewBatchGetFeedsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BatchGetFeedsLogic {
	return &BatchGetFeedsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 批量获取帖子详情
func (l *BatchGetFeedsLogic) BatchGetFeeds(in *feed.BatchGetFeedsReq) (*feed.BatchGetFeedsResp, error) {
	// todo: add your logic here and delete this line

	return &feed.BatchGetFeedsResp{}, nil
}
