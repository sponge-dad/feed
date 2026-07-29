package worker

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/alicebob/miniredis/v2"
	red "github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/sponge-dad/feed/app/feed/model"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/svc"
	commentEvent "github.com/sponge-dad/feed/common/event/comment"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

// stubFeedsModel 是 model.FeedsModel 的部分桩，仅实现 IncrCommentCount，
// 记录最近一次调用参数，供断言使用。
type stubFeedsModel struct {
	model.FeedsModel
	lastFeedID uint64
	lastDelta  int64
	updCnt     int
}

func (s *stubFeedsModel) IncrCommentCount(_ context.Context, feedID uint64, delta int64) error {
	s.lastFeedID = feedID
	s.lastDelta = delta
	s.updCnt++
	return nil
}

// newTestWorker 构造带 miniredis 与桩依赖的 Worker（无 Comment RPC 依赖）。
func newTestWorker(t *testing.T, sm *stubFeedsModel) *Worker {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	rdb := redis.MustNewRedis(redis.RedisConf{Type: "node", Host: mr.Addr()})
	ctx := &svc.ServiceContext{
		Redis:     rdb,
		FeedModel: sm,
	}
	return NewWorker(ctx)
}

func newCommentMsg(t *testing.T, ev commentEvent.Event) *red.MessageExt {
	t.Helper()
	body, err := json.Marshal(ev)
	require.NoError(t, err)
	return &red.MessageExt{Message: red.Message{Body: body}}
}

// TestHandleCommentEvent_CreateIncr 验证：收到 CREATE 事件后 comment_count +1。
func TestHandleCommentEvent_CreateIncr(t *testing.T) {
	sm := &stubFeedsModel{}
	wk := newTestWorker(t, sm)

	ev := commentEvent.Event{EventID: "evt-create-1", FeedID: 2082031676489207808, ActionType: commentEvent.ActionCreate}
	require.NoError(t, wk.handleCommentEvent(context.Background(), newCommentMsg(t, ev)))

	require.Equal(t, 1, sm.updCnt, "应增量更新一次")
	require.Equal(t, uint64(2082031676489207808), sm.lastFeedID)
	require.Equal(t, int64(1), sm.lastDelta, "CREATE 应 +1")
}

// TestHandleCommentEvent_DeleteDecr 验证：收到 DELETE 事件后 comment_count -1。
func TestHandleCommentEvent_DeleteDecr(t *testing.T) {
	sm := &stubFeedsModel{}
	wk := newTestWorker(t, sm)

	ev := commentEvent.Event{EventID: "evt-delete-1", FeedID: 42, ActionType: commentEvent.ActionDelete}
	require.NoError(t, wk.handleCommentEvent(context.Background(), newCommentMsg(t, ev)))

	require.Equal(t, 1, sm.updCnt)
	require.Equal(t, int64(-1), sm.lastDelta, "DELETE 应 -1")
}

// TestHandleCommentEvent_Idempotent 验证：同 event_id 重放只处理一次（Redis SETNX 去重）。
func TestHandleCommentEvent_Idempotent(t *testing.T) {
	sm := &stubFeedsModel{}
	wk := newTestWorker(t, sm)

	ev := commentEvent.Event{EventID: "evt-idem-1", FeedID: 42, ActionType: commentEvent.ActionCreate}
	require.NoError(t, wk.handleCommentEvent(context.Background(), newCommentMsg(t, ev)))
	require.NoError(t, wk.handleCommentEvent(context.Background(), newCommentMsg(t, ev))) // 重放

	require.Equal(t, 1, sm.updCnt, "去重后只增量一次")
	require.Equal(t, int64(1), sm.lastDelta)
}

// TestHandleCommentEvent_BadBody 验证：消息体损坏时返回 nil（避免死信堆积），不触发更新。
func TestHandleCommentEvent_BadBody(t *testing.T) {
	sm := &stubFeedsModel{}
	wk := newTestWorker(t, sm)

	require.NoError(t, wk.handleCommentEvent(context.Background(), &red.MessageExt{Message: red.Message{Body: []byte("not-json")}}))
	require.Equal(t, 0, sm.updCnt)
}

// TestHandleCommentEvent_UnknownAction 验证：未知 action_type 不更新计数、不报错。
func TestHandleCommentEvent_UnknownAction(t *testing.T) {
	sm := &stubFeedsModel{}
	wk := newTestWorker(t, sm)

	ev := commentEvent.Event{EventID: "evt-unknown-1", FeedID: 99, ActionType: "UNKNOWN"}
	require.NoError(t, wk.handleCommentEvent(context.Background(), newCommentMsg(t, ev)))
	require.Equal(t, 0, sm.updCnt, "未知动作不应更新计数")
}
