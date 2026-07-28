// likefeedlogic.go
//
// 职责：点赞帖子。转发 Interaction.LikeFeed（下游幂等，重复点赞不报错），
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

type LikeFeedLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLikeFeedLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LikeFeedLogic {
	return &LikeFeedLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// LikeFeed 点赞帖子。
func (l *LikeFeedLogic) LikeFeed(req *types.LikeFeedReq) (*types.LikeFeedResp, error) {
	me := middleware.MustGetUserID(l.ctx)
	if me == 0 {
		return nil, errorx.New(errorx.Unauthorized)
	}
	if req.FeedID <= 0 {
		return nil, errorx.NewWithMsg(errorx.ParamError, "feedId 非法")
	}

	if _, err := l.svcCtx.InteractionRpc.LikeFeed(l.ctx, &interactionClient.LikeFeedReq{
		UserId: me,
		FeedId: req.FeedID,
	}); err != nil {
		return nil, err
	}

	resp := &types.LikeFeedResp{Success: true}
	if stats := fetchStats(l.ctx, l.svcCtx, req.FeedID); stats != nil {
		resp.LikeCount = stats.LikeCount
	}
	return resp, nil
}
