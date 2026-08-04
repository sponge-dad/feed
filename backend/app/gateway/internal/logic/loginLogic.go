package logic

import (
	"context"
	"fmt"

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

	user := userInfoToUser(l.svcCtx, rpcResp.User)
	if user == nil {
		return nil, fmt.Errorf("user info is nil after login")
	}
	return &types.LoginResp{
		User:  *user,
		Token: rpcResp.Token,
	}, nil
}
