// metrics_calc.go
//
// 创作者指标计算核心（US5）：窗口解析、原子指标 → 派生率换算、Feed 作者归属查询。
// 派生率分母为 0 时返回 nil（optional double），不返回 0，避免误导（08-creator-metrics.md §2）。
package logic

import (
	"context"
	"time"

	feedClient "github.com/sponge-dad/feed/app/feed/rpc/feedclient"
	"github.com/sponge-dad/feed/app/interaction/model"
	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	feedpb "github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/common/errorx"
)

// windowToSince 解析时间窗口 → 起始时间（含当前小时由调用方决定完整度）。
func windowToSince(window string, now time.Time) time.Time {
	switch window {
	case "1h":
		return now.Add(-1 * time.Hour)
	case "7d":
		return now.Add(-7 * 24 * time.Hour)
	case "all":
		return time.Unix(0, 0)
	default: // "24h" 及非法值都按 24h（安全默认）
		return now.Add(-24 * time.Hour)
	}
}

// errParam 参数错误。
func errParam() error { return errorx.New(errorx.ParamError) }

// errParamWith 参数错误（自定义提示）。
func errParamWith(msg string) error { return errorx.NewWithMsg(errorx.ParamError, msg) }

// rateOf 派生率换算：分母为 0 返回 nil（null），不返回 0。
func rateOf(num, den int64) *float64 {
	if den <= 0 {
		return nil
	}
	v := float64(num) / float64(den)
	return &v
}

// buildFeedMetrics 由聚合结果构造 FeedMetrics（raw + 派生率）。
func buildFeedMetrics(feedID int64, window string, publishedAt int64, agg *model.FeedMetricsHourly, complete bool) *interaction.FeedMetrics {
	if agg == nil {
		agg = &model.FeedMetricsHourly{}
	}
	m := &interaction.FeedMetrics{
		FeedId:      feedID,
		Window:      window,
		PublishedAt: publishedAt,
		Raw: &interaction.FeedMetricsRaw{
			Expose:          agg.ExposeCount,
			Play:            agg.PlayCount,
			EffectivePlay:   agg.EffectivePlayCount,
			Finish:          agg.FinishCount,
			Skip:            agg.SkipCount,
			WatchDurationMs: agg.WatchDurationMs,
			Like:            agg.LikeCount,
			Collect:         agg.CollectCount,
			Comment:         agg.CommentCount,
			Share:           agg.ShareCount,
		},
		Rate: &interaction.FeedMetricsRate{
			PlayRate:          rateOf(agg.PlayCount, agg.ExposeCount),
			EffectivePlayRate: rateOf(agg.EffectivePlayCount, agg.ExposeCount),
			FinishRate:        rateOf(agg.FinishCount, agg.PlayCount),
			SkipRate:          rateOf(agg.SkipCount, agg.ExposeCount),
			AvgWatchMs:        rateOf(agg.WatchDurationMs, agg.PlayCount),
			LikeRate:          rateOf(agg.LikeCount, agg.PlayCount),
			CollectRate:       rateOf(agg.CollectCount, agg.PlayCount),
			CommentRate:       rateOf(agg.CommentCount, agg.PlayCount),
			ShareRate:         rateOf(agg.ShareCount, agg.PlayCount),
		},
		DataComplete: complete,
	}
	return m
}

// feedBasic 从 Feed RPC 取作者与发布时间（指标归属校验用）。
type feedBasic struct {
	AuthorID    int64
	PublishedAt int64
	FeedType    int32
}

// fetchFeedBasic 查 Feed 基础信息；不存在返回 12001（透传）。
func fetchFeedBasic(ctx context.Context, feedRpc feedClient.Feed, feedID int64) (*feedBasic, error) {
	resp, err := feedRpc.GetFeed(ctx, &feedpb.GetFeedReq{FeedId: feedID})
	if err != nil {
		return nil, err
	}
	f := resp.Feed
	if f == nil {
		return nil, errorx.New(errorx.FeedNotFound)
	}
	return &feedBasic{
		AuthorID:    f.AuthorId,
		PublishedAt: f.CreatedAt,
		FeedType:    f.FeedType,
	}, nil
}
