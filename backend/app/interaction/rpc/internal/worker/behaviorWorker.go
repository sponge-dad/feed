// behaviorWorker.go
//
// 职责：feed-behavior-event 消费者（behavior-persistence-consumer）。
// 订阅行为埋点事件，做：
//   1) 幂等（event_id 绝对去重）
//   2) 服务端重判（feed 存在 + status==NORMAL；SHARE 不做 owner 校验）
//   3) 频率限制兜底（5/s/uid+action+feed）
//   4) EXPOSE 去重（同 user+feed+scene 24h 仅计一次）
//   5) 指标累加（Redis Hash，按小时桶）
//   6) 明细抽样落库（EXPOSE 抽样，主动行为全量）
//   7) 兴趣更新（见 06-用户兴趣画像.md，依赖 Content 服务，当前未实现）
//
// 设计见 docs/design/agent/03-behavior-event.md。与现有 interaction 落库 consumer 并列运行。
package worker

import (
	"context"
	"encoding/json"
	"math/rand"
	"strconv"
	"strings"
	"time"

	bhv "github.com/sponge-dad/feed/common/event/behavior"
	feedClient "github.com/sponge-dad/feed/app/feed/rpc/feedclient"
	feedpb "github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/app/interaction/model"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"

	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	behaviorIdemPrefix = "behavior:idem:"
	exposeIdemPrefix   = "expose:idem:"
	ratePrefix         = "behavior:rate:"
	metricsPrefix      = "feed:metrics:"
	dirtySetKey        = "feed:metrics:dirty"
	interestPrefix     = "user:interest:"

	idemExpire       = 24 * time.Hour
	exposeIdemExpire = 24 * time.Hour
)

// BehaviorWorker 行为埋点消费者。
type BehaviorWorker struct {
	svcCtx *svc.ServiceContext
	stop   chan struct{}
}

// NewBehaviorWorker 创建行为埋点消费者。
func NewBehaviorWorker(svcCtx *svc.ServiceContext) *BehaviorWorker {
	return &BehaviorWorker{svcCtx: svcCtx, stop: make(chan struct{})}
}

// Start 订阅 feed-behavior-event 并启动消费 + 小时级指标落库定时任务。
func (bw *BehaviorWorker) Start() error {
	if err := bw.svcCtx.BehaviorConsumer.Subscribe(bhv.TopicFeedBehaviorEvent, bw.HandleMessage); err != nil {
		return err
	}
	go bw.flushLoop()
	return bw.svcCtx.BehaviorConsumer.Start()
}

// Shutdown 停止消费与落库任务。
func (bw *BehaviorWorker) Shutdown() error {
	close(bw.stop)
	return bw.svcCtx.BehaviorConsumer.Shutdown()
}

// HandleMessage 处理一条行为事件消息。返回 error 时 RocketMQ 会重试投递。
func (bw *BehaviorWorker) HandleMessage(ctx context.Context, msg *primitive.MessageExt) error {
	var ev bhv.FeedBehaviorEvent
	if err := json.Unmarshal(msg.Body, &ev); err != nil {
		logx.WithContext(ctx).Errorf("behavior-worker: invalid body=%s err=%v", msg.Body, err)
		return nil // 消息体损坏，吞掉避免无限重试
	}
	return bw.HandleEvent(ctx, &ev)
}

// HandleEvent 单事件处理（幂等 / 重判 / 限流 / 去重 / 累加 / 落库 / 兴趣）。
func (bw *BehaviorWorker) HandleEvent(ctx context.Context, ev *bhv.FeedBehaviorEvent) error {
	// 1) 幂等：event_id 绝对去重（24h）
	idemKey := behaviorIdemPrefix + ev.EventKey()
	ok, err := bw.svcCtx.Redis.Setnx(idemKey, "1")
	if err != nil {
		return err
	}
	if !ok {
		return nil // 已处理过，幂等跳过
	}
	_ = bw.svcCtx.Redis.Expire(idemKey, int(idemExpire.Seconds()))

	// 2) 服务端重判：feed 存在 + status==NORMAL（SHARE 放开，任何登录用户可分享任意视频）
	ok, err = bw.checkFeedNormal(ctx, ev.FeedID)
	if err != nil {
		return err
	}
	if !ok {
		return nil // feed 不存在 / 非 NORMAL：丢弃
	}

	// 3) 频率限制兜底（gateway 已强校验，这里再挡一层）
	if limited, err := bw.rateLimit(ev); err != nil {
		return err
	} else if limited {
		return nil
	}

	// 4) EXPOSE 去重（同 user+feed+scene 24h 内只计一次有效曝光）
	if ev.Action == bhv.ProtoExpose {
		exKey := exposeIdemPrefix + strconv.FormatInt(ev.UserID, 10) + ":" +
			strconv.FormatInt(ev.FeedID, 10) + ":" + ev.Scene
		if ok, err := bw.svcCtx.Redis.Setnx(exKey, "1"); err != nil {
			return err
		} else if !ok {
			return nil // 重复曝光，丢弃
		}
		_ = bw.svcCtx.Redis.Expire(exKey, int(exposeIdemExpire.Seconds()))
	}

	// 5) 指标累加（按小时桶写入 Redis Hash，并标记脏集合）
	if err := bw.accumulate(ev); err != nil {
		return err
	}

	// 6) 明细抽样落库
	if shouldPersist(ev, bw.svcCtx.Config.Behavior.ExposeSampleRate) {
		if err := bw.persistDetail(ctx, ev); err != nil {
			logx.WithContext(ctx).Errorf("behavior-worker: persist detail failed uid=%d feed=%d action=%s err=%v",
				ev.UserID, ev.FeedID, ev.Action, err)
			// 明细落库失败不阻塞指标累加，仅记录
		}
	}

	// 7) 兴趣更新（见 06，依赖 Content 服务，当前未实现）
	bw.updateInterest(ev)
	return nil
}

// checkFeedNormal 服务端重判 feed 是否存在且 status==NORMAL。
func (bw *BehaviorWorker) checkFeedNormal(ctx context.Context, feedID int64) (bool, error) {
	resp, err := bw.svcCtx.FeedRpc.BatchGetFeeds(ctx, &feedClient.BatchGetFeedsReq{
		FeedIds: []int64{feedID},
	})
	if err != nil {
		return false, err
	}
	info, ok := resp.Feeds[feedID]
	if !ok || feedpb.FeedStatus(info.Status) != feedpb.FeedStatus_FEED_STATUS_NORMAL {
		return false, nil
	}
	return true, nil
}

// rateLimit 单 uid+action+feed 频率限制兜底（5/s）。
func (bw *BehaviorWorker) rateLimit(ev *bhv.FeedBehaviorEvent) (bool, error) {
	key := ratePrefix + strconv.FormatInt(ev.UserID, 10) + ":" + ev.Action + ":" + strconv.FormatInt(ev.FeedID, 10)
	cnt, err := bw.svcCtx.Redis.Incr(key)
	if err != nil {
		return false, err
	}
	if cnt == 1 {
		_ = bw.svcCtx.Redis.Expire(key, 1)
	}
	limit := bw.svcCtx.Config.Behavior.RateLimitPerActionFeedPerSec
	if limit <= 0 {
		limit = 5
	}
	return cnt > int64(limit), nil
}

// accumulate 把事件累加到对应小时的 Redis Hash，并加入脏集合等待落库。
func (bw *BehaviorWorker) accumulate(ev *bhv.FeedBehaviorEvent) error {
	bucket := time.Now().UTC().Truncate(time.Hour).Unix()
	key := metricsPrefix + strconv.FormatInt(ev.FeedID, 10) + ":" + strconv.FormatInt(bucket, 10)

	var field string
	switch ev.Action {
	case bhv.ProtoExpose:
		field = "expose"
	case bhv.ProtoLike:
		field = "like"
	case bhv.ProtoCollect:
		field = "collect"
	case bhv.ProtoComment:
		field = "comment"
	case bhv.ProtoShare:
		field = "share"
	default:
		field = "pv"
	}
	if _, err := bw.svcCtx.Redis.Hincrby(key, field, 1); err != nil {
		return err
	}
	// pv 始终 +1（曝光也算一次页面访问）
	if field != "pv" {
		if _, err := bw.svcCtx.Redis.Hincrby(key, "pv", 1); err != nil {
			return err
		}
	}
	// EXPOSE 累加停留时长
	if ev.Action == bhv.ProtoExpose && ev.Duration > 0 {
		if _, err := bw.svcCtx.Redis.Hincrby(key, "duration", int(ev.Duration)); err != nil {
			return err
		}
	}
	// 标记脏集合，定时任务据此落库
	if _, err := bw.svcCtx.Redis.Sadd(dirtySetKey, key); err != nil {
		return err
	}
	return nil
}

// shouldPersist 是否落库明细：EXPOSE 抽样，主动行为全量。
func shouldPersist(ev *bhv.FeedBehaviorEvent, exposeSampleRate float64) bool {
	if ev.Action == bhv.ProtoExpose {
		if exposeSampleRate <= 0 {
			return false
		}
		if exposeSampleRate >= 1 {
			return true
		}
		return rand.Float64() < exposeSampleRate
	}
	return true
}

// persistDetail 把行为明细写入 feed_behavior_events（抽样落库）。
func (bw *BehaviorWorker) persistDetail(ctx context.Context, ev *bhv.FeedBehaviorEvent) error {
	_, err := bw.svcCtx.FeedBehaviorEventsModel.Insert(ctx, &model.FeedBehaviorEvents{
		Id:        uint64(bw.svcCtx.IdGen()),
		EventId:   ev.EventID,
		UserId:    uint64(ev.UserID),
		FeedId:    uint64(ev.FeedID),
		Action:    ev.Action,
		TargetId:  uint64(ev.TargetID),
		ClientIp:  ev.ClientIP,
		UserAgent: ev.UserAgent,
		ReqId:     ev.ReqID,
		CreatedAt: time.Now(),
	})
	return err
}

// flushLoop 定时把脏集合中的小时指标刷入 MySQL。
func (bw *BehaviorWorker) flushLoop() {
	interval := time.Duration(bw.svcCtx.Config.Behavior.MetricsFlushIntervalSec) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
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

// flushOnce  draining 脏集合并逐键落库。
func (bw *BehaviorWorker) flushOnce(ctx context.Context) {
	for {
		key, err := bw.svcCtx.Redis.Spop(dirtySetKey)
		if err != nil || key == "" {
			return
		}
		fields, err := bw.svcCtx.Redis.Hgetall(key)
		if err != nil {
			logx.WithContext(ctx).Errorf("behavior-worker: hgetall %s err=%v", key, err)
			continue
		}
		feedID, bucket, ok := parseMetricsKey(key)
		if !ok {
			continue
		}
		m := &model.FeedMetricsHourly{
			FeedId:     uint64(feedID),
			HourBucket: time.Unix(bucket, 0).UTC(),
			Pv:         atoi64(fields["pv"]),
			Expose:     atoi64(fields["expose"]),
			Like:       atoi64(fields["like"]),
			Collect:    atoi64(fields["collect"]),
			Comment:    atoi64(fields["comment"]),
			Share:      atoi64(fields["share"]),
			DurationMs: atoi64(fields["duration"]),
		}
		if err := bw.svcCtx.FeedMetricsHourlyModel.Upsert(ctx, m); err != nil {
			logx.WithContext(ctx).Errorf("behavior-worker: upsert metrics feed=%d bucket=%d err=%v", feedID, bucket, err)
			continue
		}
		// 落库后清除该小时桶的 Redis Hash（下一小时自然使用新 key）
		_, _ = bw.svcCtx.Redis.Del(key)
	}
}

// parseMetricsKey 解析 "feed:metrics:{feedID}:{bucketEpoch}"。
func parseMetricsKey(key string) (int64, int64, bool) {
	const prefix = "feed:metrics:"
	if !strings.HasPrefix(key, prefix) {
		return 0, 0, false
	}
	parts := strings.Split(key[len(prefix):], ":")
	if len(parts) != 2 {
		return 0, 0, false
	}
	feedID, err1 := strconv.ParseInt(parts[0], 10, 64)
	bucket, err2 := strconv.ParseInt(parts[1], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return feedID, bucket, true
}

func atoi64(s string) int64 {
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

// updateInterest 更新用户兴趣画像（步骤 7）。
//
// 注意：兴趣画像聚合依赖阶段二 Content 服务产出的 content profile（tags/category），
// 该服务尚未实现（见 06-用户兴趣画像.md）。此处仅保留扩展点，待 06 落地后补全：
// 订阅 behavior/interaction/comment 事件，按行为权重 + 时间衰减聚合到
// user_interest_profiles（MySQL）+ user:interest:{uid}（Redis）。
// 当前不写 user_interest_profiles / user:interest，避免写入无标签来源的空数据。
func (bw *BehaviorWorker) updateInterest(ev *bhv.FeedBehaviorEvent) {
	logx.Infof("behavior interest update deferred (see docs/design/agent/06-user-interest.md): uid=%d action=%s feed=%d",
		ev.UserID, ev.Action, ev.FeedID)
}
