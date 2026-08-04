// getUserLikedFeedsLogic_test.go
//
// 职责：GetUserLikedFeeds / GetUserCollectedFeeds 单元测试。
// 覆盖游标分页（顺序、页数、同秒 tie-break）、取消后列表移除、冷 key 重建、非法游标。
package logic

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/keys"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedZSet 直接向 miniredis 写入用户点赞 ZSet 条目。
func seedZSet(t *testing.T, env *testEnv, key string, score int64, feedID int64) {
	t.Helper()
	_, err := env.svcCtx.Redis.Zadd(key, score, strconv.FormatInt(feedID, 10))
	require.NoError(t, err)
}

// TestGetUserLikedFeeds_Pagination 分页：每页 2 条遍历 5 条数据，顺序倒序、游标正确。
func TestGetUserLikedFeeds_Pagination(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	base := testTime().Unix()
	for i := 1; i <= 5; i++ {
		seedZSet(t, env, keys.UserLikes(1), base+int64(i), int64(100+i))
	}
	l := NewGetUserLikedFeedsLogic(ctx, env.svcCtx)

	// 第一页
	p1, err := l.GetUserLikedFeeds(&interaction.GetUserLikedFeedsReq{UserId: 1, PageSize: 2})
	require.NoError(t, err)
	assert.Equal(t, []int64{105, 104}, p1.FeedIds)
	assert.Equal(t, int64(5), p1.Total)
	require.NotEmpty(t, p1.NextCursor)

	// 第二页
	p2, err := l.GetUserLikedFeeds(&interaction.GetUserLikedFeedsReq{UserId: 1, PageSize: 2, Cursor: p1.NextCursor})
	require.NoError(t, err)
	assert.Equal(t, []int64{103, 102}, p2.FeedIds)
	require.NotEmpty(t, p2.NextCursor)

	// 第三页（最后一页，不满一页，next_cursor 为空）
	p3, err := l.GetUserLikedFeeds(&interaction.GetUserLikedFeedsReq{UserId: 1, PageSize: 2, Cursor: p2.NextCursor})
	require.NoError(t, err)
	assert.Equal(t, []int64{101}, p3.FeedIds)
	assert.Empty(t, p3.NextCursor)
}

// TestGetUserLikedFeeds_SameScoreTieBreak 同秒点赞多条：分页不丢条目、不重复
// （回归：Redis 同分是字典序，"9" 与 "10" 数值序与字典序相反）。
func TestGetUserLikedFeeds_SameScoreTieBreak(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	score := testTime().Unix()
	for _, feedID := range []int64{9, 10, 11} {
		seedZSet(t, env, keys.UserLikes(1), score, feedID)
	}
	l := NewGetUserLikedFeedsLogic(ctx, env.svcCtx)

	var got []int64
	cursor := ""
	for range [4]int{} {
		resp, err := l.GetUserLikedFeeds(&interaction.GetUserLikedFeedsReq{UserId: 1, PageSize: 2, Cursor: cursor})
		require.NoError(t, err)
		got = append(got, resp.FeedIds...)
		if resp.NextCursor == "" {
			break
		}
		cursor = resp.NextCursor
	}
	assert.Equal(t, []int64{11, 10, 9}, got, "同分条目应按 feed_id 数值降序且不丢不重")
}

// TestGetUserLikedFeeds_UnlikedRemoved 取消点赞后列表即时移除。
func TestGetUserLikedFeeds_UnlikedRemoved(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		_, err := NewLikeFeedLogic(ctx, env.svcCtx).LikeFeed(&interaction.LikeFeedReq{UserId: 1, FeedId: int64(100 + i)})
		require.NoError(t, err)
	}
	_, err := NewUnlikeFeedLogic(ctx, env.svcCtx).UnlikeFeed(&interaction.UnlikeFeedReq{UserId: 1, FeedId: 102})
	require.NoError(t, err)

	resp, err := NewGetUserLikedFeedsLogic(ctx, env.svcCtx).
		GetUserLikedFeeds(&interaction.GetUserLikedFeedsReq{UserId: 1, PageSize: 10})
	require.NoError(t, err)
	assert.Equal(t, int64(2), resp.Total)
	assert.NotContains(t, resp.FeedIds, int64(102), "取消的帖子不应出现在列表中")
}

// TestGetUserLikedFeeds_ColdKeyRebuild 冷 key 从 MySQL 重建列表。
func TestGetUserLikedFeeds_ColdKeyRebuild(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	base := testTime()
	env.likes.seed(1, 201, 1, base.Add(1*time.Minute))
	env.likes.seed(1, 202, 1, base.Add(2*time.Minute))
	env.likes.seed(1, 203, 2, base.Add(3*time.Minute)) // 已取消，不应出现

	resp, err := NewGetUserLikedFeedsLogic(ctx, env.svcCtx).
		GetUserLikedFeeds(&interaction.GetUserLikedFeedsReq{UserId: 1, PageSize: 10})
	require.NoError(t, err)
	assert.Equal(t, []int64{202, 201}, resp.FeedIds, "重建后按时间倒序，且不含已取消")
	assert.Equal(t, int64(2), resp.Total)
	assert.True(t, env.mr.Exists(keys.UserLikes(1)), "ZSet 应已重建")
}

// TestGetUserCollectedFeeds_Basic 收藏列表同构冒烟。
func TestGetUserCollectedFeeds_Basic(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	_, err := NewCollectFeedLogic(ctx, env.svcCtx).CollectFeed(&interaction.CollectFeedReq{UserId: 1, FeedId: 301})
	require.NoError(t, err)

	resp, err := NewGetUserCollectedFeedsLogic(ctx, env.svcCtx).
		GetUserCollectedFeeds(&interaction.GetUserCollectedFeedsReq{UserId: 1, PageSize: 10})
	require.NoError(t, err)
	assert.Equal(t, []int64{301}, resp.FeedIds)
	assert.Equal(t, int64(1), resp.Total)
}

// TestGetUserLikedFeeds_InvalidCursor 非法游标返回参数错误。
func TestGetUserLikedFeeds_InvalidCursor(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	seedZSet(t, env, keys.UserLikes(1), testTime().Unix(), 100)

	_, err := NewGetUserLikedFeedsLogic(ctx, env.svcCtx).
		GetUserLikedFeeds(&interaction.GetUserLikedFeedsReq{UserId: 1, PageSize: 2, Cursor: "!!!not-base64!!!"})
	requireBizCode(t, err, errorx.ParamError)
}
