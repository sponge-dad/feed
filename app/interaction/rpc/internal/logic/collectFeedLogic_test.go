// collectFeedLogic_test.go
//
// 职责：CollectFeed / UncollectFeed 单元测试。
// 收藏与点赞同构，这里覆盖基本链路 + 幂等 + 与点赞计数互不干扰。
package logic

import (
	"context"
	"testing"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/keys"
	event "github.com/sponge-dad/feed/common/event/interaction"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCollectFeed_Basic 首次收藏 + 重复收藏幂等 + 取消收藏。
func TestCollectFeed_Basic(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	_, err := NewCollectFeedLogic(ctx, env.svcCtx).CollectFeed(&interaction.CollectFeedReq{UserId: 1, FeedId: 100})
	require.NoError(t, err)
	_, err = NewCollectFeedLogic(ctx, env.svcCtx).CollectFeed(&interaction.CollectFeedReq{UserId: 1, FeedId: 100})
	require.NoError(t, err)

	collected, err := env.svcCtx.Redis.Sismember(keys.CollectFeed(100), "1")
	require.NoError(t, err)
	assert.True(t, collected)
	assert.Equal(t, "1", env.mr.HGet(keys.FeedStats(100), keys.FieldCollectCount), "重复收藏计数不变")

	events := env.pub.all()
	require.Len(t, events, 1)
	assert.Equal(t, event.ActionCollect, events[0].ActionType)

	_, err = NewUncollectFeedLogic(ctx, env.svcCtx).UncollectFeed(&interaction.UncollectFeedReq{UserId: 1, FeedId: 100})
	require.NoError(t, err)
	assert.Equal(t, "0", env.mr.HGet(keys.FeedStats(100), keys.FieldCollectCount))
	events = env.pub.all()
	require.Len(t, events, 2)
	assert.Equal(t, event.ActionUncollect, events[1].ActionType)
}

// TestCollectFeed_IndependentFromLike 点赞与收藏计数互不影响。
func TestCollectFeed_IndependentFromLike(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	_, err := NewLikeFeedLogic(ctx, env.svcCtx).LikeFeed(&interaction.LikeFeedReq{UserId: 1, FeedId: 100})
	require.NoError(t, err)
	_, err = NewCollectFeedLogic(ctx, env.svcCtx).CollectFeed(&interaction.CollectFeedReq{UserId: 1, FeedId: 100})
	require.NoError(t, err)
	_, err = NewUncollectFeedLogic(ctx, env.svcCtx).UncollectFeed(&interaction.UncollectFeedReq{UserId: 1, FeedId: 100})
	require.NoError(t, err)

	assert.Equal(t, "1", env.mr.HGet(keys.FeedStats(100), keys.FieldLikeCount), "取消收藏不应影响点赞计数")
	assert.Equal(t, "0", env.mr.HGet(keys.FeedStats(100), keys.FieldCollectCount))
}
