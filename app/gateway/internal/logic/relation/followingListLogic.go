// followinglistlogic.go
//
// 职责：关注列表（我/指定用户 关注了谁）。
// Relation.GetFollows 拿 ID 列表后，并行聚合用户信息与 viewer 的关注状态。
package relation

import (
	"context"

	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	relationClient "github.com/sponge-dad/feed/app/relation/rpc/relationclient"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

type FollowingListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewFollowingListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FollowingListLogic {
	return &FollowingListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// FollowingList 关注列表，user_id 缺省时查当前登录用户。
func (l *FollowingListLogic) FollowingList(req *types.FollowingListReq) (*types.RelationUserList, error) {
	me := middleware.MustGetUserID(l.ctx)
	if me == 0 {
		return nil, errorx.New(errorx.Unauthorized)
	}

	uid := req.UserID
	if uid == 0 {
		uid = me
	}
	page, pageSize := clampPage(req.Page, req.PageSize)

	rpcResp, err := l.svcCtx.RelationRpc.GetFollows(l.ctx, &relationClient.GetFollowsReq{
		UserId:   uid,
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		return nil, err
	}

	list, err := buildRelationUsers(l.ctx, l.svcCtx, me, rpcResp.FolloweeIds)
	if err != nil {
		return nil, err
	}

	return &types.RelationUserList{
		List:     list,
		Page:     page,
		PageSize: pageSize,
		Total:    rpcResp.Total,
		HasMore:  page*pageSize < rpcResp.Total,
	}, nil
}
