// followLogic.go
//
// 职责：处理关注（Follow）请求的业务逻辑。
// 主要流程：
//  1. 校验参数：不能自己关注自己
//  2. 幂等检查：通过唯一索引查是否已关注
//  3. 生成 Snowflake ID 并插入 relations 表
//  4. 更新 Redis：关注列表、粉丝列表、粉丝数、大V集合
package logic

import (
	"context"
	"time"

	"github.com/sponge-dad/feed/app/relation/model"
	"github.com/sponge-dad/feed/app/relation/rpc/internal/svc"
	"github.com/sponge-dad/feed/app/relation/rpc/relation"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

// FollowLogic 关注逻辑处理器。
type FollowLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

// NewFollowLogic 构造 FollowLogic 实例。
func NewFollowLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FollowLogic {
	return &FollowLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// Follow 执行关注操作。
func (l *FollowLogic) Follow(in *relation.FollowReq) (*relation.FollowResp, error) {
	if in.FollowerId <= 0 || in.FolloweeId <= 0 {
		return nil, errorx.New(errorx.ParamError)
	}
	if in.FollowerId == in.FolloweeId {
		return nil, errorx.New(errorx.RelationSelf)
	}

	// 幂等：已存在则直接返回成功（可能客户端重复提交或网络重试）
	_, err := l.svcCtx.RelationModel.FindOneByFollowerIdFolloweeId(l.ctx, uint64(in.FollowerId), uint64(in.FolloweeId))
	if err == nil {
		return &relation.FollowResp{Success: true}, nil
	}
	if err != model.ErrNotFound {
		l.Errorf("FindOneByFollowerIdFolloweeId fail, follower=%d followee=%d err=%v", in.FollowerId, in.FolloweeId, err)
		return nil, err
	}

	now := time.Now().Unix()
	_, err = l.svcCtx.RelationModel.Insert(l.ctx, &model.Relations{
		Id:         uint64(l.svcCtx.IdGen()),
		FollowerId: uint64(in.FollowerId),
		FolloweeId: uint64(in.FolloweeId),
		CreatedAt:  now,
	})
	if err != nil {
		l.Errorf("Insert relation fail, follower=%d followee=%d err=%v", in.FollowerId, in.FolloweeId, err)
		return nil, err
	}

	// 异步更新 Redis 缓存（缓存写失败不阻塞主流程，打印日志即可）
	l.updateCacheAfterFollow(in.FollowerId, in.FolloweeId, now)

	return &relation.FollowResp{Success: true}, nil
}

// updateCacheAfterFollow 关注成功后更新相关缓存。
func (l *FollowLogic) updateCacheAfterFollow(followerId, followeeId int64, score int64) {
	if _, err := l.svcCtx.Redis.Zadd(followKey(followerId), score, int64toa(followeeId)); err != nil {
		l.Errorf("Zadd follow list fail, follower=%d followee=%d err=%v", followerId, followeeId, err)
	}
	if _, err := l.svcCtx.Redis.Zadd(fansKey(followeeId), score, int64toa(followerId)); err != nil {
		l.Errorf("Zadd fans list fail, followee=%d follower=%d err=%v", followeeId, followerId, err)
	}
	if _, err := l.svcCtx.Redis.Incr(fansCountKey(followeeId)); err != nil {
		l.Errorf("Incr fans count fail, followee=%d err=%v", followeeId, err)
	}

	// 检查是否达到大V阈值
	fansCount, err := l.svcCtx.Redis.Get(fansCountKey(followeeId))
	if err == nil {
		cnt := parseInt64(fansCount)
		if cnt >= l.svcCtx.Config.Vip.FansThreshold {
			if _, err := l.svcCtx.Redis.Sadd(redisKeyVipUsers, int64toa(followeeId)); err != nil {
				l.Errorf("Sadd vip set fail, followee=%d err=%v", followeeId, err)
			}
		}
	}
}
