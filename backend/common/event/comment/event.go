// Package comment 定义 Comment 服务对外广播的 MQ 事件模型（comment.event）。
//
// 事件为「通知型」而非「落库型」：评论内容已在 CreateComment 同步写 MySQL，
// 事件仅用于驱动通知服务、Feed 计数异步同步与统计（见 docs/design/comment/07-mq-event.md）。
// 事件体不携带评论原文，仅携带 content_len，降低带宽与隐私风险。
package comment

import "github.com/google/uuid"

/*
{
  "event_id": "uuid-v4",
  "action_type": "CREATE",
  "comment_id": 123456789,
  "feed_id": 987654321,
  "user_id": 10001,
  "reply_user_id": 10002,
  "parent_id": 123456700,
  "root_id": 123456600,
  "content_len": 32,
  "timestamp": 1752998400000
}
*/

// TopicCommentEvent 评论事件 Topic，CREATE / DELETE 共用，靠 action_type 区分。
// 注意：RocketMQ topic 仅允许 ^[%|a-zA-Z0-9_-]+$，不能含 "."，故用连字符。
const TopicCommentEvent = "comment-event"

// 事件动作类型。
const (
	ActionCreate = "CREATE" // 发表评论
	ActionDelete = "DELETE" // 删除评论
)

// Event 评论事件体，CREATE 与 DELETE 共用同一结构。
type Event struct {
	EventID     string `json:"event_id"`      // 全局唯一事件 ID，消费方以此幂等去重
	ActionType  string `json:"action_type"`   // CREATE / DELETE
	CommentID   int64  `json:"comment_id"`    // 评论 ID（Snowflake）
	FeedID      int64  `json:"feed_id"`       // 所属帖子 ID
	UserID      int64  `json:"user_id"`       // 评论作者 ID
	ReplyUserID int64  `json:"reply_user_id"` // 被回复者 ID（CREATE 有效，一级评论为 0）
	ParentID    int64  `json:"parent_id"`     // 直接父评论 ID
	RootID      int64  `json:"root_id"`       // 根评论 ID（一级为 0）
	ContentLen  int32  `json:"content_len"`   // 评论内容长度（不发原文）
	Timestamp   int64  `json:"timestamp"`     // 毫秒级事件时间
	RequestID   string `json:"request_id"`
}

// NewEventCommentCreated 构造发表评论事件。
func NewEventCommentCreated(commentID, feedID, userID, replyUserID, parentID, rootID int64, contentLen int32, timestampMs int64, requestID string) *Event {
	return &Event{
		EventID:     uuid.NewString(),
		ActionType:  ActionCreate,
		CommentID:   commentID,
		FeedID:      feedID,
		UserID:      userID,
		ReplyUserID: replyUserID,
		ParentID:    parentID,
		RootID:      rootID,
		ContentLen:  contentLen,
		Timestamp:   timestampMs,
		RequestID:   requestID,
	}
}

// NewEventCommentDeleted 构造删除评论事件。
func NewEventCommentDeleted(commentID, feedID, userID, parentID, rootID int64, timestampMs int64, requestID string) *Event {
	return &Event{
		EventID:    uuid.NewString(),
		ActionType: ActionDelete,
		CommentID:  commentID,
		FeedID:     feedID,
		UserID:     userID,
		ParentID:   parentID,
		RootID:     rootID,
		Timestamp:  timestampMs,
		RequestID:  requestID,
	}
}
