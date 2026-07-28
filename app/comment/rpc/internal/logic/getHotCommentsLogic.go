// getHotCommentsLogic.go
//
// 职责：帖子热门评论 Top-K（仅一级评论，按 like_count 降序）。
// 优先读 comment_hot:{feed_id} ZSet（TTL 5min）；未命中按 like_count 从 MySQL
// 重建 ZSet 后返回。ZSet 中可能残留已删评论 ID，取详情时按 status=1 过滤。
// 规范见 docs/design/comment/05-stats.md 第 4 节。
package logic

import (
	"context"
	"strconv"

	"github.com/sponge-dad/feed/app/comment/model"
	"github.com/sponge-dad/feed/app/comment/rpc/comment"
	"github.com/sponge-dad/feed/app/comment/rpc/internal/keys"
	"github.com/sponge-dad/feed/app/comment/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

type GetHotCommentsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetHotCommentsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetHotCommentsLogic {
	return &GetHotCommentsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetHotComments 返回帖子热门一级评论 Top-K，按 like_count 降序。
func (l *GetHotCommentsLogic) GetHotComments(in *comment.GetHotCommentsReq) (*comment.GetHotCommentsResp, error) {
	if in.FeedId <= 0 {
		return nil, errorx.New(errorx.ParamError)
	}
	limit := in.Limit
	if limit <= 0 {
		limit = defaultHotLimit
	} else if limit > maxHotLimit {
		limit = maxHotLimit
	}

	// 1. 优先读 comment_hot ZSet；Redis 异常时直接回源 MySQL（性能降级不出错）
	var commentIds []uint64
	pairs, err := l.svcCtx.Redis.ZrevrangeWithScoresCtx(l.ctx, keys.CommentHot(in.FeedId), 0, limit-1)
	if err != nil {
		l.Errorf("redis zrevrange comment_hot failed feedId=%d err=%v", in.FeedId, err)
	}
	for _, p := range pairs {
		if id, parseErr := strconv.ParseUint(p.Key, 10, 64); parseErr == nil {
			commentIds = append(commentIds, id)
		}
	}

	var rows []*model.Comments
	if len(commentIds) > 0 {
		// FindByIds 只返回 status=1 的评论，天然过滤 ZSet 中残留的已删 ID
		rows, err = l.svcCtx.CommentModel.FindByIds(l.ctx, commentIds)
		if err != nil {
			return nil, err
		}
		rows = sortByIdOrder(rows, commentIds)
	} else {
		// 2. 缓存未命中：按 like_count 从 MySQL 重建
		rows, err = l.svcCtx.CommentModel.FindTopRootsByLike(l.ctx, uint64(in.FeedId), uint64(limit))
		if err != nil {
			return nil, err
		}
		l.rebuildHotCache(in.FeedId, rows)
	}

	infos := make([]*comment.CommentInfo, 0, len(rows))
	for _, r := range rows {
		infos = append(infos, toCommentInfo(r))
	}
	fillUserInfos(l.ctx, l.svcCtx, infos)

	return &comment.GetHotCommentsResp{Comments: infos}, nil
}

// sortByIdOrder 按 ZSet 返回的热度顺序重排 DB 查询结果（IN 查询不保序）。
func sortByIdOrder(rows []*model.Comments, order []uint64) []*model.Comments {
	byID := make(map[uint64]*model.Comments, len(rows))
	for _, r := range rows {
		byID[r.Id] = r
	}
	sorted := make([]*model.Comments, 0, len(rows))
	for _, id := range order {
		if r, ok := byID[id]; ok {
			sorted = append(sorted, r)
		}
	}
	return sorted
}

// rebuildHotCache 用 Top-K 一级评论重建 comment_hot ZSet（TTL 5min）；失败仅记日志。
func (l *GetHotCommentsLogic) rebuildHotCache(feedID int64, rows []*model.Comments) {
	if len(rows) == 0 {
		return
	}
	key := keys.CommentHot(feedID)
	pairs := make([]redis.Pair, 0, len(rows))
	for _, r := range rows {
		pairs = append(pairs, redis.Pair{Key: strconv.FormatUint(r.Id, 10), Score: int64(r.LikeCount)})
	}
	if _, err := l.svcCtx.Redis.ZaddsCtx(l.ctx, key, pairs...); err != nil {
		l.Errorf("redis rebuild comment_hot failed feedId=%d err=%v", feedID, err)
		return
	}
	if err := l.svcCtx.Redis.ExpireCtx(l.ctx, key, commentHotZsetTL); err != nil {
		l.Errorf("redis expire comment_hot failed feedId=%d err=%v", feedID, err)
	}
}
