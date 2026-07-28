// unlikefeedlogic.go
//
// 职责：取消点赞帖子。转发 Interaction.UnlikeFeed（下游幂等），
// 成功后 best-effort 回查最新点赞数返回。
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

type UnlikeFeedLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnlikeFeedLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnlikeFeedLogic {
	return &UnlikeFeedLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// UnlikeFeed 取消点赞帖子。
func (l *UnlikeFeedLogic) UnlikeFeed(req *types.UnlikeFeedReq) (*types.UnlikeFeedResp, error) {
	me := middleware.MustGetUserID(l.ctx)
	if me == 0 {
		return nil, errorx.New(errorx.Unauthorized)
	}
	if req.FeedID <= 0 {
		return nil, errorx.NewWithMsg(errorx.ParamError, "feedId 非法")
	}

	if _, err := l.svcCtx.InteractionRpc.UnlikeFeed(l.ctx, &interactionClient.UnlikeFeedReq{
		UserId: me,
		FeedId: req.FeedID,
	}); err != nil {
		return nil, err
	}

	resp := &types.UnlikeFeedResp{Success: true}
	if stats := fetchStats(l.ctx, l.svcCtx, req.FeedID); stats != nil {
		resp.LikeCount = stats.LikeCount
	}
	return resp, nil
}
