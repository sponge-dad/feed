// mycollectslogic.go
//
// 职责：我收藏过的帖子列表（cursor 分页）。
// Interaction.GetUserCollectedFeeds 拿 feed_id 列表后，批量取帖子并聚合 FeedCard。
package interaction

import (
	"context"

	"github.com/sponge-dad/feed/app/gateway/internal/logic/aggregate"
	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	interactionClient "github.com/sponge-dad/feed/app/interaction/rpc/interactionClient"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

type MyCollectsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMyCollectsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MyCollectsLogic {
	return &MyCollectsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// MyCollects 我收藏过的帖子。
func (l *MyCollectsLogic) MyCollects(req *types.MyCollectsReq) (*types.FeedCardList, error) {
	me := middleware.MustGetUserID(l.ctx)
	if me == 0 {
		return nil, errorx.New(errorx.Unauthorized)
	}
	pageSize := aggregate.ClampPageSize(req.PageSize, 10, 50)

	rpcResp, err := l.svcCtx.InteractionRpc.GetUserCollectedFeeds(l.ctx, &interactionClient.GetUserCollectedFeedsReq{
		UserId:   me,
		PageSize: int32(pageSize),
		Cursor:   req.Cursor,
	})
	if err != nil {
		return nil, err
	}

	cards, err := buildFeedCardsByIDs(l.ctx, l.svcCtx, me, rpcResp.FeedIds)
	if err != nil {
		return nil, err
	}

	return &types.FeedCardList{
		List:       cards,
		NextCursor: rpcResp.NextCursor,
		HasMore:    rpcResp.NextCursor != "",
	}, nil
}
