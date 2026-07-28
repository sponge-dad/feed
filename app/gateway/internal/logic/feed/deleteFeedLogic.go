// deletefeedlogic.go
//
// 职责：删除帖子。网关先校验 token 中的 userId 是否为帖子作者（越权返回 12002），
// 再转发 Feed.DeleteFeed（服务端二次校验）。
package feed

import (
	"context"

	feedClient "github.com/sponge-dad/feed/app/feed/rpc/feedclient"
	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

type DeleteFeedLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDeleteFeedLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteFeedLogic {
	return &DeleteFeedLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// DeleteFeed 删除自己的帖子。
func (l *DeleteFeedLogic) DeleteFeed(req *types.DeleteFeedReq) (*types.DeleteFeedResp, error) {
	me := middleware.MustGetUserID(l.ctx)
	if me == 0 {
		return nil, errorx.New(errorx.Unauthorized)
	}
	if req.FeedID <= 0 {
		return nil, errorx.NewWithMsg(errorx.ParamError, "feedId 非法")
	}

	// 网关侧权限校验：仅作者可删。
	getResp, err := l.svcCtx.FeedRpc.GetFeed(l.ctx, &feedClient.GetFeedReq{
		FeedId: req.FeedID,
		UserId: me,
	})
	if err != nil {
		return nil, err
	}
	if getResp.Feed == nil || getResp.Feed.AuthorId != me {
		return nil, errorx.New(errorx.FeedNoPermission)
	}

	rpcResp, err := l.svcCtx.FeedRpc.DeleteFeed(l.ctx, &feedClient.DeleteFeedReq{
		FeedId: req.FeedID,
		UserId: me,
	})
	if err != nil {
		return nil, err
	}

	return &types.DeleteFeedResp{Success: rpcResp.Success}, nil
}
