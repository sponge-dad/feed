package feed

import (
	"context"

	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	"github.com/sponge-dad/feed/common/errorx"
	interactionpb "github.com/sponge-dad/feed/app/interaction/rpc/interaction"

	"github.com/zeromicro/go-zero/core/logx"
)

// CreatorMetricsLogic 创作者查看自己作品的指标（http-api.md #7，作者本人校验在 Interaction RPC）。
type CreatorMetricsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreatorMetricsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreatorMetricsLogic {
	return &CreatorMetricsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// CreatorMetrics 查询指标：viewer_id 取自 JWT（客户端传的一律忽略）。
func (l *CreatorMetricsLogic) CreatorMetrics(req *types.GetCreatorMetricsReq) (*types.GetCreatorMetricsResp, error) {
	me := middleware.MustGetUserID(l.ctx)
	if me == 0 {
		return nil, errorx.New(errorx.Unauthorized)
	}
	if req.FeedId <= 0 {
		return nil, errorx.New(errorx.ParamError)
	}

	resp, err := l.svcCtx.InteractionRpc.GetCreatorMetrics(l.ctx, &interactionpb.GetCreatorMetricsReq{
		FeedId:   req.FeedId,
		ViewerId: me,
		Window:   req.Window,
	})
	if err != nil {
		return nil, err
	}
	m := resp.Metrics
	if m == nil {
		return &types.GetCreatorMetricsResp{}, nil
	}
	return &types.GetCreatorMetricsResp{Metrics: &types.FeedMetrics{
		FeedId:       m.FeedId,
		Window:       m.Window,
		PublishedAt:  m.PublishedAt,
		DataComplete: m.DataComplete,
		Raw:          mapRaw(m.Raw),
		Rate:         mapRate(m.Rate),
	}}, nil
}

func mapRaw(r *interactionpb.FeedMetricsRaw) *types.FeedMetricsRaw {
	if r == nil {
		return &types.FeedMetricsRaw{}
	}
	return &types.FeedMetricsRaw{
		Expose:          r.Expose,
		Play:            r.Play,
		EffectivePlay:   r.EffectivePlay,
		Finish:          r.Finish,
		Skip:            r.Skip,
		WatchDurationMs: r.WatchDurationMs,
		Like:            r.Like,
		Collect:         r.Collect,
		Comment:         r.Comment,
		Share:           r.Share,
	}
}

func mapRate(r *interactionpb.FeedMetricsRate) *types.FeedMetricsRate {
	if r == nil {
		return &types.FeedMetricsRate{}
	}
	return &types.FeedMetricsRate{
		PlayRate:          r.PlayRate,
		EffectivePlayRate: r.EffectivePlayRate,
		FinishRate:        r.FinishRate,
		SkipRate:          r.SkipRate,
		AvgWatchMs:        r.AvgWatchMs,
		LikeRate:          r.LikeRate,
		CollectRate:       r.CollectRate,
		CommentRate:       r.CommentRate,
		ShareRate:         r.ShareRate,
	}
}
