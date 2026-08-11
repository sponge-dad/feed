package logic

import (
	"context"
	"sort"
	"time"

	contentpb "github.com/sponge-dad/feed/app/content/rpc/content"
	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetPeerAverageMetricsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetPeerAverageMetricsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetPeerAverageMetricsLogic {
	return &GetPeerAverageMetricsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// minPeerSample 同类对比最少样本数。
const minPeerSample = 20

// GetPeerAverageMetrics 同类内容匿名对比（08-creator-metrics.md §3）：
//
//	两步跨库：先按 category 从 Content RPC 取同类 feed_id 列表（SearchContent），
//	再聚合 feed_metrics_hourly。只返回匿名统计量（avg/p25/p50/p75/sample_size），
//	⚠️ 禁止返回任何 feed_id / author_id（响应结构不含此类字段）。
//
// 简化实现说明（相对设计文档）：
//   - 未实现 L1（发布时间±72h）/L2 多级降级，直接取「同 category 近 30 天」为同类集，
//     应用层按时长分桶过滤；不足 minPeerSample 时返回 insufficient_sample=true。
func (l *GetPeerAverageMetricsLogic) GetPeerAverageMetrics(in *interaction.GetPeerAverageMetricsReq) (*interaction.GetPeerAverageMetricsResp, error) {
	if in.FeedId <= 0 || in.ViewerId <= 0 {
		return nil, errParam()
	}

	// 1. 归属校验：持有该 feed 的创作者（或内部）。
	_, basic, err := aggregateFeedMetrics(l.ctx, l.svcCtx, in.FeedId, normalizeWindow(in.Window))
	if err != nil {
		return nil, err
	}
	if basic.AuthorID != in.ViewerId && !l.svcCtx.IsInternal(in.ViewerId) {
		return nil, errorx.New(errorx.InteractionMetricsForbidden)
	}

	// 2. 取当前 feed 画像（category / 时长分桶）。
	profile, err := l.svcCtx.ContentRpc.GetContentProfile(l.ctx, &contentpb.GetContentProfileReq{
		FeedId:   in.FeedId,
		ViewerId: in.ViewerId,
	})
	if err != nil {
		// 画像不存在/未完成：无对比基础 → 样本不足。
		return &interaction.GetPeerAverageMetricsResp{InsufficientSample: true, PeerLevel: "NONE"}, nil
	}
	p := profile.Profile
	bucket := durationBucket(p.MediaDurationMs)

	// 3. 按 category 召回同类 feed（近 30 天，上限 100）。
	searchResp, err := l.svcCtx.ContentRpc.SearchContent(l.ctx, &contentpb.SearchContentReq{
		Category:          p.Category,
		PublishedWithinDays: 30,
		Limit:             100,
		ViewerId:          in.ViewerId,
	})
	if err != nil {
		return nil, err
	}
	if searchResp == nil || len(searchResp.Items) == 0 {
		return &interaction.GetPeerAverageMetricsResp{
			InsufficientSample: true,
			PeerLevel:          "L3",
			Category:           p.Category,
			DurationBucket:     bucket,
		}, nil
	}

	// 4. 应用层按时长分桶过滤；不足则放宽到全部同类。
	items := searchResp.Items
	filtered := make([]int64, 0, len(items))
	all := make([]int64, 0, len(items))
	for _, it := range items {
		all = append(all, it.FeedId)
		if durationBucket(it.MediaDurationMs) == bucket {
			filtered = append(filtered, it.FeedId)
		}
	}
	peerIDs := filtered
	peerLevel := "L3"
	if len(filtered) < minPeerSample {
		peerIDs = all
		peerLevel = "FALLBACK"
	}

	// 5. 聚合同类指标并计算分位数。
	rate, sampleSize := l.peerRate(in.FeedId, peerIDs)
	if sampleSize < minPeerSample {
		return &interaction.GetPeerAverageMetricsResp{
			PeerLevel:          peerLevel,
			SampleSize:         int32(sampleSize),
			Category:           p.Category,
			DurationBucket:     bucket,
			Rate:               rate,
			InsufficientSample: true,
		}, nil
	}

	return &interaction.GetPeerAverageMetricsResp{
		PeerLevel:      peerLevel,
		SampleSize:     int32(sampleSize),
		Category:       p.Category,
		DurationBucket: bucket,
		Rate:           rate,
	}, nil
}

// peerRate 计算同类 play_rate / skip_rate / finish_rate 的 avg/p25/p50/p75。
func (l *GetPeerAverageMetricsLogic) peerRate(selfFeedID int64, peerIDs []int64) (*interaction.PeerRateDistribution, int) {
	since := windowToSince("24h", time.Now())
	aggMap, err := l.svcCtx.FeedMetricsHourlyModel.SumByFeedIDs(l.ctx, peerIDs, since)
	if err != nil {
		return &interaction.PeerRateDistribution{}, 0
	}

	var playRates, skipRates, finishRates []float64
	for feedID, agg := range aggMap {
		if feedID == selfFeedID {
			continue // 不把自己算进同类基准
		}
		if r := rateOf(agg.PlayCount, agg.ExposeCount); r != nil {
			playRates = append(playRates, *r)
		}
		if r := rateOf(agg.SkipCount, agg.ExposeCount); r != nil {
			skipRates = append(skipRates, *r)
		}
		if r := rateOf(agg.FinishCount, agg.PlayCount); r != nil {
			finishRates = append(finishRates, *r)
		}
	}
	n := len(playRates)
	if len(finishRates) > n {
		n = len(finishRates)
	}
	if len(skipRates) > n {
		n = len(skipRates)
	}

	return &interaction.PeerRateDistribution{
		PlayRate:   percentiles(playRates),
		SkipRate:   percentiles(skipRates),
		FinishRate: percentiles(finishRates),
	}, n
}

// percentiles 计算 avg/p25/p50/p75；空输入返回空 Percentile。
func percentiles(values []float64) *interaction.Percentile {
	out := &interaction.Percentile{}
	if len(values) == 0 {
		return out
	}
	sort.Float64s(values)
	var sum float64
	for _, v := range values {
		sum += v
	}
	avg := sum / float64(len(values))
	p25 := values[len(values)*25/100]
	p50 := values[len(values)*50/100]
	p75 := values[len(values)*75/100]
	out.Avg = &avg
	out.P25 = &p25
	out.P50 = &p50
	out.P75 = &p75
	return out
}

// durationBucket 时长分桶（08-creator-metrics.md §3）。
func durationBucket(ms int64) string {
	switch {
	case ms <= 15000:
		return "0-15s"
	case ms <= 30000:
		return "15-30s"
	case ms <= 60000:
		return "30-60s"
	default:
		return "60s+"
	}
}
