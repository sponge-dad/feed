// likeFeedLogic_test.go
//
// 职责：LikeFeed / UnlikeFeed 单元测试。
// 覆盖 docs/design/interaction/08-test-strategy.md §3.1 全部用例，
// 以及"冷 key 先重建再增量"回归用例（见 interactionHelper.go 文件头）。
package logic

import (
	"context"
	"strconv"
	"sync"
	"testing"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/keys"
	"github.com/sponge-dad/feed/common/errorx"
	event "github.com/sponge-dad/feed/common/event/interaction"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLikeFeed_First 首次点赞：Set 新增成员、计数 +1、发送 MQ 事件。
func TestLikeFeed_First(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	_, err := NewLikeFeedLogic(ctx, env.svcCtx).LikeFeed(&interaction.LikeFeedReq{UserId: 1, FeedId: 100})
	require.NoError(t, err)

	liked, err := env.svcCtx.Redis.Sismember(keys.LikeFeed(100), "1")
	require.NoError(t, err)
	assert.True(t, liked, "Set 应包含点赞用户")

	assert.Equal(t, "1", env.mr.HGet(keys.FeedStats(100), keys.FieldLikeCount), "like_count 应为 1")

	events := env.pub.all()
	require.Len(t, events, 1)
	assert.Equal(t, event.ActionLike, events[0].ActionType)
	assert.Equal(t, int64(1), events[0].UserID)
	assert.Equal(t, int64(100), events[0].FeedID)

	// 三个 key 均应带 TTL（可通过过期自愈）
	assert.Greater(t, int64(env.mr.TTL(keys.LikeFeed(100))), int64(0))
	assert.Greater(t, int64(env.mr.TTL(keys.UserLikes(1))), int64(0))
	assert.Greater(t, int64(env.mr.TTL(keys.FeedStats(100))), int64(0))
}

// TestLikeFeed_Duplicate 重复点赞：幂等成功、计数不变、不重复发事件。
func TestLikeFeed_Duplicate(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	l := NewLikeFeedLogic(ctx, env.svcCtx)

	_, err := l.LikeFeed(&interaction.LikeFeedReq{UserId: 1, FeedId: 100})
	require.NoError(t, err)
	_, err = l.LikeFeed(&interaction.LikeFeedReq{UserId: 1, FeedId: 100})
	require.NoError(t, err)

	assert.Equal(t, "1", env.mr.HGet(keys.FeedStats(100), keys.FieldLikeCount), "重复点赞计数不变")
	assert.Len(t, env.pub.all(), 1, "重复点赞不重复发事件")
}

// TestUnlikeFeed_Normal 取消已点赞：Set 移除成员、计数 -1、发送取消事件。
func TestUnlikeFeed_Normal(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	_, err := NewLikeFeedLogic(ctx, env.svcCtx).LikeFeed(&interaction.LikeFeedReq{UserId: 1, FeedId: 100})
	require.NoError(t, err)
	_, err = NewUnlikeFeedLogic(ctx, env.svcCtx).UnlikeFeed(&interaction.UnlikeFeedReq{UserId: 1, FeedId: 100})
	require.NoError(t, err)

	liked, err := env.svcCtx.Redis.Sismember(keys.LikeFeed(100), "1")
	require.NoError(t, err)
	assert.False(t, liked)
	assert.Equal(t, "0", env.mr.HGet(keys.FeedStats(100), keys.FieldLikeCount))

	events := env.pub.all()
	require.Len(t, events, 2)
	assert.Equal(t, event.ActionUnlike, events[1].ActionType)
}

// TestUnlikeFeed_Idempotent 重复取消：幂等成功、计数不变、不发事件。
func TestUnlikeFeed_Idempotent(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	_, err := NewUnlikeFeedLogic(ctx, env.svcCtx).UnlikeFeed(&interaction.UnlikeFeedReq{UserId: 1, FeedId: 100})
	require.NoError(t, err)

	assert.Empty(t, env.pub.all(), "未点赞时取消不应发事件")
	assert.Equal(t, "0", env.mr.HGet(keys.FeedStats(100), keys.FieldLikeCount), "计数保持 0")
}

// TestUnlikeFeed_ZeroCount 计数已为 0 时取消：不执行 HINCRBY -1，计数不为负。
func TestUnlikeFeed_ZeroCount(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	// 构造异常态：Set 有成员但计数已被外部置 0
	_, err := env.svcCtx.Redis.Sadd(keys.LikeFeed(100), "1")
	require.NoError(t, err)
	env.mr.HSet(keys.FeedStats(100), keys.FieldLikeCount, "0")
	env.mr.HSet(keys.FeedStats(100), keys.FieldCollectCount, "0")

	_, err = NewUnlikeFeedLogic(ctx, env.svcCtx).UnlikeFeed(&interaction.UnlikeFeedReq{UserId: 1, FeedId: 100})
	require.NoError(t, err)

	assert.Equal(t, "0", env.mr.HGet(keys.FeedStats(100), keys.FieldLikeCount), "计数不能为负")
	require.Len(t, env.pub.all(), 1, "确实移除了成员，应发取消事件")
}

// TestLikeFeed_ColdKeyRebuild 回归：冷 key（缓存过期）点赞时先回源重建再增量，
// 计数不能从 1 起步，老点赞用户不能丢失。
func TestLikeFeed_ColdKeyRebuild(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	// MySQL 已有用户 2、3 点赞，Redis 为空（模拟 key 过期）
	env.likes.seed(2, 100, 1, testTime())
	env.likes.seed(3, 100, 1, testTime())

	_, err := NewLikeFeedLogic(ctx, env.svcCtx).LikeFeed(&interaction.LikeFeedReq{UserId: 1, FeedId: 100})
	require.NoError(t, err)

	assert.Equal(t, "3", env.mr.HGet(keys.FeedStats(100), keys.FieldLikeCount), "重建后计数应为 3 而非 1")
	for _, member := range []string{"1", "2", "3"} {
		ok, err := env.svcCtx.Redis.Sismember(keys.LikeFeed(100), member)
		require.NoError(t, err)
		assert.True(t, ok, "成员 %s 不应丢失", member)
	}
}

// TestUnlikeFeed_ColdKey 回归：冷 key 取消点赞不能因 SREM 返回 0 而丢失取消事件。
func TestUnlikeFeed_ColdKey(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	// MySQL 中用户 1 已点赞，Redis 为空（模拟 key 过期）
	env.likes.seed(1, 100, 1, testTime())

	_, err := NewUnlikeFeedLogic(ctx, env.svcCtx).UnlikeFeed(&interaction.UnlikeFeedReq{UserId: 1, FeedId: 100})
	require.NoError(t, err)

	events := env.pub.all()
	require.Len(t, events, 1, "冷 key 取消必须发出取消事件")
	assert.Equal(t, event.ActionUnlike, events[0].ActionType)
	assert.Equal(t, "0", env.mr.HGet(keys.FeedStats(100), keys.FieldLikeCount), "计数应从重建值 1 减到 0")

	liked, err := env.svcCtx.Redis.Sismember(keys.LikeFeed(100), "1")
	require.NoError(t, err)
	assert.False(t, liked)
}

// TestLikeFeed_ConcurrentFlipConsistency 回归：同一用户并发点赞/取消时，
// 「集合翻转 + 计数增减」必须原子执行（Lua），否则非负保护会读到中间态，
// 造成计数与集合基数漂移（集成并发测试曾复现计数=2）。
func TestLikeFeed_ConcurrentFlipConsistency(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			var err error
			if n%2 == 0 {
				_, err = NewLikeFeedLogic(ctx, env.svcCtx).LikeFeed(&interaction.LikeFeedReq{UserId: 1, FeedId: 100})
			} else {
				_, err = NewUnlikeFeedLogic(ctx, env.svcCtx).UnlikeFeed(&interaction.UnlikeFeedReq{UserId: 1, FeedId: 100})
			}
			assert.NoError(t, err)
		}(i)
	}
	wg.Wait()

	// 集合含哨兵成员，真实成员数需剔除哨兵后统计
	members, err := env.svcCtx.Redis.Smembers(keys.LikeFeed(100))
	require.NoError(t, err)
	var card int64
	for _, m := range members {
		if m != keys.SetSentinel {
			card++
		}
	}
	assert.Contains(t, []int64{0, 1}, card, "单用户集合真实基数只能是 0 或 1")

	cnt := env.mr.HGet(keys.FeedStats(100), keys.FieldLikeCount)
	assert.Equal(t, strconv.FormatInt(card, 10), cnt, "计数必须与集合真实基数一致（不含哨兵）")
}

// TestLikeFeed_ParamError 参数校验：非法 user_id / feed_id 返回参数错误。
func TestLikeFeed_ParamError(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	_, err := NewLikeFeedLogic(ctx, env.svcCtx).LikeFeed(&interaction.LikeFeedReq{UserId: 0, FeedId: 100})
	requireBizCode(t, err, errorx.ParamError)

	_, err = NewUnlikeFeedLogic(ctx, env.svcCtx).UnlikeFeed(&interaction.UnlikeFeedReq{UserId: 1, FeedId: 0})
	requireBizCode(t, err, errorx.ParamError)
}
