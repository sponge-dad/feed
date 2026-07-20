// unfollowLogic.go
//
// 职责：处理取消关注（Unfollow）请求的业务逻辑。
// 主要流程：
//  1. 参数校验
//  2. 按 follower_id + followee_id 唯一索引查记录
//  3. 删除 MySQL 记录（model 层会自动清理相关缓存 key）
//  4. 同步删除/更新 Redis 里的关注列表、粉丝列表、粉丝数、大V集合
package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/relation/model"
	"github.com/sponge-dad/feed/app/relation/rpc/internal/svc"
	"github.com/sponge-dad/feed/app/relation/rpc/relation"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

// UnfollowLogic 取消关注逻辑处理器。
type UnfollowLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

// NewUnfollowLogic 构造 UnfollowLogic 实例。
func NewUnfollowLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnfollowLogic {
	return &UnfollowLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// Unfollow 执行取消关注操作。
func (l *UnfollowLogic) Unfollow(in *relation.UnfollowReq) (*relation.UnfollowResp, error) {
	if in.FollowerId <= 0 || in.FolloweeId <= 0 {
		return nil, errorx.New(errorx.ParamError)
	}
	if in.FollowerId == in.FolloweeId {
		return nil, errorx.New(errorx.RelationSelf)
	}

	rel, err := l.svcCtx.RelationModel.FindOneByFollowerIdFolloweeId(l.ctx, uint64(in.FollowerId), uint64(in.FolloweeId))
	if err == model.ErrNotFound {
		return &relation.UnfollowResp{Success: true}, nil
	}
	if err != nil {
		l.Errorf("FindOneByFollowerIdFolloweeId fail, follower=%d followee=%d err=%v", in.FollowerId, in.FolloweeId, err)
		return nil, err
	}

	if err := l.svcCtx.RelationModel.Delete(l.ctx, rel.Id); err != nil {
		l.Errorf("Delete relation fail, id=%d err=%v", rel.Id, err)
		return nil, err
	}

	l.updateCacheAfterUnfollow(in.FollowerId, in.FolloweeId)

	return &relation.UnfollowResp{Success: true}, nil
}

// updateCacheAfterUnfollow 取消关注后更新相关缓存。
func (l *UnfollowLogic) updateCacheAfterUnfollow(followerId, followeeId int64) {
	if _, err := l.svcCtx.Redis.Zrem(followKey(followerId), int64toa(followeeId)); err != nil {
		l.Errorf("Zrem follow list fail, follower=%d followee=%d err=%v", followerId, followeeId, err)
	}
	if _, err := l.svcCtx.Redis.Zrem(fansKey(followeeId), int64toa(followerId)); err != nil {
		l.Errorf("Zrem fans list fail, followee=%d follower=%d err=%v", followeeId, followerId, err)
	}
	if _, err := l.svcCtx.Redis.Decr(fansCountKey(followeeId)); err != nil {
		l.Errorf("Decr fans count fail, followee=%d err=%v", followeeId, err)
	}
	if _, err := l.svcCtx.Redis.Srem(redisKeyVipUsers, int64toa(followeeId)); err != nil {
		l.Errorf("Srem vip set fail, followee=%d err=%v", followeeId, err)
	}
}
