// Package logic 实现 Feed 服务的所有 gRPC 业务逻辑。
// getfollowtimelinelogic.go 提供关注流查询，采用「推拉结合」混合模式：
// 普通好友帖子已由 worker 推入 inbox（读 inbox），关注的大V帖子实时拉取其 outbox，再合并去重排序。
package logic

import (
	"context"
	"math"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/sponge-dad/feed/app/feed/model"
	"github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/keys"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/svc"
	"github.com/sponge-dad/feed/app/relation/rpc/relation"
	"github.com/sponge-dad/feed/common/errorx"
	"github.com/sponge-dad/feed/common/requestid"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"golang.org/x/sync/errgroup"

	"github.com/sponge-dad/feed/app/feed/rpc/internal/logic/trace"
)

// GetFollowTimelineLogic 封装 GetFollowTimeline 请求所需的上下文与依赖。
type GetFollowTimelineLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

// NewGetFollowTimelineLogic 构造 GetFollowTimelineLogic。
func NewGetFollowTimelineLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetFollowTimelineLogic {
	return &GetFollowTimelineLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetFollowTimeline 查询当前用户的关注流（推拉结合）。
// 流程：
//  1. 收件箱 inbox:{user} 取普通好友已推送的帖子（worker 已写入）。
//  2. 通过 RelationRpc.GetFollows 取关注列表，一次 BatchIsVip 批量识别大V，
//     对大V拉取其 outbox 最近 N 条（拉模式，保证大V内容实时可见且避免写放大）。
//  3. 合并去重并标记来源（inbox → FOLLOW_INBOX；大V outbox → VIP_OUTBOX；
//     同 feed 多路命中以 FOLLOW_INBOX 优先），按 (score 秒级时间戳倒序, id 倒序) 排序。
//  4. 游标过滤（score<游标 或 同分且 id<游标）后截取本页，生成下一页游标。
//  5. 批量 FindByIds 回填详情，并将来源标记（FeedSource）写入 FeedBrief.Source。
//
// 阶段一补齐（见 02-request-trace）：当 inbox 读取为空且用户有关注关系时，
// 用 GetFollows + 各作者 outbox 兜底重建并回写 inbox，命中该重建路径的帖子标记为 INBOX_REBUILD。
func (l *GetFollowTimelineLogic) GetFollowTimeline(in *feed.GetFollowTimelineReq) (*feed.GetFollowTimelineResp, error) {
	if in.UserId <= 0 {
		return nil, errorx.New(errorx.ParamError)
	}
	pageSize := int64(in.PageSize)
	if pageSize <= 0 {
		pageSize = defaultUserFeedPageSize
	}
	if pageSize > maxUserFeedPageSize {
		pageSize = maxUserFeedPageSize
	}

	cursorScore, cursorID, ok := parseFollowCursor(in.Cursor)
	if !ok {
		return nil, errorx.New(errorx.ParamError)
	}

	rdb := l.svcCtx.Redis

	tb := trace.NewBuilder(requestid.FromContext(l.ctx), in.UserId, "follow", in.Cursor, int32(pageSize))

	// 1. 候选合并（带来源标记）：inbox（普通好友已推）+ 关注大V 的 outbox（实时拉）。
	candidates := make(map[int64]*feedScore) // feedID -> 候选（含来源，多路命中按优先级收敛）

	// 1a. 收件箱：worker 已将普通好友帖子推入，按 score 倒序全量取出（上限内）。
	inboxStart := time.Now()
	inboxPairs, err := rdb.ZrevrangebyscoreWithScoresAndLimitCtx(l.ctx, keys.Inbox(in.UserId), math.MinInt64, math.MaxInt64, 0, followInboxReadCap)
	if err != nil {
		return nil, err
	}
	tb.RecordSource(feedSourceFollowInbox.String(), int32(len(inboxPairs)), time.Since(inboxStart).Milliseconds())
	for _, p := range inboxPairs {
		id, e := strconvParseFeedID(p.Key)
		if e != nil {
			continue
		}
		mergeFeedScore(candidates, id, p.Score, feedSourceFollowInbox)
	}

	if len(inboxPairs) == 0 {
		// inbox 为空：尝试兜底重建（GetFollows + 各作者 outbox），命中标记 INBOX_REBUILD。
		// 重建失败不致命，仅缺失该部分数据。
		rebuildStart := time.Now()
		rebuildPairs, rerr := l.rebuildInbox(in.UserId)
		if rerr != nil {
			l.Errorf("GetFollowTimeline rebuildInbox failed userId=%d err=%v", in.UserId, rerr)
		}
		tb.RecordSource(feedSourceInboxRebuild.String(), int32(len(rebuildPairs)), time.Since(rebuildStart).Milliseconds())
		for _, p := range rebuildPairs {
			id, e := strconvParseFeedID(p.Key)
			if e != nil {
				continue
			}
			mergeFeedScore(candidates, id, p.Score, feedSourceInboxRebuild)
		}
	} else {
		// 1b. 关注列表 + 大V识别，拉取大V发件箱最近 N 条（拉模式，保证大V内容实时可见）。
		follows, err := l.svcCtx.RelationRpc.GetFollows(l.ctx, &relation.GetFollowsReq{
			UserId:   in.UserId,
			Page:     1,
			PageSize: 5000,
		})
		if err != nil {
			l.Errorf("GetFollowTimeline GetFollows failed userId=%d err=%v", in.UserId, err)
			return nil, err
		}
		// 一次 RPC 批量判定关注列表中的大V，消除逐个 IsVip 的 N+1 问题。
		vipResp, berr := l.svcCtx.RelationRpc.BatchIsVip(l.ctx, &relation.BatchIsVipReq{UserIds: follows.FolloweeIds})
		if berr != nil {
			l.Errorf("GetFollowTimeline BatchIsVip failed userId=%d err=%v", in.UserId, berr)
			return nil, berr
		}
		bigVCount := 0
		vipStart := time.Now()
		var vipOutboxTotal int32
		for _, fid := range follows.FolloweeIds {
			if bigVCount >= followMaxBigV {
				// V1 限制拉取的大V数量，避免极端关注数下的读放大。
				break
			}
			if !vipResp.Results[fid] {
				continue
			}
			bigVCount++
			obPairs, oerr := rdb.ZrevrangebyscoreWithScoresAndLimitCtx(l.ctx, keys.Outbox(fid), math.MinInt64, math.MaxInt64, 0, followOutboxPullSize)
			if oerr != nil {
				return nil, oerr
			}
			vipOutboxTotal += int32(len(obPairs))
			for _, p := range obPairs {
				id, e := strconvParseFeedID(p.Key)
				if e != nil {
					continue
				}
				mergeFeedScore(candidates, id, p.Score, feedSourceVipOutbox)
			}
		}
		tb.RecordSource(feedSourceVipOutbox.String(), vipOutboxTotal, time.Since(vipStart).Milliseconds())
	}

	// 2. 排序候选。
	items := make([]feedScore, 0, len(candidates))
	for _, c := range candidates {
		items = append(items, *c)
	}
	sortFeedScores(items)
	tb.SetMergedCount(int32(len(items)))

	// 3. 游标过滤并截取本页。
	result := make([]feedScore, 0, pageSize)
	for _, it := range items {
		if !beforeFollowCursor(it.score, it.feedID, cursorScore, cursorID) {
			continue
		}
		result = append(result, it)
		if int64(len(result)) >= pageSize {
			break
		}
	}

	var nextCursor string
	if len(result) > 0 {
		last := result[len(result)-1]
		nextCursor = encodeFollowCursor(last.score, last.feedID)
	}

	// 4. 批量回填详情，按 result 顺序保证时间线有序，并写入来源标记。
	var briefs []*feed.FeedBrief
	if len(result) > 0 {
		ids := make([]uint64, 0, len(result))
		for _, it := range result {
			ids = append(ids, uint64(it.feedID))
		}
		feeds, ferr := l.svcCtx.FeedModel.FindByIds(l.ctx, ids)
		if ferr != nil {
			return nil, ferr
		}
		byID := make(map[uint64]*model.Feeds, len(feeds))
		for _, f := range feeds {
			byID[f.Id] = f
		}
		briefs = make([]*feed.FeedBrief, 0, len(result))
		for _, it := range result {
			if f, ok := byID[uint64(it.feedID)]; ok {
				b := toFeedBrief(f)
				b.Source = int32(it.source)
				briefs = append(briefs, b)
				tb.AddItem(it.feedID, it.source.String(), int32(len(briefs)-1), it.score)
			}
		}
		tb.SetReturnedCount(int32(len(briefs)))
		tb.SetFilteredCount(int32(len(result) - len(briefs)))
	}

	// 5. 异步写入请求级 Trace（失败不阻塞主流程，见 02-request-trace §6）。
	go trace.Write(context.Background(), rdb, tb.Build(), l.svcCtx.Config.Trace.TTL, l.svcCtx.Config.Trace.SampleRate)

	return &feed.GetFollowTimelineResp{
		Feeds: briefs,
		Page:  &feed.PageInfo{Cursor: nextCursor, HasMore: int64(len(result)) >= pageSize},
	}, nil
}

// rebuildInbox 兜底重建收件箱：当 inbox 为空（首次进入/缓存过期）且用户有关注关系时，
// 并行拉取各关注作者的 outbox，按 score 去重后回写 inbox（便于后续请求命中推模式），
// 并返回重建得到的候选（供 GetFollowTimeline 标记为 INBOX_REBUILD）。
func (l *GetFollowTimelineLogic) rebuildInbox(userID int64) ([]redis.Pair, error) {
	follows, err := l.svcCtx.RelationRpc.GetFollows(l.ctx, &relation.GetFollowsReq{
		UserId:   userID,
		Page:     1,
		PageSize: 5000,
	})
	if err != nil {
		return nil, err
	}
	followees := follows.FolloweeIds
	if len(followees) == 0 {
		return nil, nil
	}

	rdb := l.svcCtx.Redis
	merged := make(map[int64]int64, len(followees)) // feedID -> 最大 score
	var mu sync.Mutex

	g, gctx := errgroup.WithContext(l.ctx)
	for _, fid := range followees {
		fid := fid
		g.Go(func() error {
			pairs, e := rdb.ZrevrangebyscoreWithScoresAndLimitCtx(gctx, keys.Outbox(fid), math.MinInt64, math.MaxInt64, 0, followOutboxPullSize)
			if e != nil {
				return e
			}
			mu.Lock()
			defer mu.Unlock()
			for _, p := range pairs {
				id, pe := strconvParseFeedID(p.Key)
				if pe != nil {
					continue
				}
				if cur, ok := merged[id]; !ok || p.Score > cur {
					merged[id] = p.Score
				}
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	if len(merged) == 0 {
		return nil, nil
	}

	// 收敛到上限内（按 score 倒序取前 followInboxReadCap 条），与正常读取上限对齐。
	type kv struct {
		id    int64
		score int64
	}
	ranked := make([]kv, 0, len(merged))
	for id, sc := range merged {
		ranked = append(ranked, kv{id: id, score: sc})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].id > ranked[j].id
	})
	if len(ranked) > followInboxReadCap {
		ranked = ranked[:followInboxReadCap]
	}

	// 回写 inbox（ZADD 幂等），便于后续请求命中推模式（FOLLOW_INBOX）。
	writePairs := make([]redis.Pair, 0, len(ranked))
	for _, r := range ranked {
		writePairs = append(writePairs, redis.Pair{Key: strconv.FormatInt(r.id, 10), Score: r.score})
	}
	if _, e := rdb.ZaddsCtx(l.ctx, keys.Inbox(userID), writePairs...); e != nil {
		l.Errorf("rebuildInbox write back inbox failed userId=%d err=%v", userID, e)
	}
	return writePairs, nil
}
