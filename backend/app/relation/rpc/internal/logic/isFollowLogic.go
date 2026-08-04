// isFollowLogic.go
//
// 职责：批量查询 A 是否关注了 B 列表。
// 应用场景：Feed 流展示每篇帖子时，需要判断当前用户是否关注了这些作者。
package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/relation/model"
	"github.com/sponge-dad/feed/app/relation/rpc/internal/svc"
	"github.com/sponge-dad/feed/app/relation/rpc/relation"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

// IsFollowLogic 是否关注批量查询逻辑处理器。
type IsFollowLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

// NewIsFollowLogic 构造 IsFollowLogic 实例。
func NewIsFollowLogic(ctx context.Context, svcCtx *svc.ServiceContext) *IsFollowLogic {
	return &IsFollowLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// IsFollow 批量查询 follower_id 是否关注了 followee_ids 列表中的每一个用户。
// 返回 map[followee_id]bool，未查询到即 false。
func (l *IsFollowLogic) IsFollow(in *relation.IsFollowReq) (*relation.IsFollowResp, error) {
	if in.FollowerId <= 0 || len(in.FolloweeIds) == 0 {
		return nil, errorx.New(errorx.ParamError)
	}

	results := make(map[int64]bool, len(in.FolloweeIds))
	for _, id := range in.FolloweeIds {
		results[id] = false
	}

	for _, followeeId := range in.FolloweeIds {
		_, err := l.svcCtx.RelationModel.FindOneByFollowerIdFolloweeId(l.ctx, uint64(in.FollowerId), uint64(followeeId))
		if err == nil {
			results[followeeId] = true
			continue
		}
		if err == model.ErrNotFound {
			results[followeeId] = false
			continue
		}
		l.Errorf("FindOneByFollowerIdFolloweeId fail, follower=%d followee=%d err=%v", in.FollowerId, followeeId, err)
		return nil, err
	}

	return &relation.IsFollowResp{Results: results}, nil
}
