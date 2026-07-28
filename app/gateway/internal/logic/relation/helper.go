// helper.go
//
// 职责：relation 模块 logic 的公共聚合逻辑。
// 关注/粉丝列表均为「Relation 拿 ID 列表 -> 并行聚合 User.BatchGetUsers + Relation.IsFollow」。
package relation

import (
	"context"

	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	relationClient "github.com/sponge-dad/feed/app/relation/rpc/relationclient"
	userClient "github.com/sponge-dad/feed/app/user/rpc/userClient"

	"github.com/zeromicro/go-zero/core/logx"
	"golang.org/x/sync/errgroup"
)

// buildRelationUsers 将用户 ID 列表聚合为 RelationUser 列表。
//
// 并行批量调用：
//   - User.BatchGetUsers  获取昵称/头像（失败则整体失败）；
//   - Relation.IsFollow   viewer 对列表用户的关注状态（失败降级为 false）。
//
// 说明：User.BatchGetUsers 返回 UserBrief，不含 bio，该字段暂返回空串。
// 已注销的用户会被跳过，返回顺序与 ids 保持一致。
func buildRelationUsers(ctx context.Context, svcCtx *svc.ServiceContext, viewerID int64, ids []int64) ([]types.RelationUser, error) {
	if len(ids) == 0 {
		return []types.RelationUser{}, nil
	}

	var (
		userMap   = make(map[int64]*userClient.UserBrief, len(ids))
		followMap = make(map[int64]bool, len(ids))
	)

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		resp, err := svcCtx.UserRpc.BatchGetUsers(gctx, &userClient.BatchGetUsersReq{UserIds: ids})
		if err != nil {
			return err
		}
		for _, u := range resp.Users {
			if u != nil {
				userMap[u.Id] = u
			}
		}
		return nil
	})

	g.Go(func() error {
		resp, err := svcCtx.RelationRpc.IsFollow(gctx, &relationClient.IsFollowReq{
			FollowerId:  viewerID,
			FolloweeIds: ids,
		})
		if err != nil {
			logx.WithContext(ctx).Errorf("relation: IsFollow degrade to false: %v", err)
			return nil
		}
		for id, ok := range resp.Results {
			followMap[id] = ok
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	list := make([]types.RelationUser, 0, len(ids))
	for _, id := range ids {
		u, ok := userMap[id]
		if !ok {
			logx.WithContext(ctx).Infof("relation: skip user %d, not found", id)
			continue
		}
		list = append(list, types.RelationUser{
			ID:          u.Id,
			Nickname:    u.Nickname,
			Avatar:      u.Avatar,
			Bio:         "", // UserBrief 不含 bio，暂返回空串
			IsFollowing: followMap[id],
		})
	}
	return list, nil
}

// clampPage 约束 page/page_size：page 最小 1；page_size 缺省 20、最大 50。
func clampPage(page, pageSize int64) (int64, int64) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 50 {
		pageSize = 50
	}
	return page, pageSize
}
