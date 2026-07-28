// Package logic 实现 Feed 服务的所有 gRPC 业务逻辑。
// getcitytimelinelogic.go 提供同城时间线查询：优先读 feed:city:{code} ZSet，冷启动降级 MySQL。
package logic

import (
	"context"
	"math"

	"github.com/sponge-dad/feed/app/feed/model"
	"github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/keys"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/svc"
	"github.com/zeromicro/go-zero/core/logx"
)

// GetCityTimelineLogic 封装 GetCityTimeline 请求所需的上下文与依赖。
type GetCityTimelineLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

// NewGetCityTimelineLogic 构造 GetCityTimelineLogic。
func NewGetCityTimelineLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetCityTimelineLogic {
	return &GetCityTimelineLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetCityTimeline 分页查询同城时间线。
// 主路径：从 feed:city:{cityCode} ZSet（score=发布秒级时间戳）按 score 倒序 offset 分页，
// 再批量 FindByIds 回填；同城池为空（冷启动/尚未写入）时降级为 MySQL 按发布时间倒序取最近帖子。
func (l *GetCityTimelineLogic) GetCityTimeline(in *feed.GetCityTimelineReq) (*feed.GetCityTimelineResp, error) {
	if in.CityCode == "" {
		return &feed.GetCityTimelineResp{Feeds: nil, Page: &feed.PageInfo{}}, nil
	}
	page := in.Page
	if page <= 0 {
		page = 1
	}
	pageSize := int64(in.PageSize)
	if pageSize <= 0 {
		pageSize = defaultUserFeedPageSize
	}
	if pageSize > maxUserFeedPageSize {
		pageSize = maxUserFeedPageSize
	}
	offset := (page - 1) * pageSize

	rdb := l.svcCtx.Redis
	cityK := keys.City(in.CityCode)
	pairs, err := rdb.ZrevrangebyscoreWithScoresAndLimitCtx(l.ctx, cityK, math.MinInt64, math.MaxInt64, int(offset), int(pageSize))
	if err != nil {
		return nil, err
	}

	var briefs []*feed.FeedBrief
	if len(pairs) > 0 {
		feeds, ferr := l.svcCtx.FeedModel.FindByIds(l.ctx, zPairsToFeedIDs(pairs))
		if ferr != nil {
			return nil, ferr
		}
		byID := make(map[uint64]*model.Feeds, len(feeds))
		for _, f := range feeds {
			byID[f.Id] = f
		}
		briefs = briefsInPairOrder(pairs, byID)
	} else {
		// 同城池冷启动降级：直接走 MySQL（见 05-timeline-city.md）。
		feeds, derr := l.svcCtx.FeedModel.FindByCityCode(l.ctx, in.CityCode, uint64(pageSize), uint64(offset))
		if derr != nil {
			return nil, derr
		}
		briefs = make([]*feed.FeedBrief, 0, len(feeds))
		for _, f := range feeds {
			briefs = append(briefs, toFeedBrief(f))
		}
	}

	hasMore := int64(len(briefs)) >= pageSize
	return &feed.GetCityTimelineResp{
		Feeds: briefs,
		Page:  &feed.PageInfo{HasMore: hasMore},
	}, nil
}
