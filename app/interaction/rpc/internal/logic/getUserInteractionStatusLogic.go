package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetUserInteractionStatusLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetUserInteractionStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserInteractionStatusLogic {
	return &GetUserInteractionStatusLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetUserInteractionStatusLogic) GetUserInteractionStatus(in *interaction.GetUserInteractionStatusReq) (*interaction.GetUserInteractionStatusResp, error) {
	// todo: add your logic here and delete this line

	return &interaction.GetUserInteractionStatusResp{}, nil
}
