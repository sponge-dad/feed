// getUserInteractionStatusLogic_test.go
//
// 职责：GetUserInteractionStatus / BatchGetUserInteractionStatus 单元测试。
// 覆盖 docs/design/interaction/08-test-strategy.md §3.3 用例，
// 以及"批量未命中不回填部分 Set"回归用例。
package logic

import (
	"context"
	"testing"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/keys"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetUserInteractionStatus_Liked 已点赞返回 is_liked=true。
func TestGetUserInteractionStatus_Liked(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	_, err := NewLikeFeedLogic(ctx, env.svcCtx).LikeFeed(&interaction.LikeFeedReq{UserId: 1, FeedId: 100})
	require.NoError(t, err)

	resp, err := NewGetUserInteractionStatusLogic(ctx, env.svcCtx).
		GetUserInteractionStatus(&interaction.GetUserInteractionStatusReq{UserId: 1, FeedId: 100})
	require.NoError(t, err)
	assert.True(t, resp.Status.IsLiked)
	assert.False(t, resp.Status.IsCollected)
}

// TestGetUserInteractionStatus_NoRecord 未互动且 MySQL 无记录：返回 false，
// 并写入仅含哨兵的「已加载空集合」标记（防止后续并发查询重复回源击穿）。
func TestGetUserInteractionStatus_NoRecord(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	resp, err := NewGetUserInteractionStatusLogic(ctx, env.svcCtx).
		GetUserInteractionStatus(&interaction.GetUserInteractionStatusReq{UserId: 1, FeedId: 100})
	require.NoError(t, err)
	assert.False(t, resp.Status.IsLiked)
	assert.False(t, resp.Status.IsCollected)

	// 空集合以哨兵形式落 Redis：key 存在、仅含哨兵、带 TTL
	for _, key := range []string{keys.LikeFeed(100), keys.CollectFeed(100)} {
		require.True(t, env.mr.Exists(key), "空集合应写入已加载标记 key: %s", key)
		members, err := env.svcCtx.Redis.Smembers(key)
		require.NoError(t, err)
		assert.Equal(t, []string{keys.SetSentinel}, members, "空集合只含哨兵成员")
		assert.Greater(t, int64(env.mr.TTL(key)), int64(0), "空集合标记必须有 TTL")
	}
}

// TestGetUserInteractionStatus_AfterUnlike 已取消：取消时即时移除 Set，返回 false。
func TestGetUserInteractionStatus_AfterUnlike(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	_, err := NewLikeFeedLogic(ctx, env.svcCtx).LikeFeed(&interaction.LikeFeedReq{UserId: 1, FeedId: 100})
	require.NoError(t, err)
	_, err = NewUnlikeFeedLogic(ctx, env.svcCtx).UnlikeFeed(&interaction.UnlikeFeedReq{UserId: 1, FeedId: 100})
	require.NoError(t, err)

	resp, err := NewGetUserInteractionStatusLogic(ctx, env.svcCtx).
		GetUserInteractionStatus(&interaction.GetUserInteractionStatusReq{UserId: 1, FeedId: 100})
	require.NoError(t, err)
	assert.False(t, resp.Status.IsLiked)
}

// TestGetUserInteractionStatus_ColdKeyRebuild 冷 key 回源重建后正确判定状态。
func TestGetUserInteractionStatus_ColdKeyRebuild(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	env.likes.seed(1, 100, 1, testTime())
	env.likes.seed(2, 100, 1, testTime())

	resp, err := NewGetUserInteractionStatusLogic(ctx, env.svcCtx).
		GetUserInteractionStatus(&interaction.GetUserInteractionStatusReq{UserId: 1, FeedId: 100})
	require.NoError(t, err)
	assert.True(t, resp.Status.IsLiked)

	// 重建后 Set 应完整包含所有点赞用户
	ok, err := env.svcCtx.Redis.Sismember(keys.LikeFeed(100), "2")
	require.NoError(t, err)
	assert.True(t, ok)
}

// TestBatchGetUserInteractionStatus 批量状态：命中走 Redis，未命中回源 MySQL 且不回填部分 Set。
func TestBatchGetUserInteractionStatus(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	// f1：Redis 命中（通过正常点赞链路建立）
	_, err := NewLikeFeedLogic(ctx, env.svcCtx).LikeFeed(&interaction.LikeFeedReq{UserId: 1, FeedId: 1})
	require.NoError(t, err)
	// f2：Redis 冷，MySQL 中已点赞（历史数据）
	env.likes.seed(1, 2, 1, testTime())
	// f3：已取消状态
	env.likes.seed(1, 3, 2, testTime())
	// f4：收藏
	_, err = NewCollectFeedLogic(ctx, env.svcCtx).CollectFeed(&interaction.CollectFeedReq{UserId: 1, FeedId: 4})
	require.NoError(t, err)

	// 清掉 f2 可能存在的 key，确保走 MySQL 回源分支
	env.mr.Del(keys.LikeFeed(2))

	resp, err := NewBatchGetUserInteractionStatusLogic(ctx, env.svcCtx).
		BatchGetUserInteractionStatus(&interaction.BatchGetUserInteractionStatusReq{
			UserId: 1, FeedIds: []int64{1, 2, 3, 4, 5},
		})
	require.NoError(t, err)
	require.Len(t, resp.StatusList, 5)

	assert.True(t, resp.StatusList[0].IsLiked, "f1 命中 Redis")
	assert.True(t, resp.StatusList[1].IsLiked, "f2 回源 MySQL")
	assert.False(t, resp.StatusList[2].IsLiked, "f3 已取消")
	assert.False(t, resp.StatusList[3].IsLiked)
	assert.True(t, resp.StatusList[3].IsCollected, "f4 已收藏")
	assert.False(t, resp.StatusList[4].IsLiked, "f5 无记录")

	// 回归：批量回源不得回填单成员，避免制造毒化其他用户查询的部分 Set
	assert.False(t, env.mr.Exists(keys.LikeFeed(2)), "批量未命中不应回填部分 Set")
}
