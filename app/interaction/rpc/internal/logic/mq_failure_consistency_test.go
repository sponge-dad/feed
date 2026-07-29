// mq_failure_consistency_test.go
//
// 职责：固化「RocketMQ 不可用」时互动写路径的当前一致性行为（Risk baseline: R-P0-2）。
// 场景：Redis 先行写成功，但 interaction-event 发送失败（或 Producer 未配置）。
// 当前实现（interactionHelper.publish）：MQ 失败只记日志、不回滚 Redis、不重试、无补偿，
// 接口仍返回成功 —— 事件永久丢失，MySQL 侧计数依赖 TTL 过期回源收敛。
// 本文件测试用于固化该行为基线，不代表该行为是理想设计。
package logic

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/keys"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingPublisher 模拟 RocketMQ 不可用：每次 SendSync 均返回错误，并记录调用次数。
type failingPublisher struct {
	mu    sync.Mutex
	calls int
}

func (p *failingPublisher) SendSync(_ string, _ []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return errors.New("rocketmq: broker unavailable")
}

func (p *failingPublisher) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// TestLikeFeed_WhenMQSendFails_CurrentlyKeepsRedisStateAndReturnsSuccess
// Risk baseline: R-P0-2
// 点赞：Redis 写成功 + interaction-event 发送失败 → 接口仍成功、Redis 状态保留、
// 事件丢失（Producer 被调用 1 次但无任何补偿/重试），MySQL 无落库。
func TestLikeFeed_WhenMQSendFails_CurrentlyKeepsRedisStateAndReturnsSuccess(t *testing.T) {
	env := newTestEnv(t)
	pub := &failingPublisher{}
	env.svcCtx.Producer = pub

	const (
		userID int64 = 101
		feedID int64 = 20001
	)

	resp, err := NewLikeFeedLogic(context.Background(), env.svcCtx).LikeFeed(
		&interaction.LikeFeedReq{UserId: userID, FeedId: feedID})

	// 当前行为：MQ 失败不影响接口返回。
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Redis 先行写已生效：点赞集合、用户列表、计数全部就位。
	isMember, err := env.svcCtx.Redis.Sismember(keys.LikeFeed(feedID), strconv.FormatInt(userID, 10))
	require.NoError(t, err)
	assert.True(t, isMember, "like:feed 集合应包含该用户")

	score, err := env.svcCtx.Redis.Zscore(keys.UserLikes(userID), strconv.FormatInt(feedID, 10))
	require.NoError(t, err)
	assert.Greater(t, score, int64(0), "user:likes ZSet 应包含该帖子")

	likeCount, err := env.svcCtx.Redis.Hget(keys.FeedStats(feedID), keys.FieldLikeCount)
	require.NoError(t, err)
	assert.Equal(t, "1", likeCount, "feed:stats like_count 应为 1")

	// 事件确实尝试发送了 1 次，但失败后无重试、无补偿 → 消息永久丢失。
	assert.Equal(t, 1, pub.callCount(), "Producer 应被调用 1 次且无重试")

	// MySQL 落库依赖消费者消费该事件，事件丢失 → likes 表无记录（当前一致性缺口）。
	assert.Empty(t, env.likes.rows, "MQ 事件丢失时 MySQL 不会有落库记录")
}

// TestCollectFeed_WhenMQSendFails_CurrentlyKeepsRedisStateAndReturnsSuccess
// Risk baseline: R-P0-2
// 收藏：Redis 写成功 + interaction-event 发送失败 → 接口仍成功、Redis 状态保留、事件丢失。
func TestCollectFeed_WhenMQSendFails_CurrentlyKeepsRedisStateAndReturnsSuccess(t *testing.T) {
	env := newTestEnv(t)
	pub := &failingPublisher{}
	env.svcCtx.Producer = pub

	const (
		userID int64 = 202
		feedID int64 = 20002
	)

	resp, err := NewCollectFeedLogic(context.Background(), env.svcCtx).CollectFeed(
		&interaction.CollectFeedReq{UserId: userID, FeedId: feedID})

	require.NoError(t, err)
	require.NotNil(t, resp)

	isMember, err := env.svcCtx.Redis.Sismember(keys.CollectFeed(feedID), strconv.FormatInt(userID, 10))
	require.NoError(t, err)
	assert.True(t, isMember, "collect:feed 集合应包含该用户")

	score, err := env.svcCtx.Redis.Zscore(keys.UserCollects(userID), strconv.FormatInt(feedID, 10))
	require.NoError(t, err)
	assert.Greater(t, score, int64(0), "user:collects ZSet 应包含该帖子")

	collectCount, err := env.svcCtx.Redis.Hget(keys.FeedStats(feedID), keys.FieldCollectCount)
	require.NoError(t, err)
	assert.Equal(t, "1", collectCount, "feed:stats collect_count 应为 1")

	assert.Equal(t, 1, pub.callCount(), "Producer 应被调用 1 次且无重试")
	assert.Empty(t, env.collects.rows, "MQ 事件丢失时 MySQL 不会有落库记录")
}

// TestLikeFeed_WhenProducerNotConfigured_CurrentlyReturnsSuccessAndDropsEvent
// Risk baseline: R-P0-2
// Producer 未配置（nil）：publish 直接记日志丢弃事件，接口仍成功、Redis 状态保留。
func TestLikeFeed_WhenProducerNotConfigured_CurrentlyReturnsSuccessAndDropsEvent(t *testing.T) {
	env := newTestEnv(t)
	env.svcCtx.Producer = nil

	const (
		userID int64 = 303
		feedID int64 = 20003
	)

	resp, err := NewLikeFeedLogic(context.Background(), env.svcCtx).LikeFeed(
		&interaction.LikeFeedReq{UserId: userID, FeedId: feedID})

	require.NoError(t, err)
	require.NotNil(t, resp)

	isMember, err := env.svcCtx.Redis.Sismember(keys.LikeFeed(feedID), strconv.FormatInt(userID, 10))
	require.NoError(t, err)
	assert.True(t, isMember, "Producer 为 nil 时 Redis 写仍应生效")
}
