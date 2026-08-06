// Package logic 实现 Feed 服务的所有 gRPC 业务逻辑。
// getrecommendtimelinelogic.go 提供推荐时间线查询：从 feed:recommend ZSet 读取并回填。
package logic

import (
	"context"
	"math"
	"time"

	"github.com/sponge-dad/feed/app/feed/model"
	"github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/keys"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/requestid"
	"github.com/zeromicro/go-zero/core/logx"

	"github.com/sponge-dad/feed/app/feed/rpc/internal/logic/trace"
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

	tb := trace.NewBuilder(requestid.FromContext(l.ctx), in.UserId, "recommend", "", int32(pageSize))

	pairsStart := time.Now()
	pairs, err := l.svcCtx.Redis.ZrevrangebyscoreWithScoresAndLimitCtx(
		l.ctx, keys.Recommend(), math.MinInt64, math.MaxInt64, int(offset), int(pageSize))
	if err != nil {
		return nil, err
	}
	tb.RecordSource(feedSourceRecommendPool.String(), int32(len(pairs)), time.Since(pairsStart).Milliseconds())

	var briefs []*feed.FeedBrief
	var byID map[uint64]*model.Feeds
	if len(pairs) > 0 {
		feeds, ferr := l.svcCtx.FeedModel.FindByIds(l.ctx, zPairsToFeedIDs(pairs))
		if ferr != nil {
			return nil, ferr
		}
		byID = make(map[uint64]*model.Feeds, len(feeds))
		for _, f := range feeds {
			byID[f.Id] = f
		}
		briefs = briefsInPairOrder(pairs, byID)
	}

	// 来源标记：推荐流命中推荐池（recommend）。
	for _, b := range briefs {
		b.Source = int32(feedSourceRecommendPool)
	}

	tb.SetMergedCount(int32(len(pairs)))
	tb.SetReturnedCount(int32(len(briefs)))
	tb.SetFilteredCount(int32(len(pairs) - len(briefs)))

	// 逐条记录位置与来源，score 取自 ZSet。
	pos := int32(0)
	for _, p := range pairs {
		id, e := strconvParseFeedID(p.Key)
		if e != nil {
			continue
		}
		if _, ok := byID[uint64(id)]; ok {
			tb.AddItem(id, feedSourceRecommendPool.String(), pos, p.Score)
			pos++
		}
	}

	// 异步写入请求级 Trace（失败不阻塞主流程，见 02-request-trace §6）。
	go trace.Write(context.Background(), l.svcCtx.Redis, tb.Build(), l.svcCtx.Config.Trace.TTL, l.svcCtx.Config.Trace.SampleRate)

	hasMore := int64(len(briefs)) >= pageSize
	return &feed.GetRecommendTimelineResp{
		Feeds: briefs,
		Page:  &feed.PageInfo{HasMore: hasMore},
	}, nil
}
