// Package worker 实现 Feed 服务的异步消费者（后台运行于 Feed RPC 进程内）。
// 它从 RocketMQ 订阅 feed.created / feed.deleted 事件，将发帖、删帖的副作用落地到
// Redis 各时间流结构：inbox（推模式）、outbox、推荐池、同城池。
// 所有写入均为幂等操作（ZADD/ZREM），消费失败返回 error 由 RocketMQ 重试。
package worker

import (
	"context"
	"encoding/json"
	"strconv"

	red "github.com/apache/rocketmq-client-go/v2/primitive"
	goredis "github.com/redis/go-redis/v9"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/keys"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/svc"
	"github.com/sponge-dad/feed/app/relation/rpc/relationclient"
	commentEvent "github.com/sponge-dad/feed/common/event/comment"
	feedEvent "github.com/sponge-dad/feed/common/event/feed"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

const (
	// inboxCap 收件箱保留最近条数（普通用户推模式），超出裁剪最旧。
	inboxCap = 1000
	// outboxCap 发件箱保留最近条数（大V拉模式），超出裁剪最旧。
	outboxCap = 2000
	// fanoutBatchSize 推模式单批拉取粉丝数量。
	fanoutBatchSize = 500
)

// Redis key 统一委托给 internal/keys 包维护（见 keys.Inbox/Outbox/City/Recommend）。
// 命名约定见 docs/design/feed/06-cache-strategy.md。

// recommendScore 推荐池分数，当前以发布时间秒级为主，未来可叠加热度/质量分。
func recommendScore(sec float64) float64 { return sec }

// zAdd 向有序集合写入成员，score 取秒级时间戳。
func zAdd(rdb *redis.Redis, key string, score float64, member int64) error {
	_, err := rdb.Zadd(key, int64(score), strconv.FormatInt(member, 10))
	return err
}

// zAddTrim 写入并裁剪到 cap 条（保留分数最高的 cap 条，即删除最旧数据）。
func zAddTrim(rdb *redis.Redis, key string, score float64, member int64, cap int) error {
	if err := zAdd(rdb, key, score, member); err != nil {
		return err
	}
	// ZREMRANGEBYRANK key start stop 用于按照集合成员从小到大顺序删除从 start 到 stop 的成员。
	// 因为是时间流，所以分数小的成员更旧，start=0 表示删除第 0 条，stop=-(cap+1) 表示删除倒数第 (cap+1) 条。
	// start=0 stop=-(cap+1) 表示删除第 0 条到倒数第 (cap+1) 条，即删除最旧数据。
	// 这样剩下的元素数量就是 cap。
	_, err := rdb.Zremrangebyrank(key, 0, int64(-(cap + 1)))
	return err
}

// zRem 从有序集合移除成员（删帖事件使用），忽略返回值：缓存清理失败不阻塞主流程。
func zRem(rdb *redis.Redis, key, member string) {
	_, _ = rdb.Zrem(key, member)
}

// Worker 持有 ServiceContext，负责消费 Feed 事件并维护 Redis 时间流。
type Worker struct {
	svcCtx *svc.ServiceContext
}

// NewWorker 创建 Worker 实例。
func NewWorker(ctx *svc.ServiceContext) *Worker {
	return &Worker{svcCtx: ctx}
}

// Start 订阅事件 topic 并启动消费者；订阅必须在 Start 之前完成。
func (wk *Worker) Start() error {
	if err := wk.svcCtx.Consumer.Subscribe(feedEvent.TopicFeedCreated, wk.handleFeedCreate); err != nil {
		return err
	}
	if err := wk.svcCtx.Consumer.Subscribe(feedEvent.TopicFeedDeleted, wk.handleFeedDelete); err != nil {
		return err
	}
	// 评论事件：异步维护 feeds.comment_count 镜像列（增量 +1/-1，不依赖 Comment RPC）。
	if err := wk.svcCtx.Consumer.Subscribe(commentEvent.TopicCommentEvent, wk.handleCommentEvent); err != nil {
		return err
	}
	return wk.svcCtx.Consumer.Start()
}

// handleFeedCreate 处理发帖事件：写 outbox + 推荐/同城池；普通用户额外推粉丝 inbox。
func (wk *Worker) handleFeedCreate(ctx context.Context, msg *red.MessageExt) error {
	var ev feedEvent.EventFeedCreate
	if err := json.Unmarshal(msg.Body, &ev); err != nil {
		// 不可恢复：消息体损坏，记录后返回 nil 避免死信堆积。
		logx.Errorf("unmarshal feed.created failed body=%s err=%v", string(msg.Body), err)
		return nil
	}
	scoreSec := float64(ev.CreatedAt / 1000)
	rdb := wk.svcCtx.Redis

	// 1. 作者 outbox：普通用户与大V均写，供拉模式及个人主页使用。（outbox中的旧数据会被删除）
	if err := zAddTrim(rdb, keys.Outbox(ev.UserID), scoreSec, ev.FeedID, outboxCap); err != nil {
		return err
	}
	// 2. 推荐池 + 同城池。
	if err := zAdd(rdb, keys.Recommend(), recommendScore(scoreSec), ev.FeedID); err != nil {
		return err
	}
	if ev.CityCode != "" {
		if err := zAdd(rdb, keys.City(ev.CityCode), scoreSec, ev.FeedID); err != nil {
			return err
		}
	}
	// 3. 普通用户走推模式，批量写入粉丝 inbox。
	if !ev.IsVipFeed {
		return wk.pushToFans(ctx, ev.UserID, scoreSec, ev.FeedID)
	}
	return nil
}

// handleFeedDelete 处理删帖事件：从推荐/同城池、outbox 移除；普通用户额外清理粉丝 inbox。
func (wk *Worker) handleFeedDelete(ctx context.Context, msg *red.MessageExt) error {
	var ev feedEvent.EventFeedDeleted
	if err := json.Unmarshal(msg.Body, &ev); err != nil {
		logx.Errorf("unmarshal feed.deleted failed body=%s err=%v", string(msg.Body), err)
		return nil
	}
	rdb := wk.svcCtx.Redis
	member := strconv.FormatInt(ev.FeedID, 10)
	// 缓存清理失败不阻塞主流程，仅记录日志由后续读触发重建。
	zRem(rdb, keys.Recommend(), member)
	if ev.CityCode != "" {
		zRem(rdb, keys.City(ev.CityCode), member)
	}
	zRem(rdb, keys.Outbox(ev.UserID), member)
	// 普通用户需清理粉丝 inbox 中的该帖子。
	if !ev.IsVipFeed {
		return wk.removeFromFans(ctx, ev.UserID, ev.FeedID)
	}
	return nil
}

// pushToFans 分批拉取作者粉丝，用 Pipeline 批量写入 inbox 并在每批内裁剪容量。
func (wk *Worker) pushToFans(ctx context.Context, authorID int64, scoreSec float64, feedID int64) error {
	page := int64(1)
	for {
		fans, err := wk.svcCtx.RelationRpc.GetFans(ctx, &relationclient.GetFansReq{
			UserId:   authorID,
			Page:     page,
			PageSize: fanoutBatchSize,
		})
		if err != nil {
			return err
		}
		ids := fans.GetFollowerIds()
		if len(ids) == 0 {
			break
		}
		member := strconv.FormatInt(feedID, 10)
		pipeErr := wk.svcCtx.Redis.Pipelined(func(pipe redis.Pipeliner) error {
			for _, fanID := range ids {
				key := keys.Inbox(fanID)
				pipe.ZAdd(context.Background(), key, goredis.Z{Score: scoreSec, Member: member})
				// 保留分数最高的 inboxCap 条（删除最旧）。
				pipe.ZRemRangeByRank(context.Background(), key, 0, int64(-(inboxCap + 1)))
			}
			return nil
		})
		if pipeErr != nil {
			return pipeErr
		}
		if int64(len(ids)) < fanoutBatchSize {
			break
		}
		page++
	}
	return nil
}

// removeFromFans 分批拉取粉丝，批量从 inbox 移除被删帖子（ZREM 幂等）。
func (wk *Worker) removeFromFans(ctx context.Context, authorID int64, feedID int64) error {
	page := int64(1)
	for {
		fans, err := wk.svcCtx.RelationRpc.GetFans(ctx, &relationclient.GetFansReq{
			UserId:   authorID,
			Page:     page,
			PageSize: fanoutBatchSize,
		})
		if err != nil {
			return err
		}
		ids := fans.GetFollowerIds()
		if len(ids) == 0 {
			break
		}
		member := strconv.FormatInt(feedID, 10)
		pipeErr := wk.svcCtx.Redis.Pipelined(func(pipe redis.Pipeliner) error {
			for _, fanID := range ids {
				pipe.ZRem(context.Background(), keys.Inbox(fanID), member)
			}
			return nil
		})
		if pipeErr != nil {
			return pipeErr
		}
		if int64(len(ids)) < fanoutBatchSize {
			break
		}
		page++
	}
	return nil
}

// handleCommentEvent 处理评论事件（CREATE / DELETE 共用）：按 action_type 对 feeds.comment_count
// 做增量更新（CREATE +1，DELETE -1），不再调用 Comment RPC，彻底消除 Feed → Comment 循环依赖。
// 计数下限为 0（不会因异常 DELETE 事件出现负数）。
// 幂等：以 event_id 去重（Redis SETNX，TTL 24h）；重复事件不会重复增减。
func (wk *Worker) handleCommentEvent(ctx context.Context, msg *red.MessageExt) error {
	var ev commentEvent.Event
	if err := json.Unmarshal(msg.Body, &ev); err != nil {
		// 不可恢复：消息体损坏，记录后返回 nil 避免死信堆积。
		logx.Errorf("unmarshal comment-event failed body=%s err=%v", string(msg.Body), err)
		return nil
	}

	// 幂等去重：已处理过则直接返回。
	dedupKey := keys.CommentEventDedup(ev.EventID)
	ok, derr := wk.svcCtx.Redis.Setnx(dedupKey, "1")
	if derr != nil {
		// 去重标记写失败不阻塞主流程，继续执行增量更新（UPDATE 本身幂等，重复执行结果一致）。
		logx.Errorf("comment-event dedup setnx failed event_id=%s err=%v", ev.EventID, derr)
	} else if !ok {
		return nil
	}
	if eerr := wk.svcCtx.Redis.Expire(dedupKey, 24*3600); eerr != nil {
		logx.Errorf("comment-event dedup expire failed event_id=%s err=%v", ev.EventID, eerr)
	}

	// 按动作类型计算增量：CREATE +1，DELETE -1。
	var delta int64
	switch ev.ActionType {
	case commentEvent.ActionCreate:
		delta = 1
	case commentEvent.ActionDelete:
		delta = -1
	default:
		logx.Errorf("comment-event unknown action_type=%s event_id=%s", ev.ActionType, ev.EventID)
		return nil
	}

	// 增量更新 feeds.comment_count，SQL 层保证下限为 0（GREATEST(comment_count + delta, 0)）。
	if uerr := wk.svcCtx.FeedModel.IncrCommentCount(ctx, uint64(ev.FeedID), delta); uerr != nil {
		logx.Errorf("comment-event IncrCommentCount failed event_id=%s feed_id=%d delta=%d err=%v", ev.EventID, ev.FeedID, delta, uerr)
		wk.svcCtx.Redis.Del(dedupKey)
		return uerr
	}
	return nil
}
