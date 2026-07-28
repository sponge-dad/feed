// batchGetFeedStatsLogic.go
//
// 职责：批量查询帖子互动计数（Feed 流卡片场景）。
// pipeline 批量读 feed:stats，未命中的帖子回源 MySQL GROUP BY 批量统计，
// 并用 HSETNX 重建缓存，详见 docs/design/interaction/04-stats.md §4。
package logic

import (
	"context"
	"strconv"
	"time"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/keys"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"

	red "github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
)

type BatchGetFeedStatsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewBatchGetFeedStatsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BatchGetFeedStatsLogic {
	return &BatchGetFeedStatsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// BatchGetFeedStats 批量查询帖子计数，返回顺序与请求一致；单次上限 100 个。
func (l *BatchGetFeedStatsLogic) BatchGetFeedStats(in *interaction.BatchGetFeedStatsReq) (*interaction.BatchGetFeedStatsResp, error) {
	feedIDs := in.GetFeedIds()
	if len(feedIDs) == 0 {
		return &interaction.BatchGetFeedStatsResp{}, nil
	}
	if len(feedIDs) > maxBatchSize {
		return nil, errorx.NewWithMsg(errorx.ParamError, "单次批量查询不超过 100 个帖子")
	}
	for _, id := range feedIDs {
		if id <= 0 {
			return nil, errorx.New(errorx.ParamError)
		}
	}

	// 1. pipeline 批量读缓存
	cmds := make(map[int64]*red.MapStringStringCmd, len(feedIDs))
	err := l.svcCtx.Redis.PipelinedCtx(l.ctx, func(pipe red.Pipeliner) error {
		for _, feedID := range feedIDs {
			if _, ok := cmds[feedID]; ok {
				continue // 去重，重复 feed_id 复用同一结果
			}
			cmds[feedID] = pipe.HGetAll(l.ctx, keys.FeedStats(feedID))
		}
		return nil
	})
	if err != nil {
		l.Errorf("BatchGetFeedStats: pipeline hgetall failed err=%v", err)
		return nil, errorx.New(errorx.ServerError)
	}

	type statPair struct{ like, collect int64 }
	stats := make(map[int64]statPair, len(feedIDs))
	var missed []int64
	for feedID, cmd := range cmds {
		fields := cmd.Val()
		likeRaw, hasLike := fields[keys.FieldLikeCount]
		collectRaw, hasCollect := fields[keys.FieldCollectCount]
		if hasLike && hasCollect {
			like, _ := strconv.ParseInt(likeRaw, 10, 64)
			collect, _ := strconv.ParseInt(collectRaw, 10, 64)
			stats[feedID] = statPair{like: like, collect: collect}
		} else {
			missed = append(missed, feedID)
		}
	}

	// 2. 未命中的批量回源 MySQL 并重建缓存（HSETNX 不覆盖并发增量；0 计数也缓存防穿透）
	if len(missed) > 0 {
		likeHelper := newInteractHelper(l.ctx, l.svcCtx, kindLike)
		collectHelper := newInteractHelper(l.ctx, l.svcCtx, kindCollect)
		likeCnts, err := likeHelper.countByFeeds(missed)
		if err != nil {
			l.Errorf("BatchGetFeedStats: count likes failed err=%v", err)
			return nil, errorx.New(errorx.ServerError)
		}
		collectCnts, err := collectHelper.countByFeeds(missed)
		if err != nil {
			l.Errorf("BatchGetFeedStats: count collections failed err=%v", err)
			return nil, errorx.New(errorx.ServerError)
		}
		if err = l.svcCtx.Redis.PipelinedCtx(l.ctx, func(pipe red.Pipeliner) error {
			for _, feedID := range missed {
				key := keys.FeedStats(feedID)
				pipe.HSetNX(l.ctx, key, keys.FieldLikeCount, strconv.FormatInt(likeCnts[feedID], 10))
				pipe.HSetNX(l.ctx, key, keys.FieldCollectCount, strconv.FormatInt(collectCnts[feedID], 10))
				pipe.Expire(l.ctx, key, time.Duration(keys.TTLFeedStats)*time.Second)
			}
			return nil
		}); err != nil {
			// 缓存重建失败不阻塞主流程，仅记日志
			l.Errorf("BatchGetFeedStats: rebuild stats cache failed err=%v", err)
		}
		for _, feedID := range missed {
			stats[feedID] = statPair{like: likeCnts[feedID], collect: collectCnts[feedID]}
		}
	}

	// 3. 按请求顺序组装
	list := make([]*interaction.FeedStats, 0, len(feedIDs))
	for _, feedID := range feedIDs {
		s := stats[feedID]
		list = append(list, &interaction.FeedStats{
			FeedId:       feedID,
			LikeCount:    clampNonNegative(s.like),
			CollectCount: clampNonNegative(s.collect),
		})
	}
	return &interaction.BatchGetFeedStatsResp{StatsList: list}, nil
}
