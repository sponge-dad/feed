// getUserLogic.go
//
// 职责：按 ID 查询单个用户详情。
//
// 关于缓存策略的重要说明：
// goctl 用 `-c` 参数生成的 UserModel.FindOne 内部已经自带 Cache-Aside 缓存
// （见 app/user/model/usersmodel_gen.go 的 QueryRowCtx 调用），
// 也就是说"查Redis未命中再查MySQL再回写"这一整套逻辑框架已经做掉了，
// 这里不需要再手动重复实现一层业务缓存，直接调用 UserModel.FindOne 即可。
package logic

import (
	"context"
	"errors"

	usermodel "github.com/sponge-dad/feed/app/user/model"
	"github.com/sponge-dad/feed/app/user/rpc/internal/svc"
	"github.com/sponge-dad/feed/app/user/rpc/user"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetUserLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetUserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserLogic {
	return &GetUserLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetUser 查询单个用户信息。
func (l *GetUserLogic) GetUser(in *user.GetUserReq) (*user.GetUserResp, error) {
	u, err := l.svcCtx.UserModel.FindOne(l.ctx, in.UserId)
	if errors.Is(err, usermodel.ErrNotFound) {
		return nil, errorx.New(errorx.UserNotFound)
	}
	if err != nil {
		return nil, err
	}

	return &user.GetUserResp{
		User: &user.UserInfo{
			Id:        u.Id,
			Username:  u.Username,
			Nickname:  u.Nickname,
			Avatar:    u.Avatar,
			Bio:       u.Bio,
			CityCode:  u.CityCode,
			CityName:  u.CityName,
			Status:    int32(u.Status),
			CreatedAt: u.CreatedAt.Unix(),
		},
	}, nil
}
