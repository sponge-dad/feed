// followlogic.go
//
// 职责：关注用户。转发 Relation.Follow，并 best-effort 查询被关注者最新粉丝数返回。
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

type FollowLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewFollowLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FollowLogic {
	return &FollowLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// Follow 关注用户。
func (l *FollowLogic) Follow(req *types.FollowReq) (*types.FollowResp, error) {
	me := middleware.MustGetUserID(l.ctx)
	if me == 0 {
		return nil, errorx.New(errorx.Unauthorized)
	}
	if req.FolloweeID <= 0 {
		return nil, errorx.NewWithMsg(errorx.ParamError, "followee_id 非法")
	}
	if req.FolloweeID == me {
		return nil, errorx.New(errorx.RelationSelf)
	}

	rpcResp, err := l.svcCtx.RelationRpc.Follow(l.ctx, &relationClient.FollowReq{
		FollowerId: me,
		FolloweeId: req.FolloweeID,
	})
	if err != nil {
		return nil, err
	}

	return &types.FollowResp{
		Success:       rpcResp.Success,
		FollowerCount: l.followerCount(req.FolloweeID),
	}, nil
}

// followerCount best-effort 获取目标用户最新粉丝数，失败降级为 0 并记录日志。
func (l *FollowLogic) followerCount(userID int64) int64 {
	resp, err := l.svcCtx.RelationRpc.GetFans(l.ctx, &relationClient.GetFansReq{
		UserId:   userID,
		Page:     1,
		PageSize: 1,
	})
	if err != nil {
		l.Errorf("relation: get follower count of %d degrade to 0: %v", userID, err)
		return 0
	}
	return resp.Total
}
