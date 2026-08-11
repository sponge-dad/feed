package logic

import (
	"context"
	"time"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type BatchGetFeedMetricsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewBatchGetFeedMetricsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BatchGetFeedMetricsLogic {
	return &BatchGetFeedMetricsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// maxMetricsBatch 批量上限（契约：≤100）。
const maxMetricsBatch = 100

// BatchGetFeedMetrics 批量原子指标（仅 raw，供内部服务聚合，如同类对比）。
// 结果按请求顺序返回；无数据的 feed 返回空 raw（不报错）。
func (l *BatchGetFeedMetricsLogic) BatchGetFeedMetrics(in *interaction.BatchGetFeedMetricsReq) (*interaction.BatchGetFeedMetricsResp, error) {
	if len(in.FeedIds) == 0 {
		return &interaction.BatchGetFeedMetricsResp{}, nil
	}
	if len(in.FeedIds) > maxMetricsBatch {
		return nil, errParamWith("批量查询超过上限(100)")
	}

	since := windowToSince("24h", time.Now())
	aggMap, err := l.svcCtx.FeedMetricsHourlyModel.SumByFeedIDs(l.ctx, in.FeedIds, since)
	if err != nil {
		return nil, err
	}

	list := make([]*interaction.FeedMetrics, 0, len(in.FeedIds))
	for _, id := range in.FeedIds {
		m := buildFeedMetrics(id, "24h", 0, aggMap[id], true)
		m.Rate = nil // 仅原子指标
		list = append(list, m)
	}
	return &interaction.BatchGetFeedMetricsResp{MetricsList: list}, nil
}
