// uncollectfeedlogic.go
//
// 职责：取消收藏帖子。转发 Interaction.UncollectFeed（下游幂等），
// 成功后 best-effort 回查最新收藏数返回。
package interaction

import (
	"context"

	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	interactionClient "github.com/sponge-dad/feed/app/interaction/rpc/interactionClient"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

type UncollectFeedLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUncollectFeedLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UncollectFeedLogic {
	return &UncollectFeedLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// UncollectFeed 取消收藏帖子。
func (l *UncollectFeedLogic) UncollectFeed(req *types.UncollectFeedReq) (*types.UncollectFeedResp, error) {
	me := middleware.MustGetUserID(l.ctx)
	if me == 0 {
		return nil, errorx.New(errorx.Unauthorized)
	}
	if req.FeedID <= 0 {
		return nil, errorx.NewWithMsg(errorx.ParamError, "feedId 非法")
	}

	if _, err := l.svcCtx.InteractionRpc.UncollectFeed(l.ctx, &interactionClient.UncollectFeedReq{
		UserId: me,
		FeedId: req.FeedID,
	}); err != nil {
		return nil, err
	}

	resp := &types.UncollectFeedResp{Success: true}
	if stats := fetchStats(l.ctx, l.svcCtx, req.FeedID); stats != nil {
		resp.CollectCount = stats.CollectCount
	}
	return resp, nil
}
