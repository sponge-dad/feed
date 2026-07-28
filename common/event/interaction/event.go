// Package interaction 定义 Interaction 服务的 MQ 事件契约。
//
// Topic：interaction.event（见 docs/design/interaction/07-mq-event.md）。
// 生产者：Interaction RPC（Redis 更新成功后发送）。
// 消费者：
//   - interaction-persistence-consumer：异步落库 MySQL likes / collections；
//   - interaction-notification-consumer：通知服务（后续实现）。
package interaction

import (
	"time"

	"github.com/google/uuid"
)

// TopicInteractionEvent 互动事件统一 Topic。
const TopicInteractionEvent = "interaction.event"

// 互动动作类型（action_type）。
const (
	ActionLike      int32 = 1 // 点赞
	ActionUnlike    int32 = 2 // 取消点赞
	ActionCollect   int32 = 3 // 收藏
	ActionUncollect int32 = 4 // 取消收藏
)

// Event 互动事件体，字段定义见 07-mq-event.md §1.2。
type Event struct {
	EventID    string `json:"event_id"`    // 事件唯一 ID（uuid v4），用于追踪与幂等排查
	UserID     int64  `json:"user_id"`     // 行为用户
	FeedID     int64  `json:"feed_id"`     // 目标帖子
	ActionType int32  `json:"action_type"` // LIKE / UNLIKE / COLLECT / UNCOLLECT
	Timestamp  int64  `json:"timestamp"`   // 行为发生时间，毫秒级 Unix
}

// NewEvent 构造一条互动事件，时间戳取当前毫秒。
func NewEvent(userID, feedID int64, actionType int32) *Event {
	return &Event{
		EventID:    uuid.NewString(),
		UserID:     userID,
		FeedID:     feedID,
		ActionType: actionType,
		Timestamp:  time.Now().UnixMilli(),
	}
}
