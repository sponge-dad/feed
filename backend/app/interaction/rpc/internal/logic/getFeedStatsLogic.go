// getFeedStatsLogic.go
//
// 职责：查询单条帖子互动计数。Cache-Aside：feed:stats Hash 未命中时回源 MySQL COUNT
// 并用 HSETNX 重建（不覆盖并发增量），详见 docs/design/interaction/04-stats.md。
package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/keys"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetFeedStatsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetFeedStatsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetFeedStatsLogic {
	return &GetFeedStatsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetFeedStats 查询帖子点赞/收藏计数。计数为 0 也会缓存，防止穿透。
func (l *GetFeedStatsLogic) GetFeedStats(in *interaction.GetFeedStatsReq) (*interaction.GetFeedStatsResp, error) {
	if in.GetFeedId() <= 0 {
		return nil, errorx.New(errorx.ParamError)
	}
	h := newInteractHelper(l.ctx, l.svcCtx, kindLike)
	if err := h.ensureStats(in.GetFeedId()); err != nil {
		l.Errorf("GetFeedStats: ensure stats failed feed=%d err=%v", in.GetFeedId(), err)
		return nil, errorx.New(errorx.ServerError)
	}
	likeCnt, err := h.currentStats(in.GetFeedId(), keys.FieldLikeCount)
	if err != nil {
		l.Errorf("GetFeedStats: read stats failed feed=%d err=%v", in.GetFeedId(), err)
		return nil, errorx.New(errorx.ServerError)
	}
	collectCnt, err := h.currentStats(in.GetFeedId(), keys.FieldCollectCount)
	if err != nil {
		l.Errorf("GetFeedStats: read stats failed feed=%d err=%v", in.GetFeedId(), err)
		return nil, errorx.New(errorx.ServerError)
	}
	return &interaction.GetFeedStatsResp{
		Stats: &interaction.FeedStats{
			FeedId:       in.GetFeedId(),
			LikeCount:    clampNonNegative(likeCnt),
			CollectCount: clampNonNegative(collectCnt),
		},
	}, nil
}

// clampNonNegative 读级非负保护：异常负数按 0 返回（写路径已有保护，这里兜底展示层）。
func clampNonNegative(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}
