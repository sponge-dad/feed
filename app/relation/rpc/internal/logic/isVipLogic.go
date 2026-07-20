// isVipLogic.go
//
// 职责：判断某用户是否是大V（粉丝数 >= 阈值）。
// 大V用户会被加入 Redis Set `user:vip_users`，Feed 推荐算法、榜单等功能可
// 直接使用这个集合。
package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/relation/rpc/internal/svc"
	"github.com/sponge-dad/feed/app/relation/rpc/relation"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

// IsVipLogic 大V判定逻辑处理器。
type IsVipLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

// NewIsVipLogic 构造 IsVipLogic 实例。
func NewIsVipLogic(ctx context.Context, svcCtx *svc.ServiceContext) *IsVipLogic {
	return &IsVipLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// IsVip 判断用户是否是大V。
// 优先查 Redis Set，未命中时查 fans_count 缓存，都没有则回源 MySQL 重建。
func (l *IsVipLogic) IsVip(in *relation.IsVipReq) (*relation.IsVipResp, error) {
	if in.UserId <= 0 {
		return nil, errorx.New(errorx.ParamError)
	}

	isMember, err := l.svcCtx.Redis.Sismember(redisKeyVipUsers, int64toa(in.UserId))
	if err == nil && isMember {
		return &relation.IsVipResp{IsVip: true}, nil
	}

	fansCountStr, err := l.svcCtx.Redis.Get(fansCountKey(in.UserId))
	if err == nil && fansCountStr != "" {
		cnt := parseInt64(fansCountStr)
		isVip := cnt >= l.svcCtx.Config.Vip.FansThreshold
		if isVip {
			if _, err := l.svcCtx.Redis.Sadd(redisKeyVipUsers, int64toa(in.UserId)); err != nil {
				l.Errorf("Sadd vip set fail, userId=%d err=%v", in.UserId, err)
			}
		}
		return &relation.IsVipResp{IsVip: isVip}, nil
	}

	count, err := l.rebuildFansCount(in.UserId)
	if err != nil {
		l.Errorf("rebuildFansCount fail, userId=%d err=%v", in.UserId, err)
		return nil, err
	}

	isVip := count >= l.svcCtx.Config.Vip.FansThreshold
	return &relation.IsVipResp{IsVip: isVip}, nil
}

// rebuildFansCount 从 DB 重新计算粉丝数并写回 Redis。
func (l *IsVipLogic) rebuildFansCount(userId int64) (int64, error) {
	resp, err := NewGetFansLogic(l.ctx, l.svcCtx).GetFans(&relation.GetFansReq{
		UserId:   userId,
		Page:     1,
		PageSize: 1000,
	})
	if err != nil {
		return 0, err
	}
	count := int64(len(resp.FollowerIds))

	if err := l.svcCtx.Redis.Set(fansCountKey(userId), int64toa(count)); err != nil {
		l.Errorf("Set fans count cache fail, userId=%d err=%v", userId, err)
	}

	if count >= l.svcCtx.Config.Vip.FansThreshold {
		if _, err := l.svcCtx.Redis.Sadd(redisKeyVipUsers, int64toa(userId)); err != nil {
			l.Errorf("Sadd vip set fail, userId=%d err=%v", userId, err)
		}
	}
	return count, nil
}
