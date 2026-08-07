// Package behavior 定义行为埋点事件契约（feed-behavior-event）。
//
// 设计见 docs/design/agent/03-behavior-event.md。
// 事件由 Gateway 在用户产生行为时上报（SendSync 到 RocketMQ），
// 由 Interaction RPC 的 BehaviorWorker 消费，做幂等、服务端重判、指标累加与抽样落库。
package behavior

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// Topic 命名注意：RocketMQ topic 仅允许 ^[%|a-zA-Z0-9_-]+$，不能含 "."，故用连字符。
const TopicFeedBehaviorEvent = "feed-behavior-event"

// 行为类型（proto action）
const (
	ProtoExpose  = "expose"
	ProtoLike    = "like"
	ProtoCollect = "collect"
	ProtoComment = "comment"
	ProtoShare   = "share"
)

// 场景（scene）
const (
	SceneFeedDetail    = "feed_detail"
	SceneFeedList      = "feed_list"
	SceneCommentDetail = "comment_detail"
	SceneUserProfile   = "user_profile"
	SceneSearch        = "search"
)

// 页面（page）
const (
	PageFeedDetail    = "feed_detail"
	PageFeedList      = "feed_list"
	PageCommentDetail = "comment_detail"
	PageUserProfile   = "user_profile"
	PageSearch        = "search"
)

// 幂等 / EXPOSE 去重键 TTL（秒）
const (
	IdemExpireSec       = 86400
	ExposeIdemExpireSec = 86400
)

// 错误定义
var (
	ErrEventInvalid = errors.New("behavior event invalid")
	ErrActionUnknown = errors.New("unknown action")
	ErrEventIDEmpty = errors.New("event_id empty")
	ErrUserIDZero   = errors.New("user_id must be > 0")
	ErrFeedIDZero   = errors.New("feed_id must be > 0")
	ErrTargetIDNeg  = errors.New("target_id must be >= 0")
)

// FeedBehaviorEvent 行为埋点事件。
//
// 字段语义见 03-behavior-event.md §3。
type FeedBehaviorEvent struct {
	EventID   string            `json:"event_id"`  // 客户端幂等键（uuid），用于绝对幂等
	EventType string            `json:"event_type"` // 固定为 TopicFeedBehaviorEvent
	UserID    int64             `json:"user_id"`
	FeedID    int64             `json:"feed_id"`
	Action    string            `json:"action"` // expose/like/collect/comment/share
	TargetID  int64             `json:"target_id"` // comment/share 所指目标（评论 ID / 被分享源 feed_id）
	Timestamp int64             `json:"timestamp"` // 客户端行为发生时间（ms）
	Scene     string            `json:"scene"` // 来源场景
	Page      string            `json:"page"` // 来源页面
	Pos       int32             `json:"pos"` // 在列表中的位置
	Duration  int64             `json:"duration"` // EXPOSE 停留时长（ms）
	ClientIP  string            `json:"client_ip"`
	UserAgent string            `json:"user_agent"`
	ReqID     string            `json:"req_id"` // 请求链路 ID
	Ext       map[string]string `json:"ext,omitempty"`
}

// NewEvent 构造一个行为事件并自动生成 event_id。
func NewEvent(userID, feedID int64, action string, targetID, ts int64, scene, page string, pos int32, duration int64, clientIP, ua, reqID string, ext map[string]string) *FeedBehaviorEvent {
	return &FeedBehaviorEvent{
		EventID:   uuid.NewString(),
		EventType: TopicFeedBehaviorEvent,
		UserID:    userID,
		FeedID:    feedID,
		Action:    action,
		TargetID:  targetID,
		Timestamp: ts,
		Scene:     scene,
		Page:      page,
		Pos:       pos,
		Duration:  duration,
		ClientIP:  clientIP,
		UserAgent: ua,
		ReqID:     reqID,
		Ext:       ext,
	}
}

// Validate 校验事件字段合法性。
func (e *FeedBehaviorEvent) Validate() error {
	if e.EventID == "" {
		return ErrEventIDEmpty
	}
	if e.UserID <= 0 {
		return ErrUserIDZero
	}
	if e.FeedID <= 0 {
		return ErrFeedIDZero
	}
	if e.TargetID < 0 {
		return ErrTargetIDNeg
	}
	switch e.Action {
	case ProtoExpose, ProtoLike, ProtoCollect, ProtoComment, ProtoShare:
		return nil
	default:
		return fmt.Errorf("%w: %s", ErrActionUnknown, e.Action)
	}
}

// ToJSON 序列化事件体（用于 MQ 投递）。
func (e *FeedBehaviorEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// FromJSON 反序列化并校验事件体（MQ 消费侧）。
func FromJSON(b []byte) (*FeedBehaviorEvent, error) {
	var e FeedBehaviorEvent
	if err := json.Unmarshal(b, &e); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEventInvalid, err)
	}
	if err := e.Validate(); err != nil {
		return nil, err
	}
	return &e, nil
}

// HashKey 幂等 / 去重键前缀：user:feed:action:target。
// 用于「同一用户对同一帖子同一动作同一目标仅一次有效」的判定。
func (e *FeedBehaviorEvent) HashKey() string {
	return fmt.Sprintf("%d:%d:%s:%d", e.UserID, e.FeedID, e.Action, e.TargetID)
}

// EventKey 单事件绝对幂等键（客户端 event_id）。
func (e *FeedBehaviorEvent) EventKey() string {
	return e.EventID
}
