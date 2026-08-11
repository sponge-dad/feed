package logic

import (
	"context"
	"time"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetFeedMetricsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetFeedMetricsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetFeedMetricsLogic {
	return &GetFeedMetricsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetFeedMetrics 单 feed 原子指标 + 派生率（创作者数据，作者归属校验在 GetCreatorMetrics）。
// 供内部服务/Agent Tool 使用；对外创作者入口请走 GetCreatorMetrics。
func (l *GetFeedMetricsLogic) GetFeedMetrics(in *interaction.GetFeedMetricsReq) (*interaction.GetFeedMetricsResp, error) {
	if in.FeedId <= 0 {
		return nil, errParam()
	}
	m, _, err := aggregateFeedMetrics(l.ctx, l.svcCtx, in.FeedId, normalizeWindow(in.Window))
	if err != nil {
		return nil, err
	}
	return &interaction.GetFeedMetricsResp{Metrics: m}, nil
}

// aggregateFeedMetrics 聚合单 feed 指标（供 GetFeedMetrics / GetCreatorMetrics 复用）。
func aggregateFeedMetrics(ctx context.Context, svcCtx *svc.ServiceContext, feedID int64, window string) (*interaction.FeedMetrics, *feedBasic, error) {
	basic, err := fetchFeedBasic(ctx, svcCtx.FeedRpc, feedID)
	if err != nil {
		return nil, nil, err
	}
	since := windowToSince(window, time.Now())
	agg, err := svcCtx.FeedMetricsHourlyModel.SumByFeedAndWindow(ctx, feedID, since)
	if err != nil {
		return nil, nil, err
	}
	// data_complete：当前小时在进行中 → false（当前小时桶数据不完整）。
	now := time.Now()
	complete := now.Minute() == 0 && now.Second() == 0
	m := buildFeedMetrics(feedID, window, basic.PublishedAt, agg, complete)
	return m, basic, nil
}

// normalizeWindow 窗口归一化（非法值回落 24h）。
func normalizeWindow(w string) string {
	switch w {
	case "1h", "24h", "7d", "all":
		return w
	default:
		return "24h"
	}
}
