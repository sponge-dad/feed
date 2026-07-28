// batchGetCommentCountLogic.go
//
// 职责：批量帖子评论总数查询（供 Feed 列表聚合，避免调用方 N+1）。
// 先批量读 Redis（MGET），缺失部分一条 GROUP BY SQL 回源并逐个回写缓存。
// 规范见 docs/design/comment/05-stats.md。
package logic

import (
	"context"
	"strconv"
	"time"

	"github.com/sponge-dad/feed/app/comment/rpc/comment"
	"github.com/sponge-dad/feed/app/comment/rpc/internal/keys"
	"github.com/sponge-dad/feed/app/comment/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

type BatchGetCommentCountLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewBatchGetCommentCountLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BatchGetCommentCountLogic {
	return &BatchGetCommentCountLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// BatchGetCommentCount 批量返回帖子评论总数；无评论的帖子计为 0。
func (l *BatchGetCommentCountLogic) BatchGetCommentCount(in *comment.BatchGetCommentCountReq) (*comment.BatchGetCommentCountResp, error) {
	if len(in.FeedIds) == 0 || len(in.FeedIds) > maxBatchFeedIds {
		return nil, errorx.New(errorx.ParamError)
	}

	counts := make(map[int64]int64, len(in.FeedIds))

	// 1. 批量读缓存（MGET）；Redis 异常时全部回源 MySQL（性能降级不出错）
	cacheKeys := make([]string, 0, len(in.FeedIds))
	for _, feedID := range in.FeedIds {
		cacheKeys = append(cacheKeys, keys.CommentCount(feedID))
	}
	vals, err := l.svcCtx.Redis.MgetCtx(l.ctx, cacheKeys...)
	if err != nil {
		l.Errorf("redis mget comment_count failed err=%v", err)
		vals = make([]string, len(in.FeedIds))
	}

	missIds := make([]uint64, 0, len(in.FeedIds))
	seen := make(map[int64]struct{}, len(in.FeedIds))
	for i, feedID := range in.FeedIds {
		if feedID <= 0 {
			return nil, errorx.New(errorx.ParamError)
		}
		if _, dup := seen[feedID]; dup {
			continue
		}
		seen[feedID] = struct{}{}
		if i < len(vals) && vals[i] != "" {
			if count, parseErr := strconv.ParseInt(vals[i], 10, 64); parseErr == nil {
				counts[feedID] = count
				continue
			}
		}
		missIds = append(missIds, uint64(feedID))
	}

	// 2. 缺失部分一条 GROUP BY SQL 回源；不在结果里的帖子无评论，计 0
	if len(missIds) > 0 {
		dbCounts, err := l.svcCtx.CommentModel.CountByFeedIds(l.ctx, missIds)
		if err != nil {
			return nil, err
		}
		for _, feedID := range missIds {
			count := dbCounts[feedID]
			counts[int64(feedID)] = count
			// 回写缓存失败不阻塞
			if setErr := l.svcCtx.Redis.SetexCtx(l.ctx, keys.CommentCount(int64(feedID)),
				strconv.FormatInt(count, 10), int(keys.CommentCountTTL/time.Second)); setErr != nil {
				l.Errorf("redis rebuild comment_count failed feedId=%d err=%v", feedID, setErr)
			}
		}
	}

	return &comment.BatchGetCommentCountResp{Counts: counts}, nil
}
