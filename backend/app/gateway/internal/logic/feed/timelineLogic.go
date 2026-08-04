// timelinelogic.go
//
// 职责：首页信息流（recommend / follow / city 三流合一）。
//   - recommend/city 下游为 page 分页，网关用「页码 cursor」对外统一成 cursor 分页；
//   - follow 下游原生 cursor 分页，透传；
//   - city 按请求 IP 实时解析 city_code（common/ipx），解析失败返回业务码 12006；
//   - 帖子基础数据返回后由 aggregate.BuildFeedCards 并行聚合作者与互动数据。
package feed

import (
	"context"

	feedClient "github.com/sponge-dad/feed/app/feed/rpc/feedclient"
	"github.com/sponge-dad/feed/app/gateway/internal/logic/aggregate"
	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	"github.com/sponge-dad/feed/common/errorx"
	"github.com/sponge-dad/feed/common/ipx"

	"github.com/zeromicro/go-zero/core/logx"
)

type TimelineLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTimelineLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TimelineLogic {
	return &TimelineLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// Timeline 首页信息流。
func (l *TimelineLogic) Timeline(req *types.TimelineReq) (*types.FeedCardList, error) {
	me := middleware.MustGetUserID(l.ctx)
	if me == 0 {
		return nil, errorx.New(errorx.Unauthorized)
	}
	pageSize := aggregate.ClampPageSize(req.PageSize, 10, 50)

	var (
		briefs     []*feedClient.FeedBrief
		nextCursor string
		hasMore    bool
	)

	switch req.Type {
	case "follow":
		rpcResp, err := l.svcCtx.FeedRpc.GetFollowTimeline(l.ctx, &feedClient.GetFollowTimelineReq{
			UserId:   me,
			Cursor:   req.Cursor,
			PageSize: pageSize,
		})
		if err != nil {
			return nil, err
		}
		briefs = rpcResp.Feeds
		if rpcResp.Page != nil {
			nextCursor, hasMore = rpcResp.Page.Cursor, rpcResp.Page.HasMore
		}
	case "city":
		loc, lerr := l.svcCtx.IPResolver.Resolve(ipx.ClientIPFromContext(l.ctx))
		if lerr != nil {
			return nil, errorx.New(errorx.FeedIPLocateFail)
		}
		page, perr := aggregate.PageFromCursor(req.Cursor)
		if perr != nil {
			return nil, perr
		}
		rpcResp, err := l.svcCtx.FeedRpc.GetCityTimeline(l.ctx, &feedClient.GetCityTimelineReq{
			UserId:   me,
			CityCode: loc.CityCode,
			Page:     page,
			PageSize: pageSize,
		})
		if err != nil {
			return nil, err
		}
		briefs = rpcResp.Feeds
		if rpcResp.Page != nil {
			hasMore = rpcResp.Page.HasMore
		}
		nextCursor = aggregate.NextPageCursor(page, hasMore)
	default: // recommend
		page, perr := aggregate.PageFromCursor(req.Cursor)
		if perr != nil {
			return nil, perr
		}
		rpcResp, err := l.svcCtx.FeedRpc.GetRecommendTimeline(l.ctx, &feedClient.GetRecommendTimelineReq{
			UserId:   me,
			Page:     page,
			PageSize: pageSize,
		})
		if err != nil {
			return nil, err
		}
		briefs = rpcResp.Feeds
		if rpcResp.Page != nil {
			hasMore = rpcResp.Page.HasMore
		}
		nextCursor = aggregate.NextPageCursor(page, hasMore)
	}

	cards, err := aggregate.BuildFeedCards(l.ctx, l.svcCtx, me, aggregate.ItemsFromBriefs(briefs))
	if err != nil {
		return nil, err
	}

	return &types.FeedCardList{
		List:       cards,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}
