// Package logic 实现 Feed 服务的所有 gRPC 业务逻辑。
// getfollowtimelinelogic.go 提供关注流查询，采用「推拉结合」混合模式：
// 普通好友帖子已由 worker 推入 inbox（读 inbox），关注的大V帖子实时拉取其 outbox，再合并去重排序。
package logic

import (
	"context"
	"math"

	"github.com/sponge-dad/feed/app/feed/model"
	"github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/keys"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/svc"
	"github.com/sponge-dad/feed/app/relation/rpc/relation"
	"github.com/sponge-dad/feed/common/errorx"
	"github.com/zeromicro/go-zero/core/logx"
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
//  2. 通过 RelationRpc.GetFollows 取关注列表，逐个 IsVip 识别大V，
//     对大V拉取其 outbox 最近 N 条（拉模式，保证大V内容实时可见且避免写放大）。
//  3. 合并去重，按 (score 秒级时间戳倒序, id 倒序) 排序。
//  4. 游标过滤（score<游标 或 同分且 id<游标）后截取本页，生成下一页游标。
//  5. 批量 FindByIds 回填详情。
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

	// 1. 候选合并：inbox（普通好友已推）+ 关注大V 的 outbox（实时拉）。
	candidates := make(map[int64]int64) // feedID -> 秒级 score

	// 1a. 收件箱：worker 已将普通好友帖子推入，按 score 倒序全量取出（上限内）。
	inboxPairs, err := rdb.ZrevrangebyscoreWithScoresAndLimitCtx(l.ctx, keys.Inbox(in.UserId), math.MinInt64, math.MaxInt64, 0, followInboxReadCap)
	if err != nil {
		return nil, err
	}
	for _, p := range inboxPairs {
		id, e := strconvParseFeedID(p.Key)
		if e != nil {
			continue
		}
		candidates[id] = p.Score
	}

	// 1b. 关注列表 + 大V识别，拉取大V发件箱最近 N 条。
	follows, err := l.svcCtx.RelationRpc.GetFollows(l.ctx, &relation.GetFollowsReq{
		UserId:   in.UserId,
		Page:     1,
		PageSize: 5000,
	})
	if err != nil {
		l.Errorf("GetFollowTimeline GetFollows failed userId=%d err=%v", in.UserId, err)
		return nil, err
	}
	bigVCount := 0
	for _, fid := range follows.FolloweeIds {
		if bigVCount >= followMaxBigV {
			// V1 限制拉取的大V数量，避免极端关注数下的读放大。
			break
		}
		vip, verr := l.svcCtx.RelationRpc.IsVip(l.ctx, &relation.IsVipReq{UserId: fid})
		if verr != nil {
			l.Errorf("GetFollowTimeline IsVip failed userId=%d err=%v", fid, verr)
			return nil, verr
		}
		if !vip.IsVip {
			continue
		}
		bigVCount++
		obPairs, oerr := rdb.ZrevrangebyscoreWithScoresAndLimitCtx(l.ctx, keys.Outbox(fid), math.MinInt64, math.MaxInt64, 0, followOutboxPullSize)
		if oerr != nil {
			return nil, oerr
		}
		for _, p := range obPairs {
			id, e := strconvParseFeedID(p.Key)
			if e != nil {
				continue
			}
			candidates[id] = p.Score
		}
	}

	// 2. 排序候选。
	items := make([]feedScore, 0, len(candidates))
	for id, sc := range candidates {
		items = append(items, feedScore{feedID: id, score: sc})
	}
	sortFeedScores(items)

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

	// 4. 批量回填详情，按 result 顺序保证时间线有序。
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
				briefs = append(briefs, toFeedBrief(f))
			}
		}
	}

	return &feed.GetFollowTimelineResp{
		Feeds: briefs,
		Page:  &feed.PageInfo{Cursor: nextCursor, HasMore: int64(len(result)) >= pageSize},
	}, nil
}
