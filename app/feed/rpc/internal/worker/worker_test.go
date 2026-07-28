package worker

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/keys"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/svc"
	"github.com/sponge-dad/feed/app/relation/rpc/relationclient"
	feedEvent "github.com/sponge-dad/feed/common/event/feed"
	"github.com/stretchr/testify/assert"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"google.golang.org/grpc"
)

// stubRelation 实现 relationclient.Relation，仅 GetFans 返回预设粉丝列表，其余方法测试中不调用。
type stubRelation struct {
	relationclient.Relation
	fans []int64
}

func (s *stubRelation) GetFans(ctx context.Context, in *relationclient.GetFansReq, opts ...grpc.CallOption) (*relationclient.GetFansResp, error) {
	return &relationclient.GetFansResp{FollowerIds: s.fans}, nil
}

// newTestWorker 使用 miniredis 与 Relation 桩构造 Worker，不依赖真实存储。
func newTestWorker(t *testing.T, fans []int64) *Worker {
	t.Helper()
	mr, err := miniredis.Run()
	assert.NoError(t, err)
	t.Cleanup(mr.Close)
	rdb := redis.MustNewRedis(redis.RedisConf{Host: mr.Addr(), Type: redis.NodeType})
	return &Worker{svcCtx: &svc.ServiceContext{Redis: rdb, RelationRpc: &stubRelation{fans: fans}}}
}

// memberExists 判断 member 是否在 zset key 中（利用 Zscore 查成员）。
func memberExists(t *testing.T, rdb *redis.Redis, key, member string) bool {
	t.Helper()
	_, err := rdb.Zscore(key, member)
	return err == nil
}

func createMsg(t *testing.T, ev interface{}) *primitive.MessageExt {
	t.Helper()
	body, err := json.Marshal(ev)
	assert.NoError(t, err)
	return &primitive.MessageExt{Message: primitive.Message{Body: body}}
}

// 普通用户发帖：应写入粉丝 inbox、作者 outbox、推荐池、同城池。
func TestHandleFeedCreate_NormalUser_PushToFans(t *testing.T) {
	wk := newTestWorker(t, []int64{1001, 1002})
	ev := feedEvent.NewEventFeedCreated(9001, 555, false, "440300", 1752998400000)
	assert.NoError(t, wk.handleFeedCreate(context.Background(), createMsg(t, ev)))

	member := "9001"
	assert.True(t, memberExists(t, wk.svcCtx.Redis, keys.Inbox(1001), member))
	assert.True(t, memberExists(t, wk.svcCtx.Redis, keys.Inbox(1002), member))
	assert.True(t, memberExists(t, wk.svcCtx.Redis, keys.Outbox(555), member))
	assert.True(t, memberExists(t, wk.svcCtx.Redis, keys.Recommend(), member))
	assert.True(t, memberExists(t, wk.svcCtx.Redis, keys.City("440300"), member))
}

// 大V发帖：只写 outbox，不推粉丝 inbox（拉模式）。
func TestHandleFeedCreate_VipUser_NoPush(t *testing.T) {
	wk := newTestWorker(t, []int64{1001})
	ev := feedEvent.NewEventFeedCreated(9002, 556, true, "", 1752998400000)
	assert.NoError(t, wk.handleFeedCreate(context.Background(), createMsg(t, ev)))

	assert.False(t, memberExists(t, wk.svcCtx.Redis, keys.Inbox(1001), "9002"))
	assert.True(t, memberExists(t, wk.svcCtx.Redis, keys.Outbox(556), "9002"))
}

// 普通用户删帖：应从粉丝 inbox、推荐池、outbox 移除。
func TestHandleFeedDelete_NormalUser_RemoveFromFans(t *testing.T) {
	wk := newTestWorker(t, []int64{1001})
	ev := feedEvent.NewEventFeedCreated(9003, 557, false, "440300", 1752998400000)
	assert.NoError(t, wk.handleFeedCreate(context.Background(), createMsg(t, ev)))
	assert.True(t, memberExists(t, wk.svcCtx.Redis, keys.Inbox(1001), "9003"))

	del := feedEvent.NewEventFeedDeleted(9003, 557, false, "440300")
	assert.NoError(t, wk.handleFeedDelete(context.Background(), createMsg(t, del)))

	assert.False(t, memberExists(t, wk.svcCtx.Redis, keys.Inbox(1001), "9003"))
	assert.False(t, memberExists(t, wk.svcCtx.Redis, keys.Recommend(), "9003"))
	assert.False(t, memberExists(t, wk.svcCtx.Redis, keys.Outbox(557), "9003"))
}

// 大V删帖：仅清理 outbox，不触碰粉丝 inbox（大V本就不推 inbox）。
func TestHandleFeedDelete_VipUser_OnlyOutbox(t *testing.T) {
	wk := newTestWorker(t, []int64{1001})
	del := feedEvent.NewEventFeedDeleted(9004, 558, true, "")
	assert.NoError(t, wk.handleFeedDelete(context.Background(), createMsg(t, del)))
	assert.False(t, memberExists(t, wk.svcCtx.Redis, keys.Outbox(558), "9004"))
}

// inbox 超过容量时应被裁剪到 inboxCap 条（保留最新）。
func TestInboxCapacityTrim(t *testing.T) {
	wk := newTestWorker(t, []int64{1001})
	for i := int64(0); i < 1200; i++ {
		ev := feedEvent.NewEventFeedCreated(10000+i, 557, false, "", 1752998400000+i*1000)
		assert.NoError(t, wk.handleFeedCreate(context.Background(), createMsg(t, ev)))
	}
	card, err := wk.svcCtx.Redis.Zcard(keys.Inbox(1001))
	assert.NoError(t, err)
	assert.Equal(t, inboxCap, card)
	// 最早发布的 feed 应已被裁剪移除。
	assert.False(t, memberExists(t, wk.svcCtx.Redis, keys.Inbox(1001), strconv.FormatInt(10000, 10)))
	// 最新发布的 feed 应保留。
	assert.True(t, memberExists(t, wk.svcCtx.Redis, keys.Inbox(1001), strconv.FormatInt(10000+1199, 10)))
}
