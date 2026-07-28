// isfollowinglogic.go
//
// 职责：查询当前登录用户是否关注了目标用户。
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

type IsFollowingLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewIsFollowingLogic(ctx context.Context, svcCtx *svc.ServiceContext) *IsFollowingLogic {
	return &IsFollowingLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// IsFollowing 是否关注了 target_id。
func (l *IsFollowingLogic) IsFollowing(req *types.IsFollowingReq) (*types.IsFollowingResp, error) {
	me := middleware.MustGetUserID(l.ctx)
	if me == 0 {
		return nil, errorx.New(errorx.Unauthorized)
	}
	if req.TargetID <= 0 {
		return nil, errorx.NewWithMsg(errorx.ParamError, "target_id 非法")
	}

	rpcResp, err := l.svcCtx.RelationRpc.IsFollow(l.ctx, &relationClient.IsFollowReq{
		FollowerId:  me,
		FolloweeIds: []int64{req.TargetID},
	})
	if err != nil {
		return nil, err
	}

	return &types.IsFollowingResp{IsFollowing: rpcResp.Results[req.TargetID]}, nil
}
