// stats_test.go
//
// 职责：GetCommentCount / BatchGetCommentCount / GetHotComments 单元测试。
// 覆盖 Cache-Aside 命中与重建一致性、批量计数混合命中、无评论帖计 0、
// 热门 ZSet 命中排序 / 已删过滤 / 未命中按 like_count 重建
// （见 docs/design/comment/08-test-strategy.md）。
package logic

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sponge-dad/feed/app/comment/rpc/comment"
	"github.com/sponge-dad/feed/app/comment/rpc/internal/keys"
	"github.com/sponge-dad/feed/common/errorx"
)

// TestGetCommentCount_CacheAside 未命中回源 MySQL 并回写；命中不再查库。
func TestGetCommentCount_CacheAside(t *testing.T) {
	m := newStubCommentsModel()
	seedFeed(t, m) // 8 条可见
	svcCtx := newTestSvc(t, m, &stubUserRpc{}, &stubFeedRpc{})
	l := NewGetCommentCountLogic(context.Background(), svcCtx)

	// 首次：miss -> COUNT -> 回写
	resp, err := l.GetCommentCount(&comment.GetCommentCountReq{FeedId: 100})
	require.NoError(t, err)
	require.EqualValues(t, 8, resp.Count)
	require.Equal(t, 1, m.countByFeedCalls)
	val, err := svcCtx.Redis.Get(keys.CommentCount(100))
	require.NoError(t, err)
	require.Equal(t, "8", val)

	// 二次：命中缓存，不再回源
	resp, err = l.GetCommentCount(&comment.GetCommentCountReq{FeedId: 100})
	require.NoError(t, err)
	require.EqualValues(t, 8, resp.Count)
	require.Equal(t, 1, m.countByFeedCalls)
}

// TestBatchGetCommentCount 混合命中：缓存命中 + 回源 + 无评论计 0。
func TestBatchGetCommentCount(t *testing.T) {
	m := newStubCommentsModel()
	seedFeed(t, m)
	svcCtx := newTestSvc(t, m, &stubUserRpc{}, &stubFeedRpc{})
	// feed=100 预置缓存值（模拟命中路径优先于 DB）
	require.NoError(t, svcCtx.Redis.Set(keys.CommentCount(100), "42"))
	// feed=200 有 1 条评论但无缓存；feed=300 无评论
	require.NoError(t, m.InsertComment(context.Background(), mkComment(50, 200, 1, 0, 0, time.Now())))

	l := NewBatchGetCommentCountLogic(context.Background(), svcCtx)
	resp, err := l.BatchGetCommentCount(&comment.BatchGetCommentCountReq{FeedIds: []int64{100, 200, 300}})
	require.NoError(t, err)
	require.EqualValues(t, 42, resp.Counts[100]) // 缓存命中
	require.EqualValues(t, 1, resp.Counts[200])  // 回源
	require.EqualValues(t, 0, resp.Counts[300])  // 无评论计 0

	// 回源部分已回写缓存
	val, err := svcCtx.Redis.Get(keys.CommentCount(200))
	require.NoError(t, err)
	require.Equal(t, "1", val)

	// 参数校验：空列表 / 超上限
	_, err = l.BatchGetCommentCount(&comment.BatchGetCommentCountReq{FeedIds: nil})
	requireBizCode(t, err, errorx.ParamError)
	tooMany := make([]int64, maxBatchFeedIds+1)
	for i := range tooMany {
		tooMany[i] = int64(i + 1)
	}
	_, err = l.BatchGetCommentCount(&comment.BatchGetCommentCountReq{FeedIds: tooMany})
	requireBizCode(t, err, errorx.ParamError)
}

// TestGetHotComments_ZsetHit ZSet 命中：按热度排序返回，已删 ID 被过滤。
func TestGetHotComments_ZsetHit(t *testing.T) {
	m := newStubCommentsModel()
	seedFeed(t, m)
	svcCtx := newTestSvc(t, m, &stubUserRpc{}, &stubFeedRpc{})

	// ZSet：id=2 热度最高，id=1 次之；再塞一个已删成员 id=999
	_, err := svcCtx.Redis.Zadd(keys.CommentHot(100), 50, "2")
	require.NoError(t, err)
	_, err = svcCtx.Redis.Zadd(keys.CommentHot(100), 10, "1")
	require.NoError(t, err)
	_, err = svcCtx.Redis.Zadd(keys.CommentHot(100), 99, "999")
	require.NoError(t, err)

	l := NewGetHotCommentsLogic(context.Background(), svcCtx)
	resp, err := l.GetHotComments(&comment.GetHotCommentsReq{FeedId: 100, Limit: 3})
	require.NoError(t, err)
	// 999 不存在于 DB（视同已删）被过滤；剩余按 ZSet 热度序
	require.Len(t, resp.Comments, 2)
	require.EqualValues(t, 2, resp.Comments[0].CommentId)
	require.EqualValues(t, 1, resp.Comments[1].CommentId)
}

// TestGetHotComments_RebuildOnMiss ZSet 未命中：按 like_count 从 DB 重建并回写。
func TestGetHotCommentsRebuildOnMiss(t *testing.T) {
	m := newStubCommentsModel()
	seedFeed(t, m)
	// 设置点赞数：楼 2 最热
	require.NoError(t, m.UpdateLikeCount(context.Background(), 2, 100))
	require.NoError(t, m.UpdateLikeCount(context.Background(), 3, 30))
	svcCtx := newTestSvc(t, m, &stubUserRpc{}, &stubFeedRpc{})

	l := NewGetHotCommentsLogic(context.Background(), svcCtx)
	resp, err := l.GetHotComments(&comment.GetHotCommentsReq{FeedId: 100, Limit: 2})
	require.NoError(t, err)
	require.Len(t, resp.Comments, 2)
	require.EqualValues(t, 2, resp.Comments[0].CommentId)
	require.EqualValues(t, 3, resp.Comments[1].CommentId)

	// ZSet 已重建
	score, err := svcCtx.Redis.Zscore(keys.CommentHot(100), "2")
	require.NoError(t, err)
	require.EqualValues(t, 100, score)
}
