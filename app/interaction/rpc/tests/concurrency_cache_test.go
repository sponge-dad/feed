// concurrency_cache_test.go
//
// 职责：Interaction 服务并发与缓存一致性集成测试（08-test-strategy §5）。
// 依赖 integration_test.go 中 TestMain 装配的真实服务 + MySQL/Redis 环境：
//   - 多用户并发点赞同一帖子：计数 == 用户数，Redis 与 MySQL 最终一致；
//   - 同一用户并发点赞/取消：计数只能是 0 或 1，与互动状态自洽；
//   - 缓存重建与并发写竞争：重建（HSETNX 保护）不覆盖并发增量，最终计数准确。
package tests

import (
	"context"
	"sync"
	"testing"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/keys"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConcurrency_MultiUserLikeSameFeed 50 个用户并发点赞同一帖子：
// Redis 计数精确等于用户数；异步落库后 MySQL 有效行数一致（§5.2）。
func TestConcurrency_MultiUserLikeSameFeed(t *testing.T) {
	requireEnv(t)
	ctx := context.Background()
	feed := nextID()
	const users = 50
	userIDs := make([]int64, users)
	for i := range userIDs {
		userIDs[i] = nextID()
	}

	var wg sync.WaitGroup
	for _, u := range userIDs {
		wg.Add(1)
		go func(uid int64) {
			defer wg.Done()
			_, err := testClient.LikeFeed(ctx, &interaction.LikeFeedReq{UserId: uid, FeedId: feed})
			assert.NoError(t, err)
		}(u)
	}
	wg.Wait()

	assert.Equal(t, int64(users), getStats(t, feed).LikeCount)

	// 异步落库最终一致：likes 表 status=1 行数 == 用户数
	waitRowCount(t, "likes", feed, users)
}

// TestConcurrency_SameUserLikeUnlike 同一用户并发交替点赞/取消：
// 无论调度顺序，最终计数只能是 0 或 1，且与互动状态自洽（§5.1）。
func TestConcurrency_SameUserLikeUnlike(t *testing.T) {
	requireEnv(t)
	ctx := context.Background()
	user, feed := nextID(), nextID()

	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			var err error
			if n%2 == 0 {
				_, err = testClient.LikeFeed(ctx, &interaction.LikeFeedReq{UserId: user, FeedId: feed})
			} else {
				_, err = testClient.UnlikeFeed(ctx, &interaction.UnlikeFeedReq{UserId: user, FeedId: feed})
			}
			assert.NoError(t, err)
		}(i)
	}
	wg.Wait()

	stats := getStats(t, feed)
	assert.Contains(t, []int64{0, 1}, stats.LikeCount, "单用户计数只能是 0 或 1")

	// 计数与状态自洽
	status := getStatus(t, user, feed)
	assert.Equal(t, stats.LikeCount == 1, status.IsLiked, "计数与互动状态应一致")

	// 集合真实基数与计数一致（防止 Set 与 Hash 漂移）。
	// Set 携带 keys.SetSentinel「已加载」哨兵成员，统计时必须剔除，
	// 禁止直接用 SCARD 作为计数（见 keys.SetSentinel 注释）。
	members, err := testCtx.Redis.SmembersCtx(ctx, keys.LikeFeed(feed))
	require.NoError(t, err)
	var card int64
	for _, m := range members {
		if m != keys.SetSentinel {
			card++
		}
	}
	assert.Equal(t, stats.LikeCount, card)
}

// TestConcurrency_RebuildVsWrite 缓存重建与并发写竞争：
// 预置 20 个点赞后删除计数缓存，随后「并发读触发重建」与「30 个新用户点赞」同时进行；
// HSETNX 重建不得覆盖并发增量，最终计数应精确等于 50（§5.3，回归「重建覆盖增量」缺陷）。
func TestConcurrency_RebuildVsWrite(t *testing.T) {
	requireEnv(t)
	ctx := context.Background()
	feed := nextID()
	const preUsers, newUsers = 20, 30

	for i := 0; i < preUsers; i++ {
		_, err := testClient.LikeFeed(ctx, &interaction.LikeFeedReq{UserId: nextID(), FeedId: feed})
		require.NoError(t, err)
	}
	// 等待预置点赞全部落库，保证重建回源结果确定为 preUsers
	waitRowCount(t, "likes", feed, preUsers)

	// 删除计数缓存，制造冷 key
	_, err := testCtx.Redis.DelCtx(ctx, keys.FeedStats(feed))
	require.NoError(t, err)

	var wg sync.WaitGroup
	// 并发读：触发计数重建
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := testClient.GetFeedStats(ctx, &interaction.GetFeedStatsReq{FeedId: feed})
			assert.NoError(t, err)
		}()
	}
	// 并发写：新增点赞
	for i := 0; i < newUsers; i++ {
		wg.Add(1)
		go func(uid int64) {
			defer wg.Done()
			_, err := testClient.LikeFeed(ctx, &interaction.LikeFeedReq{UserId: uid, FeedId: feed})
			assert.NoError(t, err)
		}(nextID())
	}
	wg.Wait()

	assert.Equal(t, int64(preUsers+newUsers), getStats(t, feed).LikeCount,
		"重建不应覆盖并发写入的增量")
	waitRowCount(t, "likes", feed, preUsers+newUsers)
}
