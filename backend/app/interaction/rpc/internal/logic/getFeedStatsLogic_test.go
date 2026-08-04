// getFeedStatsLogic_test.go
//
// 职责：GetFeedStats / BatchGetFeedStats 单元测试。
// 覆盖 docs/design/interaction/08-test-strategy.md §3.2 全部用例，
// 以及"stats 重建用 HSETNX 不覆盖并发增量"回归用例。
package logic

import (
	"context"
	"testing"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/keys"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetFeedStats_CacheHit 缓存命中：直接返回 Redis 值，不回源 MySQL。
func TestGetFeedStats_CacheHit(t *testing.T) {
	env := newTestEnv(t)
	env.mr.HSet(keys.FeedStats(100), keys.FieldLikeCount, "5")
	env.mr.HSet(keys.FeedStats(100), keys.FieldCollectCount, "2")

	resp, err := NewGetFeedStatsLogic(context.Background(), env.svcCtx).
		GetFeedStats(&interaction.GetFeedStatsReq{FeedId: 100})
	require.NoError(t, err)
	assert.Equal(t, int64(5), resp.Stats.LikeCount)
	assert.Equal(t, int64(2), resp.Stats.CollectCount)
	assert.Zero(t, env.likes.countCalls, "缓存命中不应回源")
	assert.Zero(t, env.collects.countCalls)
}

// TestGetFeedStats_CacheMiss 缓存未命中：回源 MySQL 并回写缓存。
func TestGetFeedStats_CacheMiss(t *testing.T) {
	env := newTestEnv(t)
	env.likes.seed(1, 100, 1, testTime())
	env.likes.seed(2, 100, 1, testTime())
	env.collects.seed(3, 100, 1, testTime())

	resp, err := NewGetFeedStatsLogic(context.Background(), env.svcCtx).
		GetFeedStats(&interaction.GetFeedStatsReq{FeedId: 100})
	require.NoError(t, err)
	assert.Equal(t, int64(2), resp.Stats.LikeCount)
	assert.Equal(t, int64(1), resp.Stats.CollectCount)

	assert.Equal(t, "2", env.mr.HGet(keys.FeedStats(100), keys.FieldLikeCount), "应回写缓存")
	assert.Greater(t, int64(env.mr.TTL(keys.FeedStats(100))), int64(0), "回写应带 TTL")
}

// TestGetFeedStats_ZeroCached 计数为 0 也缓存，避免反复回源（防穿透）。
func TestGetFeedStats_ZeroCached(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	l := NewGetFeedStatsLogic(ctx, env.svcCtx)

	resp, err := l.GetFeedStats(&interaction.GetFeedStatsReq{FeedId: 100})
	require.NoError(t, err)
	assert.Zero(t, resp.Stats.LikeCount)
	firstCalls := env.likes.countCalls
	require.Positive(t, firstCalls)

	_, err = l.GetFeedStats(&interaction.GetFeedStatsReq{FeedId: 100})
	require.NoError(t, err)
	assert.Equal(t, firstCalls, env.likes.countCalls, "0 计数已缓存，二次查询不应回源")
}

// TestGetFeedStats_PartialFieldRebuild 回归：Hash 只有 like_count（HINCRBY 部分写）时，
// 重建只补缺失字段，不得覆盖已有的增量值。
func TestGetFeedStats_PartialFieldRebuild(t *testing.T) {
	env := newTestEnv(t)
	env.mr.HSet(keys.FeedStats(100), keys.FieldLikeCount, "7") // Redis 先行的增量值
	env.collects.seed(1, 100, 1, testTime())                   // MySQL 收藏 1 条

	resp, err := NewGetFeedStatsLogic(context.Background(), env.svcCtx).
		GetFeedStats(&interaction.GetFeedStatsReq{FeedId: 100})
	require.NoError(t, err)
	assert.Equal(t, int64(7), resp.Stats.LikeCount, "HSETNX 不得覆盖已有 like_count")
	assert.Equal(t, int64(1), resp.Stats.CollectCount, "缺失的 collect_count 应回源补齐")
}

// TestBatchGetFeedStats_PartialHit 批量查询部分命中：命中直接返回，未命中批量回源并回写。
func TestBatchGetFeedStats_PartialHit(t *testing.T) {
	env := newTestEnv(t)
	// f1 缓存命中
	env.mr.HSet(keys.FeedStats(1), keys.FieldLikeCount, "10")
	env.mr.HSet(keys.FeedStats(1), keys.FieldCollectCount, "3")
	// f2 冷，MySQL 有 1 赞
	env.likes.seed(9, 2, 1, testTime())

	resp, err := NewBatchGetFeedStatsLogic(context.Background(), env.svcCtx).
		BatchGetFeedStats(&interaction.BatchGetFeedStatsReq{FeedIds: []int64{1, 2, 3}})
	require.NoError(t, err)
	require.Len(t, resp.StatsList, 3)

	assert.Equal(t, int64(1), resp.StatsList[0].FeedId, "返回顺序应与请求一致")
	assert.Equal(t, int64(10), resp.StatsList[0].LikeCount)
	assert.Equal(t, int64(1), resp.StatsList[1].LikeCount)
	assert.Zero(t, resp.StatsList[2].LikeCount)

	assert.Equal(t, "1", env.mr.HGet(keys.FeedStats(2), keys.FieldLikeCount), "未命中的应回写缓存")
	assert.Equal(t, "0", env.mr.HGet(keys.FeedStats(3), keys.FieldLikeCount), "0 计数也应缓存")
}

// TestBatchGetFeedStats_Limit 批量上限校验与空请求。
func TestBatchGetFeedStats_Limit(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	l := NewBatchGetFeedStatsLogic(ctx, env.svcCtx)

	resp, err := l.BatchGetFeedStats(&interaction.BatchGetFeedStatsReq{})
	require.NoError(t, err)
	assert.Empty(t, resp.StatsList)

	big := make([]int64, maxBatchSize+1)
	for i := range big {
		big[i] = int64(i + 1)
	}
	_, err = l.BatchGetFeedStats(&interaction.BatchGetFeedStatsReq{FeedIds: big})
	requireBizCode(t, err, errorx.ParamError)
}
