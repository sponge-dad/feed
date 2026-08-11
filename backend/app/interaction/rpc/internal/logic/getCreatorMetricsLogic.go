package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetCreatorMetricsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetCreatorMetricsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetCreatorMetricsLogic {
	return &GetCreatorMetricsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetCreatorMetrics 创作者视角的作品指标：
//   - viewer_id 归属校验：非作者本人且非内部用户 → 14005 InteractionMetricsForbidden
//   - 内部用户可查任意作品，审计日志记录（08-creator-metrics.md §6）
func (l *GetCreatorMetricsLogic) GetCreatorMetrics(in *interaction.GetCreatorMetricsReq) (*interaction.GetCreatorMetricsResp, error) {
	if in.FeedId <= 0 || in.ViewerId <= 0 {
		return nil, errParam()
	}

	// 归属校验（先查 feed 基础信息，顺带复用聚合）。
	m, basic, err := aggregateFeedMetrics(l.ctx, l.svcCtx, in.FeedId, normalizeWindow(in.Window))
	if err != nil {
		return nil, err
	}
	if basic.AuthorID != in.ViewerId && !l.svcCtx.IsInternal(in.ViewerId) {
		return nil, errorx.New(errorx.InteractionMetricsForbidden)
	}
	if l.svcCtx.IsInternal(in.ViewerId) && basic.AuthorID != in.ViewerId {
		l.Logger.Infof("internal user %d accessed creator metrics of feed %d (author %d)", in.ViewerId, in.FeedId, basic.AuthorID)
	}

	return &interaction.GetCreatorMetricsResp{Metrics: m}, nil
}
