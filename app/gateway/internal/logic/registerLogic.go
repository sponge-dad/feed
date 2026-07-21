package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	userClient "github.com/sponge-dad/feed/app/user/rpc/userClient"

	"github.com/zeromicro/go-zero/core/logx"
)

type RegisterLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRegisterLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RegisterLogic {
	return &RegisterLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RegisterLogic) Register(req *types.RegisterReq) (*types.RegisterResp, error) {
	rpcResp, err := l.svcCtx.UserRpc.Register(l.ctx, &userClient.RegisterReq{
		Username: req.Username,
		Password: req.Password,
		Nickname: req.Nickname,
	})
	if err != nil {
		return nil, err
	}

	return &types.RegisterResp{
		User:  *userInfoToUser(rpcResp.User),
		Token: rpcResp.Token,
	}, nil
}
