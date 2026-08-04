// worker_test.go
//
// 职责：interaction.event 持久化消费者单元测试。
// 覆盖：插入、状态翻转、重复投递幂等、乱序旧事件不覆盖新状态、
// 「取消先到」墓碑行防复活、并发插入撞唯一键转条件更新。
// 桩模型通过内嵌接口只实现 worker 用到的方法，参照 feed 的 stubRelation 写法。
package worker

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/sponge-dad/feed/app/interaction/model"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"
	event "github.com/sponge-dad/feed/common/event/interaction"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// likeRowStore 内存 likes 行存储。
type likeRowStore struct {
	mu   sync.Mutex
	rows map[string]*model.Likes
	// failFirstFind 首次查询强制 NotFound，模拟并发插入撞唯一键
	failFirstFind bool
}

func key(userID, feedID uint64) string { return fmt.Sprintf("%d:%d", userID, feedID) }

// stubLikes 只实现 worker 依赖的方法，其余由内嵌接口兜底（调用即 panic，测试可及时暴露）。
type stubLikes struct {
	model.LikesModel
	s *likeRowStore
}

func (m *stubLikes) FindOneByUserIdFeedId(_ context.Context, userId, feedId uint64) (*model.Likes, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()
	if m.s.failFirstFind {
		m.s.failFirstFind = false
		return nil, model.ErrNotFound
	}
	if r, ok := m.s.rows[key(userId, feedId)]; ok {
		cp := *r
		return &cp, nil
	}
	return nil, model.ErrNotFound
}

func (m *stubLikes) Insert(_ context.Context, data *model.Likes) (sql.Result, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()
	k := key(data.UserId, data.FeedId)
	if _, ok := m.s.rows[k]; ok {
		return nil, &mysql.MySQLError{Number: 1062, Message: "Duplicate entry"}
	}
	cp := *data
	cp.CreatedAt = time.Now()
	cp.UpdatedAt = cp.CreatedAt
	m.s.rows[k] = &cp
	return nil, nil
}

func (m *stubLikes) UpdateStatusIfNewer(_ context.Context, data *model.Likes, status int64, eventTime time.Time) (bool, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()
	r, ok := m.s.rows[key(data.UserId, data.FeedId)]
	if !ok || r.UpdatedAt.After(eventTime) {
		return false, nil
	}
	r.Status = status
	r.UpdatedAt = eventTime
	return true, nil
}

// status 返回记录状态，-1 表示无记录。
func (s *likeRowStore) status(userID, feedID uint64) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.rows[key(userID, feedID)]; ok {
		return r.Status
	}
	return -1
}

func newTestWorker() (*Worker, *likeRowStore) {
	store := &likeRowStore{rows: make(map[string]*model.Likes)}
	var idSeq int64 = 1
	var mu sync.Mutex
	svcCtx := &svc.ServiceContext{
		LikesModel: &stubLikes{s: store},
		IdGen: func() int64 {
			mu.Lock()
			defer mu.Unlock()
			idSeq++
			return idSeq
		},
	}
	return NewWorker(svcCtx), store
}

// mkEvent 构造事件。
func mkEvent(userID, feedID int64, action int32, ts time.Time) *event.Event {
	return &event.Event{EventID: "test", UserID: userID, FeedID: feedID,
		ActionType: action, Timestamp: ts.UnixMilli()}
}

// TestWorker_LikeInsert 点赞事件：无记录时插入 status=1。
func TestWorker_LikeInsert(t *testing.T) {
	w, store := newTestWorker()
	err := w.HandleEvent(context.Background(), mkEvent(1, 100, event.ActionLike, time.Now()))
	require.NoError(t, err)
	assert.Equal(t, int64(1), store.status(1, 100))
}

// TestWorker_UnlikeFlip 取消事件：已有 status=1 记录且事件更新时间更晚，翻转为 2。
func TestWorker_UnlikeFlip(t *testing.T) {
	w, store := newTestWorker()
	ctx := context.Background()
	now := time.Now()
	require.NoError(t, w.HandleEvent(ctx, mkEvent(1, 100, event.ActionLike, now)))
	require.NoError(t, w.HandleEvent(ctx, mkEvent(1, 100, event.ActionUnlike, now.Add(time.Second))))
	assert.Equal(t, int64(2), store.status(1, 100))
}

// TestWorker_DuplicateDelivery 重复投递同一点赞事件：幂等，不报错，状态不变。
func TestWorker_DuplicateDelivery(t *testing.T) {
	w, store := newTestWorker()
	ctx := context.Background()
	ev := mkEvent(1, 100, event.ActionLike, time.Now())
	require.NoError(t, w.HandleEvent(ctx, ev))
	require.NoError(t, w.HandleEvent(ctx, ev))
	assert.Equal(t, int64(1), store.status(1, 100))
}

// TestWorker_StaleEventSkipped 乱序：晚到的旧「取消」事件不得覆盖更新的「点赞」状态。
func TestWorker_StaleEventSkipped(t *testing.T) {
	w, store := newTestWorker()
	ctx := context.Background()
	now := time.Now()
	require.NoError(t, w.HandleEvent(ctx, mkEvent(1, 100, event.ActionLike, now)))
	// 旧事件：时间戳早于记录 updated_at
	require.NoError(t, w.HandleEvent(ctx, mkEvent(1, 100, event.ActionUnlike, now.Add(-time.Hour))))
	assert.Equal(t, int64(1), store.status(1, 100), "旧取消事件不应覆盖新点赞状态")
}

// TestWorker_TombstoneOnEarlyUnlike 「取消」先到且无记录：插入 status=2 墓碑，
// 防止乱序晚到的旧「点赞」事件复活状态。
func TestWorker_TombstoneOnEarlyUnlike(t *testing.T) {
	w, store := newTestWorker()
	ctx := context.Background()
	now := time.Now()

	require.NoError(t, w.HandleEvent(ctx, mkEvent(1, 100, event.ActionUnlike, now)))
	assert.Equal(t, int64(2), store.status(1, 100), "取消先到应落墓碑行")

	// 乱序晚到的旧点赞（时间戳更早）不得复活
	require.NoError(t, w.HandleEvent(ctx, mkEvent(1, 100, event.ActionLike, now.Add(-time.Minute))))
	assert.Equal(t, int64(2), store.status(1, 100), "旧点赞事件不应复活已取消状态")

	// 用户之后真正重新点赞（时间戳更新）可以正常生效
	require.NoError(t, w.HandleEvent(ctx, mkEvent(1, 100, event.ActionLike, now.Add(time.Minute))))
	assert.Equal(t, int64(1), store.status(1, 100))
}

// TestWorker_DupInsertRace 并发插入撞唯一键：识别 1062 后转条件更新，不报错。
func TestWorker_DupInsertRace(t *testing.T) {
	w, store := newTestWorker()
	ctx := context.Background()
	now := time.Now()
	// 预置已取消记录，并让首次查询强制 NotFound → Insert 撞 1062 → 重查 → 条件更新
	require.NoError(t, w.HandleEvent(ctx, mkEvent(1, 100, event.ActionUnlike, now)))
	store.failFirstFind = true

	require.NoError(t, w.HandleEvent(ctx, mkEvent(1, 100, event.ActionLike, now.Add(time.Second))))
	assert.Equal(t, int64(1), store.status(1, 100))
}

// TestWorker_UnknownAction 未知动作直接确认，不报错。
func TestWorker_UnknownAction(t *testing.T) {
	w, _ := newTestWorker()
	require.NoError(t, w.HandleEvent(context.Background(), mkEvent(1, 100, 99, time.Now())))
}
