// worker_dedup_test.go
//
// 职责：comment-event 消费去重（E-CE-03 / R-P1-8）回归测试。
//
// E-CE-03 缺陷现场：若先 SETNX 去重键、随后 DB 增量更新失败，且去重键未被清除，
// 则 MQ 重投放会被去重逻辑跳过，导致 feeds.comment_count 镜像计数永久丢失。
// 当前实现在 DB 失败时主动 Del 去重键，使重试可重放成功。本文件固化该正确行为，
// 防止去重逻辑回归为「失败即永久跳过」。
package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/sponge-dad/feed/app/feed/model"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/keys"
	commentEvent "github.com/sponge-dad/feed/common/event/comment"
	"github.com/stretchr/testify/require"
)

// flakyFeedsModel 第一次 IncrCommentCount 失败，后续成功 —— 模拟 E-CE-03 故障注入
// （SETNX 成功后 DB 更新失败）。验证实现清除去重键允许重试重放（计数不丢）。
type flakyFeedsModel struct {
	model.FeedsModel
	calls     int
	lastFeed  uint64
	lastDelta int64
	okCnt     int
}

func (s *flakyFeedsModel) IncrCommentCount(_ context.Context, feedID uint64, delta int64) error {
	s.calls++
	s.lastFeed = feedID
	s.lastDelta = delta
	if s.calls == 1 {
		return errors.New("db down")
	}
	s.okCnt++
	return nil
}

// TestHandleCommentEvent_DBFailThenRetryRecovers 复现并验证 E-CE-03 修复：
// 首次 DB 失败不应永久去重，重试可重放成功，最终计数 +1（不丢、不重复）。
func TestHandleCommentEvent_DBFailThenRetryRecovers(t *testing.T) {
	sm := &flakyFeedsModel{}
	wk := newTestWorker(t, sm)

	ev := commentEvent.Event{EventID: "evt-flaky-1", FeedID: 777, ActionType: commentEvent.ActionCreate}
	err1 := wk.handleCommentEvent(context.Background(), newCommentMsg(t, ev))
	require.Error(t, err1, "首次 DB 失败应返回 error 触发 MQ 重试")
	require.Equal(t, 0, sm.okCnt, "首次失败不应计为成功更新")

	// 重试（同一 event_id）
	err2 := wk.handleCommentEvent(context.Background(), newCommentMsg(t, ev))
	require.NoError(t, err2, "重试应成功（去重键已被清除，未被跳过）")
	require.Equal(t, 1, sm.okCnt, "重试后计数应成功更新一次")
	require.Equal(t, uint64(777), sm.lastFeed)
	require.Equal(t, int64(1), sm.lastDelta)
}

// TestHandleCommentEvent_DedupKeyTTL 验证去重键写入后具备 24h 过期（避免永久占用）。
func TestHandleCommentEvent_DedupKeyTTL(t *testing.T) {
	sm := &stubFeedsModel{}
	wk := newTestWorker(t, sm)

	ev := commentEvent.Event{EventID: "evt-ttl-1", FeedID: 888, ActionType: commentEvent.ActionCreate}
	require.NoError(t, wk.handleCommentEvent(context.Background(), newCommentMsg(t, ev)))

	key := keys.CommentEventDedup(ev.EventID)
	ttl, ttlErr := wk.svcCtx.Redis.Ttl(key)
	require.NoError(t, ttlErr)
	require.Greater(t, ttl, 23*3600)
}
