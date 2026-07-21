package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	userClient "github.com/sponge-dad/feed/app/user/rpc/userClient"

	"github.com/zeromicro/go-zero/core/logx"
)

type UpdateMeLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateMeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateMeLogic {
	return &UpdateMeLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateMeLogic) UpdateMe(req *types.UpdateUserReq) (*types.UpdateUserResp, error) {
	meID := middleware.MustGetUserID(l.ctx)
	if meID == 0 {
		return nil, nil
	}

	rpcResp, err := l.svcCtx.UserRpc.UpdateUser(l.ctx, &userClient.UpdateUserReq{
		UserId:   meID,
		Nickname: req.Nickname,
		Avatar:   req.Avatar,
		Bio:      req.Bio,
		CityCode: req.CityCode,
		CityName: req.CityName,
	})
	if err != nil {
		return nil, err
	}

	return &types.UpdateUserResp{
		User: *userInfoToUser(rpcResp.User),
	}, nil
}
