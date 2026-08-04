// updateUserLogic.go
//
// 职责：更新用户资料（昵称/头像/简介/城市）。
//
// 关于缓存策略的说明：
// UserModel.Update 内部在执行 SQL 后会自动清理该记录在各唯一索引维度上的缓存 key
// （见 usersmodel_gen.go 的 Update 函数，传给 ExecCtx 的 usersXxxKey 参数），
// 所以这里不需要手动 DEL 缓存，框架已经按 Cache-Aside 的"先写库后删缓存"原则处理了。
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

type UpdateUserLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUpdateUserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateUserLogic {
	return &UpdateUserLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// UpdateUser 更新用户资料。proto 约定：字段为空字符串表示不更新该字段。
func (l *UpdateUserLogic) UpdateUser(in *user.UpdateUserReq) (*user.UpdateUserResp, error) {
	// 1. 先查出当前记录，作为"只更新非空字段"的基础（避免把没传的字段覆盖成空）。
	u, err := l.svcCtx.UserModel.FindOne(l.ctx, in.UserId)
	if errors.Is(err, usermodel.ErrNotFound) {
		return nil, errorx.New(errorx.UserNotFound)
	}
	if err != nil {
		return nil, err
	}

	// 2. 逐字段判断：非空字符串才覆盖，空字符串保留原值。
	if in.Nickname != "" {
		u.Nickname = in.Nickname
	}
	if in.Avatar != "" {
		u.Avatar = in.Avatar
	}
	if in.Bio != "" {
		u.Bio = in.Bio
	}
	if in.CityCode != "" {
		u.CityCode = in.CityCode
	}
	if in.CityName != "" {
		u.CityName = in.CityName
	}

	// 3. 写库（同时自动清理缓存，见文件头注释）。
	if err := l.svcCtx.UserModel.Update(l.ctx, u); err != nil {
		return nil, err
	}

	return &user.UpdateUserResp{
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
