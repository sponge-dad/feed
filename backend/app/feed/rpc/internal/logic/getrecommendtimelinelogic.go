// Package logic 实现 Feed 服务的所有 gRPC 业务逻辑。
// getrecommendtimelinelogic.go 提供推荐时间线查询：从 feed:recommend ZSet 读取并回填。
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

// GetRecommendTimelineLogic 封装 GetRecommendTimeline 请求所需的上下文与依赖。
type GetRecommendTimelineLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

// NewGetRecommendTimelineLogic 构造 GetRecommendTimelineLogic。
func NewGetRecommendTimelineLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetRecommendTimelineLogic {
	return &GetRecommendTimelineLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetRecommendTimeline 分页查询推荐时间线。
// V1 策略：推荐池由 worker 写入 feed:recommend ZSet（score=发布秒级时间戳），
// 按 score 倒序 offset 分页后批量 FindByIds 回填。推荐池为空时返回空列表（最终一致性）。
func (l *GetRecommendTimelineLogic) GetRecommendTimeline(in *feed.GetRecommendTimelineReq) (*feed.GetRecommendTimelineResp, error) {
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

	pairs, err := l.svcCtx.Redis.ZrevrangebyscoreWithScoresAndLimitCtx(
		l.ctx, keys.Recommend(), math.MinInt64, math.MaxInt64, int(offset), int(pageSize))
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
	}

	// 来源标记：推荐流命中推荐池（recommend）。
	for _, b := range briefs {
		b.Source = int32(feedSourceRecommendPool)
	}

	hasMore := int64(len(briefs)) >= pageSize
	return &feed.GetRecommendTimelineResp{
		Feeds: briefs,
		Page:  &feed.PageInfo{HasMore: hasMore},
	}, nil
}
