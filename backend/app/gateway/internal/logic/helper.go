package logic

import (
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	userClient "github.com/sponge-dad/feed/app/user/rpc/userClient"
)

// userInfoToUser 将 user.proto 的 UserInfo 转换为 HTTP 层对外展示的类型。
// 私有桶头像地址在此处统一签名，使客户端可直接访问。
func userInfoToUser(svcCtx *svc.ServiceContext, info *userClient.UserInfo) *types.User {
	if info == nil {
		return nil
	}
	return &types.User{
		ID:       info.Id,
		Username: info.Username,
		Nickname: info.Nickname,
		Avatar:   SignCosRef(svcCtx, info.Avatar),
		Bio:      info.Bio,
		CityName: info.CityName,
	}
}

// userInfoToDetail 将 user.proto 的 UserInfo 转换为 UserDetail（不含聚合字段）。
// 私有桶头像地址在此处统一签名，使客户端可直接访问。
func userInfoToDetail(svcCtx *svc.ServiceContext, info *userClient.UserInfo) *types.UserDetail {
	if info == nil {
		return nil
	}
	return &types.UserDetail{
		ID:       info.Id,
		Username: info.Username,
		Nickname: info.Nickname,
		Avatar:   SignCosRef(svcCtx, info.Avatar),
		Bio:      info.Bio,
		CityName: info.CityName,
	}
}
