// behaviorWorkerIntegration_test.go
//
// 职责：用 miniredis + 内存 Model 桩覆盖行为事件消费链路的集成行为。
//
// 重点验证三条正确性保证（见 docs/design/agent/03-behavior-event.md §4/§6.2）：
//
//  1. event_id 幂等——同一事件重复投递只累加一次；
//  2. EXPOSE 去重——同一 (request_id, feed_id) 只计一次曝光；
//  3. flush 写绝对值——重复 flush / 中途失败重试都不会让指标翻倍。
//
// 另外覆盖失败路径：落库失败必须删除幂等 key，否则 MQ 重试会被误判为已处理导致永久丢数。
package worker

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/sponge-dad/feed/app/interaction/model"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/config"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/keys"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"
	bhv "github.com/sponge-dad/feed/common/event/behavior"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

// ---------- 内存 Model 桩 ----------

// stubBehaviorEventsModel 记录明细写入，可注入错误模拟落库失败。
type stubBehaviorEventsModel struct {
	mu       sync.Mutex
	rows     []*model.FeedBehaviorEvents
	insertFn func(*model.FeedBehaviorEvents) error
}

func (s *stubBehaviorEventsModel) Insert(_ context.Context, data *model.FeedBehaviorEvents) (sql.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.insertFn != nil {
		if err := s.insertFn(data); err != nil {
			return nil, err
		}
	}
	s.rows = append(s.rows, data)
	return nil, nil
}

func (s *stubBehaviorEventsModel) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.rows)
}

func (s *stubBehaviorEventsModel) FindOne(context.Context, uint64) (*model.FeedBehaviorEvents, error) {
	return nil, model.ErrNotFound
}

func (s *stubBehaviorEventsModel) FindOneByEventId(context.Context, string) (*model.FeedBehaviorEvents, error) {
	return nil, model.ErrNotFound
}

func (s *stubBehaviorEventsModel) DeleteBefore(context.Context, time.Time, int64) (int64, error) {
	return 0, nil
}

// stubMetricsHourlyModel 以 (feed_id, stat_hour) 为键保存最后一次写入的绝对值，
// 并统计 Upsert 次数，用于验证「重复 flush 不翻倍」。
type stubMetricsHourlyModel struct {
	mu        sync.Mutex
	rows      map[string]*model.FeedMetricsHourly
	upsertCnt int
	upsertErr error
}

func newStubMetricsHourlyModel() *stubMetricsHourlyModel {
	return &stubMetricsHourlyModel{rows: make(map[string]*model.FeedMetricsHourly)}
}

func metricsStubKey(feedID uint64, statHour time.Time) string {
	return strconv.FormatUint(feedID, 10) + ":" + statHour.Format(keys.StatHourLayout)
}

func (s *stubMetricsHourlyModel) Upsert(_ context.Context, data *model.FeedMetricsHourly) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upsertCnt++
	if s.upsertErr != nil {
		return s.upsertErr
	}
	cp := *data
	s.rows[metricsStubKey(data.FeedId, data.StatHour)] = &cp
	return nil
}

func (s *stubMetricsHourlyModel) get(feedID uint64, statHour time.Time) *model.FeedMetricsHourly {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rows[metricsStubKey(feedID, statHour)]
}

func (s *stubMetricsHourlyModel) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.upsertCnt
}

func (s *stubMetricsHourlyModel) Insert(context.Context, *model.FeedMetricsHourly) (sql.Result, error) {
	return nil, nil
}

func (s *stubMetricsHourlyModel) FindOne(context.Context, uint64) (*model.FeedMetricsHourly, error) {
	return nil, model.ErrNotFound
}

func (s *stubMetricsHourlyModel) SumByFeedAndWindow(ctx context.Context, feedID int64, since time.Time) (*model.FeedMetricsHourly, error) {
	return &model.FeedMetricsHourly{}, nil
}

func (s *stubMetricsHourlyModel) SumByAuthorAndWindow(ctx context.Context, authorID int64, since time.Time) (*model.FeedMetricsHourly, error) {
	return &model.FeedMetricsHourly{}, nil
}

func (s *stubMetricsHourlyModel) SumByFeedIDs(ctx context.Context, feedIDs []int64, since time.Time) (map[int64]*model.FeedMetricsHourly, error) {
	return map[int64]*model.FeedMetricsHourly{}, nil
}

func (s *stubMetricsHourlyModel) AvgByFeedIDs(ctx context.Context, feedIDs []int64, since time.Time) (*model.FeedMetricsHourly, error) {
	return &model.FeedMetricsHourly{}, nil
}

// ---------- 测试环境 ----------

type behaviorEnv struct {
	bw      *BehaviorWorker
	mr      *miniredis.Miniredis
	details *stubBehaviorEventsModel
	metrics *stubMetricsHourlyModel
}

// newBehaviorEnv 构造 miniredis + 内存 Model 的消费环境。
// ExposeSampleRate 置 1，让 EXPOSE 也全量落库，便于断言明细条数。
func newBehaviorEnv(t *testing.T) *behaviorEnv {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	rdb := redis.MustNewRedis(redis.RedisConf{Type: "node", Host: mr.Addr()})
	details := &stubBehaviorEventsModel{}
	metrics := newStubMetricsHourlyModel()

	var idSeq int64 = 1000
	var idMu sync.Mutex

	rule := config.BehaviorRule{}
	rule.Fill()

	cfg := config.Config{}
	cfg.Behavior.ExposeSampleRate = 1
	cfg.Behavior.MetricsFlushBatch = 500

	bw := &BehaviorWorker{
		svcCtx: &svc.ServiceContext{
			Config:                  cfg,
			Redis:                   rdb,
			FeedBehaviorEventsModel: details,
			FeedMetricsHourlyModel:  metrics,
			IdGen: func() int64 {
				idMu.Lock()
				defer idMu.Unlock()
				idSeq++
				return idSeq
			},
		},
		rule: rule,
		skew: newClockSkewTracker(),
		stop: make(chan struct{}),
	}
	return &behaviorEnv{bw: bw, mr: mr, details: details, metrics: metrics}
}

// hourBucket 事件对应的小时桶（与 worker 的分桶口径一致：优先 server_time）。
func hourBucket(ev *bhv.FeedBehaviorEvent) time.Time {
	return eventTime(ev).Truncate(time.Hour)
}

func (e *behaviorEnv) hashField(t *testing.T, ev *bhv.FeedBehaviorEvent, field string) string {
	t.Helper()
	key := keys.MetricsHour(ev.FeedID, hourBucket(ev).Format(keys.StatHourLayout))
	return e.mr.HGet(key, field)
}

// newTestEvent 构造一条合法事件；同一 requestID 下可产生多条不同 event_id 的事件。
func newTestEvent(requestID, action string, watchMs, mediaMs int64) *bhv.FeedBehaviorEvent {
	now := time.Now().UnixMilli()
	return bhv.NewEvent(requestID, 2001, 3001, 4001, action, 3, watchMs, mediaMs, now, now, "")
}

// ---------- 用例 ----------

// TestHandleBehaviorEventIdempotent 同一 event_id 重复投递只应生效一次。
func TestHandleBehaviorEventIdempotent(t *testing.T) {
	env := newBehaviorEnv(t)
	ev := newTestEvent("req-1", bhv.ActionPlay, 0, 0)

	for i := 0; i < 3; i++ {
		require.NoError(t, env.bw.HandleBehaviorEvent(context.Background(), ev))
	}

	assert.Equal(t, 1, env.details.count(), "明细只应落库一次")
	assert.Equal(t, "1", env.hashField(t, ev, keys.FieldPlay), "指标只应累加一次")
}

// TestHandleBehaviorEventExposeDedup 同一 (request_id, feed_id) 的重复曝光只计一次。
func TestHandleBehaviorEventExposeDedup(t *testing.T) {
	env := newBehaviorEnv(t)

	// 两条 event_id 不同、request_id 相同的曝光：模拟客户端重复上报
	first := newTestEvent("req-dup", bhv.ActionExpose, 0, 0)
	second := newTestEvent("req-dup", bhv.ActionExpose, 0, 0)
	require.NotEqual(t, first.EventID, second.EventID)

	require.NoError(t, env.bw.HandleBehaviorEvent(context.Background(), first))
	require.NoError(t, env.bw.HandleBehaviorEvent(context.Background(), second))

	assert.Equal(t, "1", env.hashField(t, first, keys.FieldExpose), "重复曝光只计一次")
	assert.Equal(t, 1, env.details.count(), "重复曝光不应重复落库")

	// 不同 request_id 属于不同的一次浏览，应正常计数
	other := newTestEvent("req-other", bhv.ActionExpose, 0, 0)
	require.NoError(t, env.bw.HandleBehaviorEvent(context.Background(), other))
	assert.Equal(t, "2", env.hashField(t, other, keys.FieldExpose))
}

// TestHandleBehaviorEventRejudgeAndAccumulate 服务端重判后应按校正结果累加，
// 并同时累加观看时长。
func TestHandleBehaviorEventRejudgeAndAccumulate(t *testing.T) {
	env := newBehaviorEnv(t)

	// 客户端谎报 SKIP，实际观看 9.8s/10s → 服务端应校正为 FINISH
	ev := newTestEvent("req-2", bhv.ActionSkip, 9800, 10000)
	require.NoError(t, env.bw.HandleBehaviorEvent(context.Background(), ev))

	assert.Equal(t, "1", env.hashField(t, ev, keys.FieldFinish), "应按重判结果计入 finish")
	assert.Equal(t, "", env.hashField(t, ev, keys.FieldSkip), "不应保留客户端谎报的 skip")
	assert.Equal(t, "9800", env.hashField(t, ev, keys.FieldWatchMs))
	assert.Equal(t, "4001", env.hashField(t, ev, keys.FieldAuthorID))
}

// TestHandleBehaviorEventDropsIdemOnPersistFailure 落库失败必须删除幂等 key，
// 否则 MQ 重试会被误判为「已处理」而永久丢数。
func TestHandleBehaviorEventDropsIdemOnPersistFailure(t *testing.T) {
	env := newBehaviorEnv(t)

	failing := errors.New("db down")
	env.details.insertFn = func(*model.FeedBehaviorEvents) error { return failing }

	ev := newTestEvent("req-3", bhv.ActionPlay, 0, 0)
	require.Error(t, env.bw.HandleBehaviorEvent(context.Background(), ev), "落库失败应返回 error 触发重试")
	assert.False(t, env.mr.Exists(ev.IdemKey()), "失败后幂等 key 必须被删除")

	// 模拟 MQ 重试：数据库恢复后重投同一条，应成功落库
	env.details.insertFn = nil
	require.NoError(t, env.bw.HandleBehaviorEvent(context.Background(), ev))
	assert.Equal(t, 1, env.details.count())
	assert.Equal(t, "1", env.hashField(t, ev, keys.FieldPlay))
}

// TestFlushOnceWritesAbsoluteValue 重复 flush 不应让指标翻倍（Upsert 写绝对值）。
func TestFlushOnceWritesAbsoluteValue(t *testing.T) {
	env := newBehaviorEnv(t)
	ctx := context.Background()

	ev := newTestEvent("req-4", bhv.ActionPlay, 1500, 10000)
	require.NoError(t, env.bw.HandleBehaviorEvent(ctx, ev))

	bucket := hourBucket(ev)
	env.bw.flushOnce(ctx)

	row := env.metrics.get(uint64(ev.FeedID), bucket)
	require.NotNil(t, row)
	assert.EqualValues(t, 1, row.PlayCount)
	assert.EqualValues(t, 1500, row.WatchDurationMs)
	assert.EqualValues(t, 4001, row.AuthorId)

	// 脏集合已被 SPOP 清空，再次 flush 不应产生任何写入
	before := env.metrics.calls()
	env.bw.flushOnce(ctx)
	assert.Equal(t, before, env.metrics.calls(), "无脏桶时不应重复落库")

	// 同小时再来一条播放：Redis 累加为 2，落库写的是绝对值 2 而非 1+1 增量
	ev2 := newTestEvent("req-5", bhv.ActionPlay, 500, 10000)
	ev2.ServerTime = ev.ServerTime // 固定到同一小时桶
	require.NoError(t, env.bw.HandleBehaviorEvent(ctx, ev2))
	env.bw.flushOnce(ctx)

	row = env.metrics.get(uint64(ev.FeedID), bucket)
	require.NotNil(t, row)
	assert.EqualValues(t, 2, row.PlayCount)
	assert.EqualValues(t, 2000, row.WatchDurationMs)
}

// TestFlushOnceRequeuesOnUpsertFailure 落库失败的桶必须放回脏集合，
// 否则该小时的增量会永久滞留在 Redis 里不再落库。
func TestFlushOnceRequeuesOnUpsertFailure(t *testing.T) {
	env := newBehaviorEnv(t)
	ctx := context.Background()

	ev := newTestEvent("req-6", bhv.ActionPlay, 0, 0)
	require.NoError(t, env.bw.HandleBehaviorEvent(ctx, ev))

	env.metrics.upsertErr = errors.New("db down")
	env.bw.flushOnce(ctx)

	member := keys.MetricsDirtyMember(ev.FeedID, hourBucket(ev).Format(keys.StatHourLayout))
	members, err := env.mr.Members(keys.MetricsDirtySet)
	require.NoError(t, err)
	assert.Contains(t, members, member, "失败的桶应放回脏集合等待下轮重试")

	// 恢复后重试应成功落库
	env.metrics.upsertErr = nil
	env.bw.flushOnce(ctx)
	row := env.metrics.get(uint64(ev.FeedID), hourBucket(ev))
	require.NotNil(t, row)
	assert.EqualValues(t, 1, row.PlayCount)
}

// TestFlushOnceSkipsExpiredHash Hash 已过期（无字段）时不应写入空行。
func TestFlushOnceSkipsExpiredHash(t *testing.T) {
	env := newBehaviorEnv(t)
	ctx := context.Background()

	ev := newTestEvent("req-7", bhv.ActionPlay, 0, 0)
	require.NoError(t, env.bw.HandleBehaviorEvent(ctx, ev))

	// 模拟小时桶 Hash 已过期，仅脏集合成员残留
	env.mr.Del(keys.MetricsHour(ev.FeedID, hourBucket(ev).Format(keys.StatHourLayout)))
	env.bw.flushOnce(ctx)

	assert.Equal(t, 0, env.metrics.calls(), "Hash 不存在时不应落库")
}

// TestHandleBehaviorEventExposeWithoutRequestID request_id 为空时不做曝光去重。
//
// 否则去重键退化为 {feed_id} 维度，不同用户/不同请求的曝光会互相去重，
// 导致曝光指标被系统性低估。
func TestHandleBehaviorEventExposeWithoutRequestID(t *testing.T) {
	env := newBehaviorEnv(t)

	first := newTestEvent("", bhv.ActionExpose, 0, 0)
	second := newTestEvent("", bhv.ActionExpose, 0, 0)
	require.NotEqual(t, first.EventID, second.EventID)

	require.NoError(t, env.bw.HandleBehaviorEvent(context.Background(), first))
	require.NoError(t, env.bw.HandleBehaviorEvent(context.Background(), second))

	assert.Equal(t, "2", env.hashField(t, first, keys.FieldExpose), "空 request_id 的曝光不应互相去重")
	assert.Equal(t, 2, env.details.count(), "空 request_id 的曝光都应落库")
}

// TestHandleBehaviorEventExposeDedupReleasedOnFailure EXPOSE 明细落库失败后重试，
// 不应被残留的曝光去重键误判为重复曝光而永久丢弃。
func TestHandleBehaviorEventExposeDedupReleasedOnFailure(t *testing.T) {
	env := newBehaviorEnv(t)

	failing := errors.New("db down")
	env.details.insertFn = func(*model.FeedBehaviorEvents) error { return failing }

	ev := newTestEvent("req-expose-fail", bhv.ActionExpose, 0, 0)
	require.Error(t, env.bw.HandleBehaviorEvent(context.Background(), ev), "落库失败应返回 error")
	assert.False(t, env.mr.Exists(ev.ExposeDedupKey()), "失败后曝光去重键必须被删除")

	// 模拟 MQ 重试：数据库恢复后重投同一条，应成功落库并计数
	env.details.insertFn = nil
	require.NoError(t, env.bw.HandleBehaviorEvent(context.Background(), ev))
	assert.Equal(t, 1, env.details.count())
	assert.Equal(t, "1", env.hashField(t, ev, keys.FieldExpose))
}

// TestFlushOnceReleasesClaim 落库成功后处理中集合应被清空（不残留认领）。
func TestFlushOnceReleasesClaim(t *testing.T) {
	env := newBehaviorEnv(t)
	ctx := context.Background()

	ev := newTestEvent("req-flush-claim", bhv.ActionPlay, 0, 0)
	require.NoError(t, env.bw.HandleBehaviorEvent(ctx, ev))

	env.bw.flushOnce(ctx)

	// 成功落库后处理中集合应无成员（空集合的 key 在 Redis 中自动消失）
	assert.False(t, env.mr.Exists(keys.MetricsProcessingSet), "成功落库后处理中集合应为空")
}

// TestRecoverProcessingMembers 崩溃遗留的处理中成员应被合并回脏集合，
// 否则该小时指标永久滞留 Redis 不再落库。
func TestRecoverProcessingMembers(t *testing.T) {
	env := newBehaviorEnv(t)
	ctx := context.Background()

	ev := newTestEvent("req-recover", bhv.ActionPlay, 0, 0)
	require.NoError(t, env.bw.HandleBehaviorEvent(ctx, ev))
	member := keys.MetricsDirtyMember(ev.FeedID, hourBucket(ev).Format(keys.StatHourLayout))

	// 模拟：成员被认领后进程崩溃，残留在处理中集合
	_, err := env.mr.SRem(keys.MetricsDirtySet, member)
	require.NoError(t, err)
	_, err = env.mr.SAdd(keys.MetricsProcessingSet, member)
	require.NoError(t, err)

	env.bw.recoverProcessingMembers()

	dirty, err := env.mr.Members(keys.MetricsDirtySet)
	require.NoError(t, err)
	assert.Contains(t, dirty, member, "遗留成员应被合并回脏集合")

	assert.False(t, env.mr.Exists(keys.MetricsProcessingSet), "处理中集合应被清空")
}
