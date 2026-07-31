package logic

import (
	"context"
	"fmt"

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

	avatar := req.Avatar
	if avatar != "" {
		// 写入前校验：必须是本人已上传到 COS 的资源，并规整为可存储地址。
		canonical, _, eerr := CanonicalizeCosRef(l.svcCtx, avatar, meID)
		if eerr != nil {
			return nil, eerr
		}
		avatar = canonical
	}

	rpcResp, err := l.svcCtx.UserRpc.UpdateUser(l.ctx, &userClient.UpdateUserReq{
		UserId:   meID,
		Nickname: req.Nickname,
		Avatar:   avatar,
		Bio:      req.Bio,
		CityCode: req.CityCode,
		CityName: req.CityName,
	})
	if err != nil {
		return nil, err
	}

	user := userInfoToUser(l.svcCtx, rpcResp.User)
	if user == nil {
		return nil, fmt.Errorf("user info is nil after update")
	}
	return &types.UpdateUserResp{
		User: *user,
	}, nil
}
