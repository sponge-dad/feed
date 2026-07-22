package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type BatchGetUserInteractionStatusLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewBatchGetUserInteractionStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BatchGetUserInteractionStatusLogic {
	return &BatchGetUserInteractionStatusLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *BatchGetUserInteractionStatusLogic) BatchGetUserInteractionStatus(in *interaction.BatchGetUserInteractionStatusReq) (*interaction.BatchGetUserInteractionStatusResp, error) {
	// todo: add your logic here and delete this line

	return &interaction.BatchGetUserInteractionStatusResp{}, nil
}
