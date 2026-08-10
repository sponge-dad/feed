// behaviorWorker.go
//
// 职责：Feed 行为事件消费者（behavior-persistence-consumer），跑在 Interaction RPC
// 进程内，与现有落库 consumer 并列。订阅三个 Topic：
//
//	feed-behavior-event  明细 + 指标（+ 兴趣，待 Content 服务落地）
//	interaction-event    仅把 like/unlike/collect/uncollect 计入指标（不落库）
//	comment-event        仅把 comment 计入指标（不落库）
//
// 处理流程（见 docs/design/agent/03-behavior-event.md §4）：
//
//  1. json.Unmarshal 失败 → 记日志 + 返回 nil（消息体损坏不可恢复，避免死信堆积）
//  2. 幂等：SETNX behavior_event:{event_id}（TTL 24h）；已存在 → 直接返回 nil
//  3. 规则重判：EFFECTIVE_PLAY / FINISH / SKIP 按服务端阈值校正 action_type
//  4. EXPOSE 去重：SETNX behavior:expose:{request_id}:{feed_id}
//     （仅当 request_id 非空；空值时跳过，避免去重键退化为 {feed_id} 维度
//     导致不同用户/不同请求的曝光互相去重、曝光指标被系统性低估）
//  5. 明细落库（按采样策略）feed_behavior_events（唯一键 uk_event_id 兜底幂等）
//  6. 指标累加：单条 Lua 脚本原子累加 HINCRBY feed:metrics:h:{feed_id}:{stat_hour}
//     与观看时长并打脏标记——脚本要么全部生效要么全部不生效，
//     保证「HINCRBY 已执行但后续步骤失败」这类半成功状态不会发生
//  7. 兴趣更新：见 06-user-interest.md（需先取内容画像标签）
//  8. 任一持久化步骤失败 → DEL 幂等 key（及已注册的 EXPOSE 去重 key）后返回 error
//     （RocketMQ 重试；不残留去重键，避免重试被误判为重复曝光）
//
// 步骤 5 在 6 之前是刻意的：明细落库靠唯一键天然幂等，而指标累加不幂等，
// 把不幂等的操作放最后，可保证「前置步骤失败后重试」不会造成指标重复累加；
// 指标累加自身再叠加 Lua 原子性，则「HINCRBY 部分失败后的重试」也不会重复累加。
package worker

import (
	"context"
	"encoding/json"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sponge-dad/feed/app/interaction/model"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/config"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/keys"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"
	bhv "github.com/sponge-dad/feed/common/event/behavior"
	cmtev "github.com/sponge-dad/feed/common/event/comment"
	intev "github.com/sponge-dad/feed/common/event/interaction"

	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/zeromicro/go-zero/core/logx"
)

// behaviorIncrScript 原子累加小时桶指标并打脏标记。
//
// 用 Lua 将「多字段 HINCRBY + HSETNX author_id + SADD 脏集合 + EXPIRE」合并为一次
// 原子操作：脚本要么全部生效、要么全部不生效（连接中断时脚本不会留下半执行状态）。
// 若仍用多条独立命令，HINCRBY 成功后 SADD 失败会返回 error，上层删除幂等键后
// MQ 重试会让 HINCRBY 再次执行，造成计数重复累加。
//
// 参数：
//
//	KEYS[1] = feed:metrics:h:{feed}:{hour}
//	KEYS[2] = feed:metrics:dirty
//	ARGV[1] = field1（行为计数，如 play）
//	ARGV[2] = delta1
//	ARGV[3] = field2（可选，如 watch_ms；空串表示不累加）
//	ARGV[4] = delta2
//	ARGV[5] = author_id 字段名
//	ARGV[6] = author_id（"0" 表示未知，不写入）
//	ARGV[7] = dirty 成员 "{feed}:{hour}"
//	ARGV[8] = hash TTL（秒）
const behaviorIncrScript = `
redis.call('HINCRBY', KEYS[1], ARGV[1], ARGV[2])
if ARGV[3] ~= '' then
  redis.call('HINCRBY', KEYS[1], ARGV[3], ARGV[4])
end
if tonumber(ARGV[6]) > 0 then
  redis.call('HSETNX', KEYS[1], ARGV[5], ARGV[6])
end
local added = redis.call('SADD', KEYS[2], ARGV[7])
if added == 1 then
  redis.call('EXPIRE', KEYS[1], ARGV[8])
end
return added
`

// claimDirtyScript 原子地从脏集合认领一个成员到处理中集合。
//
// SRANDMEMBER + SMOVE 两步合并进一个脚本：多实例并发 flush 时，同一成员只会被
// 一个实例认领（SMOVE 只在成员存在时移动）。成员在落库完成前保存在
// MetricsProcessingSet，进程崩溃后由启动自愈合并回脏集合，避免桶标记丢失。
//
//	KEYS[1] = feed:metrics:dirty
//	KEYS[2] = feed:metrics:processing
//	返回认领到的成员；脏集合为空时返回空字符串。
const claimDirtyScript = `
local member = redis.call('SRANDMEMBER', KEYS[1])
if member == false then
  return ''
end
redis.call('SMOVE', KEYS[1], KEYS[2], member)
return member
`

// BehaviorWorker 行为事件消费者。
type BehaviorWorker struct {
	svcCtx   *svc.ServiceContext
	rule     config.BehaviorRule
	skew     *clockSkewTracker
	stop     chan struct{}
	stopOnce sync.Once
}

// NewBehaviorWorker 创建行为事件消费者，并为未配置的判定阈值填充默认值。
func NewBehaviorWorker(svcCtx *svc.ServiceContext) *BehaviorWorker {
	rule := svcCtx.Config.Behavior.Rule
	rule.Fill()
	return &BehaviorWorker{
		svcCtx: svcCtx,
		rule:   rule,
		skew:   newClockSkewTracker(),
		stop:   make(chan struct{}),
	}
}

// Start 订阅三个 Topic 并启动消费 + 小时指标 flush 定时任务。
func (bw *BehaviorWorker) Start() error {
	bw.recoverProcessingMembers()
	if err := bw.svcCtx.BehaviorConsumer.Subscribe(bhv.TopicFeedBehaviorEvent, bw.HandleBehaviorMessage); err != nil {
		return err
	}
	if err := bw.svcCtx.BehaviorConsumer.Subscribe(intev.TopicInteractionEvent, bw.HandleInteractionMessage); err != nil {
		return err
	}
	if err := bw.svcCtx.BehaviorConsumer.Subscribe(cmtev.TopicCommentEvent, bw.HandleCommentMessage); err != nil {
		return err
	}
	go bw.flushLoop()
	return bw.svcCtx.BehaviorConsumer.Start()
}

// Shutdown 停止消费与 flush 任务，并做最后一次 flush 尽量减少数据滞留。
func (bw *BehaviorWorker) Shutdown() error {
	bw.stopOnce.Do(func() { close(bw.stop) })
	err := bw.svcCtx.BehaviorConsumer.Shutdown()
	bw.flushOnce(context.Background())
	return err
}

// ---------- feed-behavior-event ----------

// HandleBehaviorMessage 处理一条行为事件消息。返回 error 时 RocketMQ 会重试投递。
func (bw *BehaviorWorker) HandleBehaviorMessage(ctx context.Context, msg *primitive.MessageExt) error {
	var ev bhv.FeedBehaviorEvent
	if err := json.Unmarshal(msg.Body, &ev); err != nil {
		logBrokenBody(ctx, bhv.TopicFeedBehaviorEvent, msg, err)
		return nil // 消息体损坏不可恢复，吞掉避免死信堆积
	}
	if err := ev.Validate(); err != nil {
		logx.WithContext(ctx).Errorf("behavior-worker: invalid event topic=%s msgId=%s event_id=%s feed=%d action=%s err=%v",
			bhv.TopicFeedBehaviorEvent, msg.MsgId, ev.EventID, ev.FeedID, ev.ActionType, err)
		return nil // 非法事件直接丢弃，重试也不会变合法
	}

	err := bw.HandleBehaviorEvent(ctx, &ev)
	if err != nil {
		logRetry(ctx, bhv.TopicFeedBehaviorEvent, msg, "event_id="+ev.EventID, err)
	}
	return err
}

// logBrokenBody 记录无法反序列化的消息。
//
// 这类消息不会重投，日志里必须留下 msgId + 消息体长度，否则事后无从定位是哪个生产者写坏的。
func logBrokenBody(ctx context.Context, topic string, msg *primitive.MessageExt, err error) {
	logx.WithContext(ctx).Errorf("behavior-worker: broken body topic=%s msgId=%s bodyLen=%d reconsumeTimes=%d err=%v",
		topic, msg.MsgId, len(msg.Body), msg.ReconsumeTimes, err)
}

// logRetry 记录一次将触发 RocketMQ 重投的失败。
//
// reconsumeTimes 用于区分「偶发抖动」与「毒丸消息」：持续增长说明该消息每次都失败，
// 即将进入死信队列（%RETRY%/%DLQ%），需要人工介入。
func logRetry(ctx context.Context, topic string, msg *primitive.MessageExt, detail string, err error) {
	logx.WithContext(ctx).Errorf("behavior-worker: handle failed, will retry topic=%s msgId=%s reconsumeTimes=%d bornTs=%d %s err=%v",
		topic, msg.MsgId, msg.ReconsumeTimes, msg.BornTimestamp, detail, err)
}

// HandleBehaviorEvent 单条行为事件处理。
func (bw *BehaviorWorker) HandleBehaviorEvent(ctx context.Context, ev *bhv.FeedBehaviorEvent) error {
	// 2) 幂等：event_id 绝对去重
	idemKey := ev.IdemKey()
	fresh, err := bw.svcCtx.Redis.SetnxEx(idemKey, "1", bhv.IdemExpireSec)
	if err != nil {
		return err
	}
	if !fresh {
		return nil // 已处理过
	}

	// 数据质量：采样客户端时钟偏差（规则 5），随 flush 周期汇报 P99
	if ev.ServerTime > 0 && ev.Timestamp > 0 {
		bw.skew.Observe(ev.ServerTime - ev.Timestamp)
	}

	// 说明：此处不再校验 feed 是否存在 / status==NORMAL，也不查 author_id——
	// 这些已由 Gateway 在上报时用一次 BatchGetFeeds 批量完成（见 03-behavior-event.md §3 步骤 5）。
	// 若在消费端逐条重查，曝光量级下会退化成「每条事件一次 Feed RPC」，
	// 而设计的消费流程（§4）中并无该步骤。

	// 3) 规则重判：EFFECTIVE_PLAY / FINISH / SKIP 以服务端阈值为准
	action, keep := bw.rejudge(ev)
	if !keep {
		return nil // 如 SKIP 未携带真实 watch_duration_ms
	}
	if action != ev.ActionType {
		logx.WithContext(ctx).Infof("behavior-worker: rejudged event_id=%s feed=%d %s -> %s watch=%d media=%d",
			ev.EventID, ev.FeedID, ev.ActionType, action, ev.WatchDurationMs, ev.MediaDurationMs)
		ev.ActionType = action
	}

	// 4) EXPOSE 去重：同一 (request_id, feed_id) 只计一次曝光。
	// 仅当 request_id 非空时去重——空值下去重键会退化为 {feed_id} 维度，
	// 不同用户/不同请求的曝光会互相去重，曝光指标被系统性低估，因此跳过去重。
	exposeKey := "" // 非空表示本次已注册曝光去重键，失败时需一并清理
	if ev.ActionType == bhv.ActionExpose {
		if ev.RequestID != "" {
			first, err := bw.svcCtx.Redis.SetnxEx(ev.ExposeDedupKey(), "1", bhv.ExposeIdemExpireSec)
			if err != nil {
				bw.dropIdem(idemKey)
				return err
			}
			if !first {
				return nil // 重复曝光
			}
			exposeKey = ev.ExposeDedupKey()
		}
	} else if ev.ActionType == bhv.ActionPlay && ev.RequestID != "" {
		// 无 EXPOSE 的 PLAY 仍接受，但打标 abnormal 供数据质量监控
		exposed, err := bw.svcCtx.Redis.Exists(ev.ExposeDedupKey())
		if err == nil && !exposed {
			ev.Abnormal = true
		}
	}

	// 5) 明细落库（EXPOSE 采样，其余全量）——靠 uk_event_id 幂等
	if bw.shouldPersist(ev.ActionType) {
		if err := bw.persistDetail(ctx, ev); err != nil {
			bw.dropOnFailure(idemKey, exposeKey)
			return err
		}
	}

	// 6) 指标累加（放最后：脚本原子执行，失败无副作用，重试不会重复累加）
	if err := bw.accumulateBehavior(ev); err != nil {
		bw.dropOnFailure(idemKey, exposeKey)
		return err
	}

	// 7) 兴趣更新（待 Content 服务落地）
	bw.updateInterest(ev.UserID, ev.FeedID, ev.ActionType)
	return nil
}

// dropOnFailure 持久化失败时删除幂等键，并清理本次已注册的曝光去重键。
//
// 曝光去重键若不删除，RocketMQ 重试时会被误判为「重复曝光」而直接跳过，
// 而实际明细未落库、指标未累加——形成永久数据丢失。
func (bw *BehaviorWorker) dropOnFailure(idemKey, exposeKey string) {
	bw.dropIdem(idemKey)
	if exposeKey != "" {
		if _, err := bw.svcCtx.Redis.Del(exposeKey); err != nil {
			logx.Errorf("behavior-worker: drop expose dedup key=%s err=%v", exposeKey, err)
		}
	}
}

// rejudge 服务端重新判定行为类型。
//
// 客户端可被篡改，把阈值放在服务端，口径变更也无需发版。
// 返回 (校正后的 action, 是否保留该事件)。
func (bw *BehaviorWorker) rejudge(ev *bhv.FeedBehaviorEvent) (string, bool) {
	switch ev.ActionType {
	case bhv.ActionExpose, bhv.ActionPlay, bhv.ActionShare:
		return ev.ActionType, true // 这三类无时长口径，不重判
	}

	watch := ev.WatchDurationMs
	media := ev.MediaDurationMs
	r := bw.rule

	switch {
	case media > 0 && float64(watch) >= r.FinishRatio*float64(media):
		return bhv.ActionFinish, true
	case watch >= r.EffectivePlayMs || (media > 0 && float64(watch) >= r.EffectivePlayRatio*float64(media)):
		return bhv.ActionEffectivePlay, true
	default:
		// SKIP 必须携带真实 watch_duration_ms，否则丢弃
		if watch <= 0 {
			return bhv.ActionSkip, false
		}
		return bhv.ActionSkip, true
	}
}

// shouldPersist 明细落库策略：EXPOSE 采样，其余全量。
func (bw *BehaviorWorker) shouldPersist(action string) bool {
	if action != bhv.ActionExpose {
		return true
	}
	rate := bw.svcCtx.Config.Behavior.ExposeSampleRate
	switch {
	case rate <= 0:
		return false
	case rate >= 1:
		return true
	default:
		return rand.Float64() < rate
	}
}

// persistDetail 写入行为明细。唯一键 uk_event_id 冲突视为已落库（重试场景）。
func (bw *BehaviorWorker) persistDetail(ctx context.Context, ev *bhv.FeedBehaviorEvent) error {
	var abnormal int64
	if ev.Abnormal {
		abnormal = 1
	}
	_, err := bw.svcCtx.FeedBehaviorEventsModel.Insert(ctx, &model.FeedBehaviorEvents{
		Id:              uint64(bw.svcCtx.IdGen()),
		EventId:         ev.EventID,
		RequestId:       ev.RequestID,
		UserId:          uint64(ev.UserID),
		FeedId:          uint64(ev.FeedID),
		AuthorId:        uint64(ev.AuthorID),
		ActionType:      ev.ActionType,
		Position:        int64(ev.Position),
		WatchDurationMs: ev.WatchDurationMs,
		MediaDurationMs: ev.MediaDurationMs,
		Abnormal:        abnormal,
		EventTime:       eventTime(ev),
	})
	if err != nil && isDuplicateErr(err) {
		return nil
	}
	return err
}

// accumulateBehavior 把行为事件累加到对应小时桶。
//
// 行为计数与观看时长合并为一次原子脚本：若拆成两次调用，第一次成功、第二次失败时
// 返回 error 并删除幂等键，MQ 重试会让第一次累加再次执行，计数重复。
func (bw *BehaviorWorker) accumulateBehavior(ev *bhv.FeedBehaviorEvent) error {
	field, ok := behaviorField(ev.ActionType)
	if !ok {
		return nil
	}
	bucket := eventTime(ev).Truncate(time.Hour)
	if ev.WatchDurationMs > 0 {
		return bw.incrMetrics(ev.FeedID, ev.AuthorID, bucket, field, 1, keys.FieldWatchMs, int(ev.WatchDurationMs))
	}
	return bw.incrMetrics(ev.FeedID, ev.AuthorID, bucket, field, 1, "", 0)
}

// behaviorField 行为类型 → 小时桶字段。
func behaviorField(action string) (string, bool) {
	switch action {
	case bhv.ActionExpose:
		return keys.FieldExpose, true
	case bhv.ActionPlay:
		return keys.FieldPlay, true
	case bhv.ActionEffectivePlay:
		return keys.FieldEffectivePlay, true
	case bhv.ActionFinish:
		return keys.FieldFinish, true
	case bhv.ActionSkip:
		return keys.FieldSkip, true
	case bhv.ActionShare:
		return keys.FieldShare, true
	default:
		return "", false
	}
}

// ---------- interaction-event ----------

// HandleInteractionMessage 把点赞/收藏计入指标（不落库）。
func (bw *BehaviorWorker) HandleInteractionMessage(ctx context.Context, msg *primitive.MessageExt) error {
	var ev intev.Event
	if err := json.Unmarshal(msg.Body, &ev); err != nil {
		logBrokenBody(ctx, intev.TopicInteractionEvent, msg, err)
		return nil
	}
	if ev.FeedID <= 0 || ev.EventID == "" {
		return nil
	}

	var field string
	var delta int
	switch ev.ActionType {
	case intev.ActionLike:
		field, delta = keys.FieldLike, 1
	case intev.ActionUnlike:
		field, delta = keys.FieldLike, -1
	case intev.ActionCollect:
		field, delta = keys.FieldCollect, 1
	case intev.ActionUncollect:
		field, delta = keys.FieldCollect, -1
	default:
		return nil
	}
	err := bw.applyCounterEvent(ctx, keys.CounterEventIdem(ev.EventID), ev.FeedID, ev.Timestamp, field, delta, ev.UserID)
	if err != nil {
		logRetry(ctx, intev.TopicInteractionEvent, msg, "event_id="+ev.EventID, err)
	}
	return err
}

// ---------- comment-event ----------

// HandleCommentMessage 把评论计入指标（不落库）。
func (bw *BehaviorWorker) HandleCommentMessage(ctx context.Context, msg *primitive.MessageExt) error {
	var ev cmtev.Event
	if err := json.Unmarshal(msg.Body, &ev); err != nil {
		logBrokenBody(ctx, cmtev.TopicCommentEvent, msg, err)
		return nil
	}
	if ev.FeedID <= 0 || ev.EventID == "" {
		return nil
	}

	var delta int
	switch ev.ActionType {
	case cmtev.ActionCreate:
		delta = 1
	case cmtev.ActionDelete:
		delta = -1
	default:
		return nil
	}
	err := bw.applyCounterEvent(ctx, keys.CounterEventIdem(ev.EventID), ev.FeedID, ev.Timestamp, keys.FieldComment, delta, ev.UserID)
	if err != nil {
		logRetry(ctx, cmtev.TopicCommentEvent, msg, "event_id="+ev.EventID, err)
	}
	return err
}

// applyCounterEvent 幂等地把一次计数变化累加到小时桶。
//
// 计数按小时净增减记录（如上一小时点赞、本小时取消 → 本小时为 -1），跨小时求和即为净值，
// 因此这里不做非负截断。
func (bw *BehaviorWorker) applyCounterEvent(ctx context.Context, idemKey string, feedID, tsMs int64, field string, delta int, userID int64) error {
	fresh, err := bw.svcCtx.Redis.SetnxEx(idemKey, "1", bhv.IdemExpireSec)
	if err != nil {
		return err
	}
	if !fresh {
		return nil
	}

	// author_id 由行为事件补齐；此处传 0 表示「未知，不覆盖已有值」
	bucket := msToTime(tsMs).Truncate(time.Hour)
	if err := bw.incrMetric(feedID, 0, bucket, field, delta); err != nil {
		logx.WithContext(ctx).Errorf("behavior-worker: incr %s feed=%d err=%v", field, feedID, err)
		bw.dropIdem(idemKey)
		return err
	}
	bw.updateInterest(userID, feedID, field)
	return nil
}

// ---------- 指标累加与落库 ----------

// incrMetric 原子累加小时桶单个字段并打脏标记（见 incrMetrics）。
func (bw *BehaviorWorker) incrMetric(feedID, authorID int64, bucket time.Time, field string, delta int) error {
	return bw.incrMetrics(feedID, authorID, bucket, field, delta, "", 0)
}

// incrMetrics 原子累加小时桶一个或两个字段（如行为计数 + 观看时长），并打脏标记。
//
// 通过单条 Lua 脚本完成，杜绝「HINCRBY 已执行、SADD 打脏标记失败」的半成功状态：
// 脚本失败时无任何副作用，上层删除幂等键后 MQ 重试不会重复累加。
func (bw *BehaviorWorker) incrMetrics(feedID, authorID int64, bucket time.Time, field1 string, delta1 int, field2 string, delta2 int) error {
	statHour := bucket.Format(keys.StatHourLayout)
	key := keys.MetricsHour(feedID, statHour)

	// 只在已知作者时写入，避免用 0 覆盖真实 author_id
	author := "0"
	if authorID > 0 {
		author = strconv.FormatInt(authorID, 10)
	}

	_, err := bw.svcCtx.Redis.Eval(behaviorIncrScript,
		[]string{key, keys.MetricsDirtySet},
		field1, delta1, field2, delta2, keys.FieldAuthorID, author,
		keys.MetricsDirtyMember(feedID, statHour), keys.TTLMetricsHour)
	return err
}

// flushLoop 定时把脏桶刷入 MySQL。
func (bw *BehaviorWorker) flushLoop() {
	interval := time.Duration(bw.svcCtx.Config.Behavior.MetricsFlushIntervalSec) * time.Second
	if interval <= 0 {
		interval = 60 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-bw.stop:
			return
		case <-ticker.C:
			bw.flushOnce(context.Background())
		}
	}
}

// flushOnce 认领脏桶（原子移入处理中集合，单轮上限 MetricsFlushBatch）并把绝对值写入小时表。
//
// 写绝对值而非增量，所以：flush 重复执行 / 中途失败重试，都不会造成指标翻倍。
// 也正因如此，落库后不能删除 Redis Hash——同小时的后续事件仍要在其基础上累加。
//
// 与 SPOP 直接移除不同：成员认领后保存在 MetricsProcessingSet，落库成功才释放；
// 若进程在认领后、落库完成前崩溃，成员残留于处理中集合，由启动自愈
// （recoverProcessingMembers）合并回脏集合，杜绝该小时指标永久滞留 Redis。
func (bw *BehaviorWorker) flushOnce(ctx context.Context) {
	bw.reportClockSkew(ctx)

	batch := bw.svcCtx.Config.Behavior.MetricsFlushBatch
	if batch <= 0 {
		batch = 500
	}

	for i := 0; i < batch; i++ {
		member, err := bw.claimDirtyMember()
		if err != nil {
			logx.WithContext(ctx).Errorf("behavior-worker: claim dirty err=%v", err)
			return
		}
		if member == "" {
			return // 脏集合已空
		}

		feedID, statHour, ok := parseDirtyMember(member)
		if !ok {
			logx.WithContext(ctx).Errorf("behavior-worker: bad dirty member=%s", member)
			bw.releaseClaim(member, false) // 无法解析的成员直接丢弃
			continue
		}

		key := keys.MetricsHour(feedID, statHour.Format(keys.StatHourLayout))
		fields, err := bw.svcCtx.Redis.Hgetall(key)
		if err != nil {
			logx.WithContext(ctx).Errorf("behavior-worker: hgetall %s err=%v", key, err)
			bw.releaseClaim(member, true)
			continue
		}
		if len(fields) == 0 {
			bw.releaseClaim(member, false) // Hash 已过期，无需落库
			continue
		}

		row := &model.FeedMetricsHourly{
			FeedId:             uint64(feedID),
			AuthorId:           uint64(atoi64(fields[keys.FieldAuthorID])),
			StatHour:           statHour,
			ExposeCount:        atoi64(fields[keys.FieldExpose]),
			PlayCount:          atoi64(fields[keys.FieldPlay]),
			EffectivePlayCount: atoi64(fields[keys.FieldEffectivePlay]),
			FinishCount:        atoi64(fields[keys.FieldFinish]),
			SkipCount:          atoi64(fields[keys.FieldSkip]),
			ShareCount:         atoi64(fields[keys.FieldShare]),
			LikeCount:          atoi64(fields[keys.FieldLike]),
			CollectCount:       atoi64(fields[keys.FieldCollect]),
			CommentCount:       atoi64(fields[keys.FieldComment]),
			WatchDurationMs:    atoi64(fields[keys.FieldWatchMs]),
		}

		// 数据质量规则 1~4：只告警不阻断，避免把「部分不准」放大成「整桶没有」
		for _, issue := range CheckMetricsRow(row) {
			logx.WithContext(ctx).Errorf("behavior-worker: data quality feed=%d hour=%s %s",
				feedID, statHour.Format(keys.StatHourLayout), issue)
		}

		if err := bw.svcCtx.FeedMetricsHourlyModel.Upsert(ctx, row); err != nil {
			logx.WithContext(ctx).Errorf("behavior-worker: upsert metrics feed=%d hour=%s err=%v",
				feedID, statHour.Format(keys.StatHourLayout), err)
			bw.releaseClaim(member, true) // 放回脏集合，下轮重试，否则本小时增量永久滞留
			continue
		}
		bw.releaseClaim(member, false) // 落库成功，释放认领
	}
}

// claimDirtyMember 原子地从脏集合认领一个成员到处理中集合。
func (bw *BehaviorWorker) claimDirtyMember() (string, error) {
	res, err := bw.svcCtx.Redis.Eval(claimDirtyScript, []string{keys.MetricsDirtySet, keys.MetricsProcessingSet})
	if err != nil {
		return "", err
	}
	member, _ := res.(string)
	return member, nil
}

// releaseClaim 释放对桶成员的认领：requeue=true 时先放回脏集合（下轮重试），
// 再移除处理中标记；requeue=false 仅移除处理中标记（成功落库或无需处理）。
//
// 先放回脏集合再移除认领，崩溃窗口内成员可能同时存在于两个集合，但重复处理
// 安全（Upsert 写绝对值，不会翻倍）。
func (bw *BehaviorWorker) releaseClaim(member string, requeue bool) {
	if requeue {
		if _, err := bw.svcCtx.Redis.Sadd(keys.MetricsDirtySet, member); err != nil {
			logx.Errorf("behavior-worker: requeue dirty member=%s err=%v", member, err)
		}
	}
	if _, err := bw.svcCtx.Redis.Srem(keys.MetricsProcessingSet, member); err != nil {
		logx.Errorf("behavior-worker: release claim member=%s err=%v", member, err)
	}
}

// recoverProcessingMembers 启动时回收上次进程崩溃遗留的处理中成员。
//
// 崩溃发生在「认领到 processing 之后、落库完成之前」时，成员残留在处理中集合，
// 不回收则该桶永远不再被 flush。用 SUNIONSTORE 合并回脏集合后清空处理中集合。
func (bw *BehaviorWorker) recoverProcessingMembers() {
	if _, err := bw.svcCtx.Redis.Sunionstore(keys.MetricsDirtySet, keys.MetricsDirtySet, keys.MetricsProcessingSet); err != nil {
		logx.Errorf("behavior-worker: recover processing members err=%v", err)
		return
	}
	if _, err := bw.svcCtx.Redis.Del(keys.MetricsProcessingSet); err != nil {
		logx.Errorf("behavior-worker: clear processing set err=%v", err)
	}
}

// reportClockSkew 数据质量规则 5：汇报客户端时间偏差 P99。
//
// 超阈值说明大量客户端时钟不可信，需排查上报 SDK；服务端已用 server_time
// 兜底分桶，因此这里只告警不做任何拦截。
func (bw *BehaviorWorker) reportClockSkew(ctx context.Context) {
	unhealthy, p99, n := bw.skew.Unhealthy()
	if !unhealthy {
		return
	}
	logx.WithContext(ctx).Errorf("behavior-worker: data quality clock_skew_p99=%dms threshold=%dms samples=%d",
		p99, skewP99ThresholdMs, n)
}

// parseDirtyMember 解析脏集合成员 "{feed_id}:{yyyyMMddHH}"。
func parseDirtyMember(member string) (int64, time.Time, bool) {
	idx := strings.LastIndex(member, ":")
	if idx <= 0 {
		return 0, time.Time{}, false
	}
	feedID, err := strconv.ParseInt(member[:idx], 10, 64)
	if err != nil || feedID <= 0 {
		return 0, time.Time{}, false
	}
	statHour, err := time.ParseInLocation(keys.StatHourLayout, member[idx+1:], time.Local)
	if err != nil {
		return 0, time.Time{}, false
	}
	return feedID, statHour, true
}

// ---------- 工具 ----------

// dropIdem 持久化失败时删除幂等 key。
//
// 为什么必须删：SETNX 成功但后续落库失败时，若不删除，RocketMQ 重试会被误判为
// 「已处理」而永久丢数据。
func (bw *BehaviorWorker) dropIdem(key string) {
	if _, err := bw.svcCtx.Redis.Del(key); err != nil {
		logx.Errorf("behavior-worker: drop idem key=%s err=%v", key, err)
	}
}

// eventTime 取事件时间：优先服务端接收时间（抗客户端时钟漂移），其次客户端时间。
func eventTime(ev *bhv.FeedBehaviorEvent) time.Time {
	if ev.ServerTime > 0 {
		return msToTime(ev.ServerTime)
	}
	return msToTime(ev.Timestamp)
}

func msToTime(ms int64) time.Time {
	if ms <= 0 {
		return time.Now()
	}
	return time.UnixMilli(ms)
}

func atoi64(s string) int64 {
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

// isDuplicateErr 判断是否 MySQL 唯一键冲突（1062）。
func isDuplicateErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "Duplicate entry") || strings.Contains(err.Error(), "Error 1062")
}

// updateInterest 更新用户兴趣画像（步骤 7）。
//
// 兴趣聚合依赖阶段二 Content 服务产出的内容画像（tags/category），该服务尚未实现
// （见 06-user-interest.md）。此处仅保留扩展点：待 06 落地后，按行为权重 + 时间衰减
// 聚合到 user_interest_profiles（MySQL）+ user:interest:{uid}（Redis）。
// 当前不写入，避免产生无标签来源的空数据。
func (bw *BehaviorWorker) updateInterest(userID, feedID int64, action string) {
	logx.Debugf("behavior-worker: interest update deferred (06-user-interest.md) uid=%d feed=%d action=%s",
		userID, feedID, action)
}
