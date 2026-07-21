package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	userClient "github.com/sponge-dad/feed/app/user/rpc/userClient"

	"github.com/zeromicro/go-zero/core/logx"
)

type LoginLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLoginLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LoginLogic {
	return &LoginLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *LoginLogic) Login(req *types.LoginReq) (*types.LoginResp, error) {
	rpcResp, err := l.svcCtx.UserRpc.Login(l.ctx, &userClient.LoginReq{
		Username: req.Username,
		Password: req.Password,
	})
	if err != nil {
		return nil, err
	}

	return &types.LoginResp{
		User:  *userInfoToUser(rpcResp.User),
		Token: rpcResp.Token,
	}, nil
}
