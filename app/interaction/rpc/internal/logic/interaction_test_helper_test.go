// interaction_test_helper_test.go
//
// 职责：logic 层单元测试共享脚手架。
// miniredis 模拟 Redis，内存桩实现 LikesModel / CollectionsModel / Publisher，
// 不依赖真实 MySQL / RocketMQ，参照 app/feed/rpc/internal/logic/feed_logic_test.go 的写法。
package logic

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/sponge-dad/feed/app/interaction/model"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"
	event "github.com/sponge-dad/feed/common/event/interaction"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

// interRow 互动记录行（likes / collections 通用形态）。
type interRow struct {
	id        uint64
	userID    uint64
	feedID    uint64
	status    int64
	createdAt time.Time
	updatedAt time.Time
}

// stubStore likes / collections 桩的共享内存核心，带互斥锁支持并发用例。
type stubStore struct {
	mu   sync.Mutex
	rows map[string]*interRow // key: "user:feed"
	// countCalls / listCalls / feedUserCalls 用于断言"缓存命中时不回源"。
	countCalls    int
	listCalls     int
	feedUserCalls int
	// failFirstFind 为 true 时首次 FindOneByUserIdFeedId 强制返回 ErrNotFound，
	// 用于模拟并发插入撞唯一键的场景。
	failFirstFind bool
}

func newStubStore() *stubStore { return &stubStore{rows: make(map[string]*interRow)} }

func rowKey(userID, feedID uint64) string { return fmt.Sprintf("%d:%d", userID, feedID) }

// seed 预置一条记录（默认时间为当前时刻）。
func (s *stubStore) seed(userID, feedID uint64, status int64, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[rowKey(userID, feedID)] = &interRow{
		id: uint64(len(s.rows) + 1), userID: userID, feedID: feedID,
		status: status, createdAt: at, updatedAt: at,
	}
}

func (s *stubStore) insert(id, userID, feedID uint64, status int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := rowKey(userID, feedID)
	if _, ok := s.rows[key]; ok {
		return &mysql.MySQLError{Number: 1062, Message: "Duplicate entry"}
	}
	now := time.Now()
	s.rows[key] = &interRow{id: id, userID: userID, feedID: feedID, status: status, createdAt: now, updatedAt: now}
	return nil
}

func (s *stubStore) findOne(userID, feedID uint64) (*interRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failFirstFind {
		s.failFirstFind = false
		return nil, model.ErrNotFound
	}
	if r, ok := s.rows[rowKey(userID, feedID)]; ok {
		cp := *r
		return &cp, nil
	}
	return nil, model.ErrNotFound
}

func (s *stubStore) updateStatusIfNewer(id uint64, status int64, eventTime time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.rows {
		if r.id == id {
			if r.updatedAt.After(eventTime) {
				return false, nil
			}
			r.status = status
			r.updatedAt = eventTime
			return true, nil
		}
	}
	return false, nil
}

func (s *stubStore) countByFeed(feedID uint64) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.countCalls++
	var cnt int64
	for _, r := range s.rows {
		if r.feedID == feedID && r.status == 1 {
			cnt++
		}
	}
	return cnt
}

func (s *stubStore) countByFeeds(feedIDs []uint64) map[uint64]int64 {
	out := make(map[uint64]int64, len(feedIDs))
	for _, id := range feedIDs {
		out[id] = s.countByFeed(id)
	}
	return out
}

func (s *stubStore) userIDsByFeed(feedID uint64) []uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.feedUserCalls++
	var out []uint64
	for _, r := range s.rows {
		if r.feedID == feedID && r.status == 1 {
			out = append(out, r.userID)
		}
	}
	return out
}

func (s *stubStore) validByUser(userID uint64, limit int) []*interRow {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listCalls++
	var out []*interRow
	for _, r := range s.rows {
		if r.userID == userID && r.status == 1 {
			cp := *r
			out = append(out, &cp)
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *stubStore) byUserFeeds(userID uint64, feedIDs []uint64) []*interRow {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*interRow
	for _, id := range feedIDs {
		if r, ok := s.rows[rowKey(userID, id)]; ok {
			cp := *r
			out = append(out, &cp)
		}
	}
	return out
}

// ---------- LikesModel 桩 ----------

type stubLikesModel struct{ *stubStore }

func (s *stubLikesModel) Insert(_ context.Context, data *model.Likes) (sql.Result, error) {
	return nil, s.insert(data.Id, data.UserId, data.FeedId, data.Status)
}

func (s *stubLikesModel) FindOne(_ context.Context, _ uint64) (*model.Likes, error) {
	return nil, model.ErrNotFound
}

func (s *stubLikesModel) FindOneByUserIdFeedId(_ context.Context, userId, feedId uint64) (*model.Likes, error) {
	r, err := s.findOne(userId, feedId)
	if err != nil {
		return nil, err
	}
	return likeFromRow(r), nil
}

func (s *stubLikesModel) Update(_ context.Context, _ *model.Likes) error { return nil }
func (s *stubLikesModel) Delete(_ context.Context, _ uint64) error       { return nil }

func (s *stubLikesModel) CountByFeedId(_ context.Context, feedId uint64) (int64, error) {
	return s.countByFeed(feedId), nil
}

func (s *stubLikesModel) CountByFeedIds(_ context.Context, feedIds []uint64) (map[uint64]int64, error) {
	return s.countByFeeds(feedIds), nil
}

func (s *stubLikesModel) FindUserIdsByFeedId(_ context.Context, feedId uint64) ([]uint64, error) {
	return s.userIDsByFeed(feedId), nil
}

func (s *stubLikesModel) FindValidByUserId(_ context.Context, userId uint64, limit int) ([]*model.Likes, error) {
	rows := s.validByUser(userId, limit)
	out := make([]*model.Likes, 0, len(rows))
	for _, r := range rows {
		out = append(out, likeFromRow(r))
	}
	return out, nil
}

func (s *stubLikesModel) FindByUserIdFeedIds(_ context.Context, userId uint64, feedIds []uint64) ([]*model.Likes, error) {
	rows := s.byUserFeeds(userId, feedIds)
	out := make([]*model.Likes, 0, len(rows))
	for _, r := range rows {
		out = append(out, likeFromRow(r))
	}
	return out, nil
}

func (s *stubLikesModel) UpdateStatusIfNewer(_ context.Context, data *model.Likes, status int64, eventTime time.Time) (bool, error) {
	return s.updateStatusIfNewer(data.Id, status, eventTime)
}

func likeFromRow(r *interRow) *model.Likes {
	return &model.Likes{Id: r.id, UserId: r.userID, FeedId: r.feedID,
		Status: r.status, CreatedAt: r.createdAt, UpdatedAt: r.updatedAt}
}

// ---------- CollectionsModel 桩 ----------

type stubCollectionsModel struct{ *stubStore }

func (s *stubCollectionsModel) Insert(_ context.Context, data *model.Collections) (sql.Result, error) {
	return nil, s.insert(data.Id, data.UserId, data.FeedId, data.Status)
}

func (s *stubCollectionsModel) FindOne(_ context.Context, _ uint64) (*model.Collections, error) {
	return nil, model.ErrNotFound
}

func (s *stubCollectionsModel) FindOneByUserIdFeedId(_ context.Context, userId, feedId uint64) (*model.Collections, error) {
	r, err := s.findOne(userId, feedId)
	if err != nil {
		return nil, err
	}
	return collectFromRow(r), nil
}

func (s *stubCollectionsModel) Update(_ context.Context, _ *model.Collections) error { return nil }
func (s *stubCollectionsModel) Delete(_ context.Context, _ uint64) error             { return nil }

func (s *stubCollectionsModel) CountByFeedId(_ context.Context, feedId uint64) (int64, error) {
	return s.countByFeed(feedId), nil
}

func (s *stubCollectionsModel) CountByFeedIds(_ context.Context, feedIds []uint64) (map[uint64]int64, error) {
	return s.countByFeeds(feedIds), nil
}

func (s *stubCollectionsModel) FindUserIdsByFeedId(_ context.Context, feedId uint64) ([]uint64, error) {
	return s.userIDsByFeed(feedId), nil
}

func (s *stubCollectionsModel) FindValidByUserId(_ context.Context, userId uint64, limit int) ([]*model.Collections, error) {
	rows := s.validByUser(userId, limit)
	out := make([]*model.Collections, 0, len(rows))
	for _, r := range rows {
		out = append(out, collectFromRow(r))
	}
	return out, nil
}

func (s *stubCollectionsModel) FindByUserIdFeedIds(_ context.Context, userId uint64, feedIds []uint64) ([]*model.Collections, error) {
	rows := s.byUserFeeds(userId, feedIds)
	out := make([]*model.Collections, 0, len(rows))
	for _, r := range rows {
		out = append(out, collectFromRow(r))
	}
	return out, nil
}

func (s *stubCollectionsModel) UpdateStatusIfNewer(_ context.Context, data *model.Collections, status int64, eventTime time.Time) (bool, error) {
	return s.updateStatusIfNewer(data.Id, status, eventTime)
}

func collectFromRow(r *interRow) *model.Collections {
	return &model.Collections{Id: r.id, UserId: r.userID, FeedId: r.feedID,
		Status: r.status, CreatedAt: r.createdAt, UpdatedAt: r.updatedAt}
}

// ---------- Publisher 桩 ----------

// stubPublisher 捕获发送到 MQ 的互动事件。
type stubPublisher struct {
	mu     sync.Mutex
	events []event.Event
}

func (p *stubPublisher) SendSync(_ string, body []byte) error {
	var ev event.Event
	if err := json.Unmarshal(body, &ev); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, ev)
	return nil
}

// all 返回已捕获事件的副本。
func (p *stubPublisher) all() []event.Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]event.Event, len(p.events))
	copy(out, p.events)
	return out
}

// ---------- ServiceContext 组装 ----------

// testEnv 单元测试环境。
type testEnv struct {
	svcCtx   *svc.ServiceContext
	mr       *miniredis.Miniredis
	likes    *stubLikesModel
	collects *stubCollectionsModel
	pub      *stubPublisher
}

// testTime 返回一个固定基准时间，避免用例之间的时间耦合。
func testTime() time.Time {
	return time.Date(2026, 7, 1, 12, 0, 0, 0, time.Local)
}

// requireBizCode 断言返回了指定业务错误码。
func requireBizCode(t *testing.T, err error, code int) {
	t.Helper()
	require.Error(t, err)
	codeErr, ok := errorx.TryParse(err)
	require.True(t, ok, "应为业务错误, got %v", err)
	require.Equal(t, code, codeErr.Code)
}

// newTestEnv 构造 miniredis + 桩依赖的测试环境。
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	rdb := redis.MustNewRedis(redis.RedisConf{Type: "node", Host: mr.Addr()})

	likes := &stubLikesModel{stubStore: newStubStore()}
	collects := &stubCollectionsModel{stubStore: newStubStore()}
	pub := &stubPublisher{}
	var idSeq uint64 = 1000
	var idMu sync.Mutex
	return &testEnv{
		svcCtx: &svc.ServiceContext{
			Redis:            rdb,
			LikesModel:       likes,
			CollectionsModel: collects,
			Producer:         pub,
			IdGen: func() int64 {
				idMu.Lock()
				defer idMu.Unlock()
				idSeq++
				return int64(idSeq)
			},
		},
		mr:       mr,
		likes:    likes,
		collects: collects,
		pub:      pub,
	}
}
