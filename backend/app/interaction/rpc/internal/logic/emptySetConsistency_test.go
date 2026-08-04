// emptySetConsistency_test.go
//
// 职责：点赞/收藏「空集合被 Redis 删除 → 误判冷缓存 → 回源读到 MQ 未消费的旧 MySQL 状态」
// 竞态的稳定复现与修复回归测试（哨兵方案，见 keys.SetSentinel 与 interactionHelper.go）。
//
// 竞态链路：
//
//	取消点赞 → Set 最后一个成员被移除 → Redis 自动删除空 Set key
//	→ 状态查询发现 key 不存在 → 误判缓存未加载 → 回源 MySQL
//	→ unlike 事件尚未被 MQ 消费 → MySQL 仍是旧状态 → 错误返回 true
//
// 时序控制：单测环境中 MQ 事件由 stubPublisher 捕获但不消费，
// MySQL 桩状态只能由测试代码显式推进，因此「Redis 已修改但 MQ 未落库」
// 的窗口完全可控，不依赖任何 time.Sleep。
package logic

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/keys"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Baseline: I-EMPTY-01
// TestGetUserInteractionStatus_UnlikeEmptySetBeforeMQConsume_ReturnsFalse
// 单用户首次点赞后取消：Set 变空、MQ unlike 未消费、MySQL 仍是旧点赞状态，
// 此时立即查询必须返回 false（以 Redis 为准），不得回源 MySQL 误报 true。
func TestGetUserInteractionStatus_UnlikeEmptySetBeforeMQConsume_ReturnsFalse(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	// MySQL 历史状态：用户 1 已点赞 feed 100（模拟 like 事件已落库）
	env.likes.seed(1, 100, statusLiked, testTime())

	// 首次查询触发缓存重建：Redis Set = {sentinel, "1"}
	resp, err := NewGetUserInteractionStatusLogic(ctx, env.svcCtx).
		GetUserInteractionStatus(&interaction.GetUserInteractionStatusReq{UserId: 1, FeedId: 100})
	require.NoError(t, err)
	require.True(t, resp.Status.IsLiked, "前置：重建后应为已点赞")

	// 取消点赞：Redis 先行移除最后一个真实成员
	_, err = NewUnlikeFeedLogic(ctx, env.svcCtx).UnlikeFeed(&interaction.UnlikeFeedReq{UserId: 1, FeedId: 100})
	require.NoError(t, err)

	// 竞态窗口断言：Set 真实成员为空但 key 因哨兵仍存在（未被 Redis 删除）
	require.True(t, env.mr.Exists(keys.LikeFeed(100)),
		"空集合 key 必须因哨兵成员而保留，否则将被误判为冷缓存")
	members, err := env.svcCtx.Redis.Smembers(keys.LikeFeed(100))
	require.NoError(t, err)
	assert.Equal(t, []string{keys.SetSentinel}, members, "取消后仅剩哨兵成员")

	// MQ unlike 事件已发出但尚未消费：MySQL 桩仍是旧状态 liked
	events := env.pub.all()
	require.Len(t, events, 1, "unlike 事件已发出")
	row, err := env.likes.findOne(1, 100)
	require.NoError(t, err)
	require.EqualValues(t, statusLiked, row.status, "前置：MySQL 仍是未消费的旧状态")

	dbCallsBefore := env.likes.feedUserCalls

	// 竞态窗口内立即查询：必须返回 false 且不回源
	resp, err = NewGetUserInteractionStatusLogic(ctx, env.svcCtx).
		GetUserInteractionStatus(&interaction.GetUserInteractionStatusReq{UserId: 1, FeedId: 100})
	require.NoError(t, err)
	assert.False(t, resp.Status.IsLiked,
		"MQ 未消费窗口内查询必须以 Redis 为准返回 false，不得回源旧 MySQL 状态")
	assert.Equal(t, dbCallsBefore, env.likes.feedUserCalls,
		"已加载的空集合不得再次回源 MySQL")

	// 模拟 MQ 消费完成：MySQL 收敛为已取消
	ok, err := env.likes.updateStatusIfNewer(row.id, 2, time.Now())
	require.NoError(t, err)
	require.True(t, ok)

	// 模拟缓存过期后重建：最终结果仍为 false
	env.mr.Del(keys.LikeFeed(100))
	resp, err = NewGetUserInteractionStatusLogic(ctx, env.svcCtx).
		GetUserInteractionStatus(&interaction.GetUserInteractionStatusReq{UserId: 1, FeedId: 100})
	require.NoError(t, err)
	assert.False(t, resp.Status.IsLiked, "MQ 消费完成、缓存重建后最终状态为 false")
}

// Baseline: I-EMPTY-02
// TestGetUserInteractionStatus_UncollectEmptySetBeforeMQConsume_ReturnsFalse
// 收藏对称场景：取消收藏后空集合窗口内查询不得误报 is_collected=true。
func TestGetUserInteractionStatus_UncollectEmptySetBeforeMQConsume_ReturnsFalse(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	env.collects.seed(1, 200, statusLiked, testTime())

	resp, err := NewGetUserInteractionStatusLogic(ctx, env.svcCtx).
		GetUserInteractionStatus(&interaction.GetUserInteractionStatusReq{UserId: 1, FeedId: 200})
	require.NoError(t, err)
	require.True(t, resp.Status.IsCollected, "前置：重建后应为已收藏")

	_, err = NewUncollectFeedLogic(ctx, env.svcCtx).UncollectFeed(&interaction.UncollectFeedReq{UserId: 1, FeedId: 200})
	require.NoError(t, err)

	require.True(t, env.mr.Exists(keys.CollectFeed(200)), "空收藏集合 key 必须保留")
	members, err := env.svcCtx.Redis.Smembers(keys.CollectFeed(200))
	require.NoError(t, err)
	assert.Equal(t, []string{keys.SetSentinel}, members)

	// MySQL 桩仍是旧状态（uncollect 事件未消费）
	row, err := env.collects.findOne(1, 200)
	require.NoError(t, err)
	require.EqualValues(t, statusLiked, row.status)

	resp, err = NewGetUserInteractionStatusLogic(ctx, env.svcCtx).
		GetUserInteractionStatus(&interaction.GetUserInteractionStatusReq{UserId: 1, FeedId: 200})
	require.NoError(t, err)
	assert.False(t, resp.Status.IsCollected, "取消收藏后窗口内查询必须返回 false")

	// MQ 消费 + 缓存过期重建后收敛
	_, err = env.collects.updateStatusIfNewer(row.id, 2, time.Now())
	require.NoError(t, err)
	env.mr.Del(keys.CollectFeed(200))
	resp, err = NewGetUserInteractionStatusLogic(ctx, env.svcCtx).
		GetUserInteractionStatus(&interaction.GetUserInteractionStatusReq{UserId: 1, FeedId: 200})
	require.NoError(t, err)
	assert.False(t, resp.Status.IsCollected)
}

// Baseline: I-EMPTY-03
// TestEnsureSet_EmptyAndNonEmptyRebuild_Distinguished
// 缓存未加载（回源）与已加载空集合两种状态必须可区分：
// 空数据重建后 key 存在（仅哨兵）；非空数据重建后 key 含哨兵 + 真实成员。
func TestEnsureSet_EmptyAndNonEmptyRebuild_Distinguished(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	// 场景 A：MySQL 无记录 → 空集合重建
	respA, err := NewGetUserInteractionStatusLogic(ctx, env.svcCtx).
		GetUserInteractionStatus(&interaction.GetUserInteractionStatusReq{UserId: 1, FeedId: 300})
	require.NoError(t, err)
	assert.False(t, respA.Status.IsLiked)
	membersA, err := env.svcCtx.Redis.Smembers(keys.LikeFeed(300))
	require.NoError(t, err)
	assert.Equal(t, []string{keys.SetSentinel}, membersA, "空重建：仅哨兵")

	// 场景 B：MySQL 有记录 → 非空重建
	env.likes.seed(7, 301, statusLiked, testTime())
	respB, err := NewGetUserInteractionStatusLogic(ctx, env.svcCtx).
		GetUserInteractionStatus(&interaction.GetUserInteractionStatusReq{UserId: 7, FeedId: 301})
	require.NoError(t, err)
	assert.True(t, respB.Status.IsLiked)
	membersB, err := env.svcCtx.Redis.Smembers(keys.LikeFeed(301))
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{keys.SetSentinel, "7"}, membersB, "非空重建：哨兵 + 真实成员")

	// 哨兵不影响 SISMEMBER 语义：非点赞用户查询为 false
	ok, err := env.svcCtx.Redis.Sismember(keys.LikeFeed(301), "8")
	require.NoError(t, err)
	assert.False(t, ok)
}

// Baseline: I-EMPTY-04
// TestGetUserInteractionStatus_ConcurrentColdQuery_NoRepeatedBackfill
// 并发冷缓存查询：首查建立空集合标记后，后续查询不得重复回源（防击穿）。
func TestGetUserInteractionStatus_ConcurrentColdQuery_NoRepeatedBackfill(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	// 并发冷查询：结果必须全部为 false 且无错误
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := NewGetUserInteractionStatusLogic(ctx, env.svcCtx).
				GetUserInteractionStatus(&interaction.GetUserInteractionStatusReq{UserId: 1, FeedId: 400})
			assert.NoError(t, err)
			if resp != nil {
				assert.False(t, resp.Status.IsLiked)
			}
		}()
	}
	wg.Wait()

	// 空集合标记已建立后，后续查询不再回源
	env.likes.mu.Lock()
	callsAfterWarm := env.likes.feedUserCalls
	env.likes.mu.Unlock()

	for i := 0; i < 5; i++ {
		_, err := NewGetUserInteractionStatusLogic(ctx, env.svcCtx).
			GetUserInteractionStatus(&interaction.GetUserInteractionStatusReq{UserId: 1, FeedId: 400})
		require.NoError(t, err)
	}
	env.likes.mu.Lock()
	callsFinal := env.likes.feedUserCalls
	env.likes.mu.Unlock()
	assert.Equal(t, callsAfterWarm, callsFinal, "空集合标记建立后不得再回源 MySQL")
}

// Baseline: I-EMPTY-05
// TestEnsureSet_EmptyRebuild_HasTTL 空集合哨兵标记必须携带 TTL，可通过过期自愈。
func TestEnsureSet_EmptyRebuild_HasTTL(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	_, err := NewGetUserInteractionStatusLogic(ctx, env.svcCtx).
		GetUserInteractionStatus(&interaction.GetUserInteractionStatusReq{UserId: 1, FeedId: 500})
	require.NoError(t, err)

	likeTTL := env.mr.TTL(keys.LikeFeed(500))
	collectTTL := env.mr.TTL(keys.CollectFeed(500))
	assert.Greater(t, int64(likeTTL), int64(0), "like 空集合标记必须有 TTL")
	assert.LessOrEqual(t, int64(likeTTL), int64(keys.TTLFeedSet)*int64(time.Second))
	assert.Greater(t, int64(collectTTL), int64(0), "collect 空集合标记必须有 TTL")

	// 过期后重建路径仍正确写入 loaded 状态
	env.mr.FastForward(time.Duration(keys.TTLFeedSet+1) * time.Second)
	require.False(t, env.mr.Exists(keys.LikeFeed(500)))
	_, err = NewGetUserInteractionStatusLogic(ctx, env.svcCtx).
		GetUserInteractionStatus(&interaction.GetUserInteractionStatusReq{UserId: 1, FeedId: 500})
	require.NoError(t, err)
	assert.True(t, env.mr.Exists(keys.LikeFeed(500)), "过期后重建应重新写入哨兵标记")
}

// Baseline: I-EMPTY-06
// TestFeedStats_SentinelNotCounted 哨兵成员不得计入点赞/收藏计数。
func TestFeedStats_SentinelNotCounted(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	// 点赞 → 取消 → 集合仅剩哨兵，计数必须为 0
	_, err := NewLikeFeedLogic(ctx, env.svcCtx).LikeFeed(&interaction.LikeFeedReq{UserId: 1, FeedId: 600})
	require.NoError(t, err)
	_, err = NewUnlikeFeedLogic(ctx, env.svcCtx).UnlikeFeed(&interaction.UnlikeFeedReq{UserId: 1, FeedId: 600})
	require.NoError(t, err)

	stats, err := NewGetFeedStatsLogic(ctx, env.svcCtx).
		GetFeedStats(&interaction.GetFeedStatsReq{FeedId: 600})
	require.NoError(t, err)
	assert.EqualValues(t, 0, stats.Stats.LikeCount, "哨兵不得计入 like_count")
	assert.EqualValues(t, 0, stats.Stats.CollectCount)
}
