// unfollowlogic.go
//
// 职责：取消关注。转发 Relation.Unfollow，并 best-effort 查询被取关者最新粉丝数返回。
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

type UnfollowLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnfollowLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnfollowLogic {
	return &UnfollowLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// Unfollow 取消关注。
func (l *UnfollowLogic) Unfollow(req *types.UnfollowReq) (*types.UnfollowResp, error) {
	me := middleware.MustGetUserID(l.ctx)
	if me == 0 {
		return nil, errorx.New(errorx.Unauthorized)
	}
	if req.FolloweeID <= 0 {
		return nil, errorx.NewWithMsg(errorx.ParamError, "followee_id 非法")
	}

	rpcResp, err := l.svcCtx.RelationRpc.Unfollow(l.ctx, &relationClient.UnfollowReq{
		FollowerId: me,
		FolloweeId: req.FolloweeID,
	})
	if err != nil {
		return nil, err
	}

	var count int64
	if fans, ferr := l.svcCtx.RelationRpc.GetFans(l.ctx, &relationClient.GetFansReq{
		UserId: req.FolloweeID, Page: 1, PageSize: 1,
	}); ferr != nil {
		l.Errorf("relation: get follower count of %d degrade to 0: %v", req.FolloweeID, ferr)
	} else {
		count = fans.Total
	}

	return &types.UnfollowResp{
		Success:       rpcResp.Success,
		FollowerCount: count,
	}, nil
}
