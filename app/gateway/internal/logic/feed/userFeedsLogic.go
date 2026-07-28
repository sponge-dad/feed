// userfeedslogic.go
//
// 职责：个人主页帖子列表。下游 Feed.GetUserFeeds 为 page 分页，
// 网关用「页码 cursor」对外统一成 cursor 分页，并聚合 FeedCard。
package feed

import (
	"context"

	feedClient "github.com/sponge-dad/feed/app/feed/rpc/feedclient"
	"github.com/sponge-dad/feed/app/gateway/internal/logic/aggregate"
	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

type UserFeedsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUserFeedsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UserFeedsLogic {
	return &UserFeedsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// UserFeeds 指定用户的帖子列表。
func (l *UserFeedsLogic) UserFeeds(req *types.UserFeedsReq) (*types.FeedCardList, error) {
	me := middleware.MustGetUserID(l.ctx)
	if me == 0 {
		return nil, errorx.New(errorx.Unauthorized)
	}
	if req.UserID <= 0 {
		return nil, errorx.NewWithMsg(errorx.ParamError, "userId 非法")
	}
	pageSize := aggregate.ClampPageSize(req.PageSize, 10, 50)
	page, err := aggregate.PageFromCursor(req.Cursor)
	if err != nil {
		return nil, err
	}

	rpcResp, err := l.svcCtx.FeedRpc.GetUserFeeds(l.ctx, &feedClient.GetUserFeedsReq{
		UserId:   req.UserID,
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		return nil, err
	}

	cards, err := aggregate.BuildFeedCards(l.ctx, l.svcCtx, me, aggregate.ItemsFromBriefs(rpcResp.Feeds))
	if err != nil {
		return nil, err
	}

	var hasMore bool
	if rpcResp.Page != nil {
		hasMore = rpcResp.Page.HasMore
	}
	return &types.FeedCardList{
		List:       cards,
		NextCursor: aggregate.NextPageCursor(page, hasMore),
		HasMore:    hasMore,
	}, nil
}
