package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	userClient "github.com/sponge-dad/feed/app/user/rpc/userClient"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetMeLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetMeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetMeLogic {
	return &GetMeLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetMeLogic) GetMe() (*types.UserDetail, error) {
	meID := middleware.MustGetUserID(l.ctx)
	if meID == 0 {
		return nil, nil
	}

	userResp, err := l.svcCtx.UserRpc.GetUser(l.ctx, &userClient.GetUserReq{
		UserId: meID,
	})
	if err != nil {
		return nil, err
	}

	return userInfoToDetail(l.svcCtx, userResp.User), nil
}
