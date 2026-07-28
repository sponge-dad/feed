// worker.go
//
// 职责：interaction.event 持久化消费者（interaction-persistence-consumer）。
// 订阅互动事件，将点赞/收藏状态异步落库 MySQL（Redis 先行 + MQ 异步落库的落库端），
// 设计见 docs/design/interaction/07-mq-event.md。
//
// 幂等与乱序兜底：
//   - likes/collections 表 uk_user_feed 唯一键保证一条 (user, feed) 只有一行；
//   - 目标状态与当前状态一致时跳过（重复投递幂等）；
//   - 状态翻转使用条件更新 UpdateStatusIfNewer（仅当 updated_at <= 事件时间才生效），
//     旧事件晚到不会覆盖新状态；
//   - 「取消」事件先到且无记录时插入 status=2 墓碑行，防止乱序晚到的「点赞」事件复活状态
//     （对 07-mq-event.md §3.1 的加固，文档场景下该情况会被静默忽略）。
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/sponge-dad/feed/app/interaction/model"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"
	event "github.com/sponge-dad/feed/common/event/interaction"

	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/go-sql-driver/mysql"
	"github.com/zeromicro/go-zero/core/logx"
)

// 记录状态：1 有效（已点赞/已收藏）、2 已取消。
const (
	statusActive   int64 = 1
	statusCanceled int64 = 2
)

// mysqlDupEntry MySQL 唯一键冲突错误码。
const mysqlDupEntry = 1062

// Worker interaction.event 持久化消费者。
type Worker struct {
	svcCtx *svc.ServiceContext
}

// NewWorker 创建持久化消费者。
func NewWorker(svcCtx *svc.ServiceContext) *Worker {
	return &Worker{svcCtx: svcCtx}
}

// Start 订阅 interaction.event 并启动消费。
func (w *Worker) Start() error {
	if err := w.svcCtx.Consumer.Subscribe(event.TopicInteractionEvent, w.HandleMessage); err != nil {
		return err
	}
	return w.svcCtx.Consumer.Start()
}

// HandleMessage 处理一条互动事件消息。返回 error 时 RocketMQ 会重试投递。
func (w *Worker) HandleMessage(ctx context.Context, msg *primitive.MessageExt) error {
	var ev event.Event
	if err := json.Unmarshal(msg.Body, &ev); err != nil {
		// 消息体损坏无法恢复，重试也不会成功：记日志后吞掉，避免无限重试阻塞队列
		logx.WithContext(ctx).Errorf("interaction-worker: invalid event body=%s err=%v", msg.Body, err)
		return nil
	}
	return w.HandleEvent(ctx, &ev)
}

// HandleEvent 按事件类型分发落库。
func (w *Worker) HandleEvent(ctx context.Context, ev *event.Event) error {
	if ev.UserID <= 0 || ev.FeedID <= 0 {
		logx.WithContext(ctx).Errorf("interaction-worker: invalid event %+v", ev)
		return nil
	}
	switch ev.ActionType {
	case event.ActionLike:
		return w.persistLike(ctx, ev, statusActive)
	case event.ActionUnlike:
		return w.persistLike(ctx, ev, statusCanceled)
	case event.ActionCollect:
		return w.persistCollect(ctx, ev, statusActive)
	case event.ActionUncollect:
		return w.persistCollect(ctx, ev, statusCanceled)
	default:
		// 其他动作（如通知类扩展）与本消费者无关，直接确认
		return nil
	}
}

// persistLike 点赞/取消点赞落库。
func (w *Worker) persistLike(ctx context.Context, ev *event.Event, target int64) error {
	eventTime := time.UnixMilli(ev.Timestamp)
	rec, err := w.svcCtx.LikesModel.FindOneByUserIdFeedId(ctx, uint64(ev.UserID), uint64(ev.FeedID))
	switch {
	case err == nil:
		return w.flipLike(ctx, ev, rec, target, eventTime)
	case errors.Is(err, model.ErrNotFound):
		insertErr := w.insertLike(ctx, ev, target)
		if isDupEntry(insertErr) {
			// 并发插入撞唯一键：视为已存在，转为条件更新
			rec, err = w.svcCtx.LikesModel.FindOneByUserIdFeedId(ctx, uint64(ev.UserID), uint64(ev.FeedID))
			if err != nil {
				return err
			}
			return w.flipLike(ctx, ev, rec, target, eventTime)
		}
		return insertErr
	default:
		return err
	}
}

// flipLike 状态翻转（幂等 + 乱序兜底）。
func (w *Worker) flipLike(ctx context.Context, ev *event.Event, rec *model.Likes, target int64, eventTime time.Time) error {
	if rec.Status == target {
		return nil // 重复投递，幂等跳过
	}
	updated, err := w.svcCtx.LikesModel.UpdateStatusIfNewer(ctx, rec, target, eventTime)
	if err != nil {
		return err
	}
	if !updated {
		logx.WithContext(ctx).Infof(
			"interaction-worker: stale like event skipped event_id=%s user=%d feed=%d target=%d",
			ev.EventID, ev.UserID, ev.FeedID, target)
	}
	return nil
}

// insertLike 插入点赞记录；target=2 时为乱序墓碑行。
func (w *Worker) insertLike(ctx context.Context, ev *event.Event, target int64) error {
	_, err := w.svcCtx.LikesModel.Insert(ctx, &model.Likes{
		Id:     uint64(w.svcCtx.IdGen()),
		UserId: uint64(ev.UserID),
		FeedId: uint64(ev.FeedID),
		Status: target,
	})
	return err
}

// persistCollect 收藏/取消收藏落库，流程与 persistLike 同构。
func (w *Worker) persistCollect(ctx context.Context, ev *event.Event, target int64) error {
	eventTime := time.UnixMilli(ev.Timestamp)
	rec, err := w.svcCtx.CollectionsModel.FindOneByUserIdFeedId(ctx, uint64(ev.UserID), uint64(ev.FeedID))
	switch {
	case err == nil:
		return w.flipCollect(ctx, ev, rec, target, eventTime)
	case errors.Is(err, model.ErrNotFound):
		insertErr := w.insertCollect(ctx, ev, target)
		if isDupEntry(insertErr) {
			rec, err = w.svcCtx.CollectionsModel.FindOneByUserIdFeedId(ctx, uint64(ev.UserID), uint64(ev.FeedID))
			if err != nil {
				return err
			}
			return w.flipCollect(ctx, ev, rec, target, eventTime)
		}
		return insertErr
	default:
		return err
	}
}

// flipCollect 收藏状态翻转（幂等 + 乱序兜底）。
func (w *Worker) flipCollect(ctx context.Context, ev *event.Event, rec *model.Collections, target int64, eventTime time.Time) error {
	if rec.Status == target {
		return nil
	}
	updated, err := w.svcCtx.CollectionsModel.UpdateStatusIfNewer(ctx, rec, target, eventTime)
	if err != nil {
		return err
	}
	if !updated {
		logx.WithContext(ctx).Infof(
			"interaction-worker: stale collect event skipped event_id=%s user=%d feed=%d target=%d",
			ev.EventID, ev.UserID, ev.FeedID, target)
	}
	return nil
}

// insertCollect 插入收藏记录；target=2 时为乱序墓碑行。
func (w *Worker) insertCollect(ctx context.Context, ev *event.Event, target int64) error {
	_, err := w.svcCtx.CollectionsModel.Insert(ctx, &model.Collections{
		Id:     uint64(w.svcCtx.IdGen()),
		UserId: uint64(ev.UserID),
		FeedId: uint64(ev.FeedID),
		Status: target,
	})
	return err
}

// isDupEntry 判断是否 MySQL 唯一键冲突（错误码 1062）。
func isDupEntry(err error) bool {
	if err == nil {
		return false
	}
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == mysqlDupEntry
}
