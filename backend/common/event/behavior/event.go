// Package behavior 定义 Feed 行为埋点事件契约（feed-behavior-event）。
//
// 设计见 docs/design/agent/03-behavior-event.md。
// 事件由 Gateway 在用户产生行为时批量上报（SendSync 到 RocketMQ），
// 由 Interaction RPC 的 BehaviorWorker 消费，做幂等、服务端重判、指标累加与抽样落库。
//
// 与 interaction-event / comment-event 的关系：不合并、不改造。点赞/收藏/评论仍走
// 各自 Topic；BehaviorWorker 额外订阅它们仅用于把 like/collect/comment 计入指标，
// 不参与落库。
package behavior

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// TopicFeedBehaviorEvent Feed 行为事件 Topic。
// 命名注意：RocketMQ topic 仅允许 ^[%|a-zA-Z0-9_-]+$，不能含 "."，故用连字符。
const TopicFeedBehaviorEvent = "feed-behavior-event"

// 行为类型（action_type）。
const (
	ActionExpose        = "EXPOSE"         // 内容真正进入可视区域（可见面积≥50% 且持续≥500ms）
	ActionPlay          = "PLAY"           // 首帧渲染成功（起播）
	ActionEffectivePlay = "EFFECTIVE_PLAY" // 有效播放（服务端按阈值重判）
	ActionFinish        = "FINISH"         // 完播（服务端按阈值重判）
	ActionSkip          = "SKIP"           // 快速划走（服务端按阈值重判）
	ActionShare         = "SHARE"          // 分享成功
)

// 字段边界。
const (
	MaxBatchSize       = 50               // 单批事件数上限
	MaxPosition        = 1000             // position 上限
	MaxWatchDurationMs = 24 * 3600 * 1000 // watch_duration_ms 上限（24h）
	MaxClockSkewMs     = 3600 * 1000      // timestamp 与服务端偏差上限（1h）
)

// 幂等 / EXPOSE 去重键 TTL（秒）。
const (
	IdemExpireSec       = 86400 // behavior_event:{event_id}
	ExposeIdemExpireSec = 86400 // behavior:expose:{request_id}:{feed_id}
)

// validActions 合法行为集合，用于 O(1) 校验。
var validActions = map[string]struct{}{
	ActionExpose:        {},
	ActionPlay:          {},
	ActionEffectivePlay: {},
	ActionFinish:        {},
	ActionSkip:          {},
	ActionShare:         {},
}

// IsValidAction 判断 action_type 是否在枚举内。
func IsValidAction(action string) bool {
	_, ok := validActions[action]
	return ok
}

// 错误定义。
var (
	ErrEventInvalid  = errors.New("behavior event invalid")
	ErrActionUnknown = errors.New("unknown action_type")
	ErrEventIDEmpty  = errors.New("event_id empty")
	ErrUserIDZero    = errors.New("user_id must be > 0")
	ErrFeedIDZero    = errors.New("feed_id must be > 0")
	ErrPositionRange = errors.New("position out of range")
	ErrWatchRange    = errors.New("watch_duration_ms out of range")
)

// FeedBehaviorEvent 单条 Feed 行为事件。字段语义见 03-behavior-event.md §2。
type FeedBehaviorEvent struct {
	EventID         string `json:"event_id"`          // 服务端生成 uuid v4，消费端幂等依据
	RequestID       string `json:"request_id"`        // 来源 Timeline 请求
	UserID          int64  `json:"user_id"`           // 由 JWT 注入，不信任客户端
	FeedID          int64  `json:"feed_id"`           // 目标帖子
	AuthorID        int64  `json:"author_id"`         // 服务端从 Feed 详情校正
	ActionType      string `json:"action_type"`       // EXPOSE/PLAY/EFFECTIVE_PLAY/FINISH/SKIP/SHARE
	Position        int32  `json:"position"`          // 在本次 Feed 结果中的位置，从 0 开始
	WatchDurationMs int64  `json:"watch_duration_ms"` // 观看时长（毫秒）
	MediaDurationMs int64  `json:"media_duration_ms"` // 媒体总时长（毫秒），服务端校正
	Timestamp       int64  `json:"timestamp"`         // 客户端行为时间（毫秒）
	ServerTime      int64  `json:"server_time"`       // 服务端接收时间（毫秒），用于纠偏
	// ClientEventID 仅作日志排查，不作为幂等键（防伪造吞事件）。
	ClientEventID string `json:"client_event_id,omitempty"`
	// Abnormal 数据质量标记：如无 EXPOSE 直接 PLAY 等异常序列。
	Abnormal bool `json:"abnormal,omitempty"`
}

// NewEvent 构造一条行为事件并由服务端生成 event_id。
//
// user_id / author_id / media_duration_ms 由服务端校正后传入，绝不信任客户端。
func NewEvent(requestID string, userID, feedID, authorID int64, actionType string, position int32, watchMs, mediaMs, tsMs, serverMs int64, clientEventID string) *FeedBehaviorEvent {
	return &FeedBehaviorEvent{
		EventID:         uuid.NewString(),
		RequestID:       requestID,
		UserID:          userID,
		FeedID:          feedID,
		AuthorID:        authorID,
		ActionType:      actionType,
		Position:        position,
		WatchDurationMs: watchMs,
		MediaDurationMs: mediaMs,
		Timestamp:       tsMs,
		ServerTime:      serverMs,
		ClientEventID:   clientEventID,
	}
}

// Validate 校验事件字段合法性（结构性校验；时钟偏差由 Gateway 结合服务端时间另行校验）。
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
	if !IsValidAction(e.ActionType) {
		return fmt.Errorf("%w: %s", ErrActionUnknown, e.ActionType)
	}
	if e.Position < 0 || e.Position > MaxPosition {
		return ErrPositionRange
	}
	if e.WatchDurationMs < 0 || e.WatchDurationMs > MaxWatchDurationMs {
		return ErrWatchRange
	}
	return nil
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

// IdemKey 单事件绝对幂等键：behavior_event:{event_id}。
func (e *FeedBehaviorEvent) IdemKey() string {
	return "behavior_event:" + e.EventID
}

// ExposeDedupKey EXPOSE 去重键：behavior:expose:{request_id}:{feed_id}。
// 保证同一请求返回中的同一 feed 只计一次曝光，避免客户端重试造成曝光虚高。
//
// RequestID 为空时（来自非 Timeline 请求的曝光），退化为按事件唯一——每个事件
// 的键互不相同，天然不去重。若沿用 "{feed_id}" 维度，不同用户/不同请求的曝光
// 会互相去重，曝光指标被系统性低估。
//
// 正常路径下消费端对空 RequestID 的 EXPOSE 跳过曝光去重（见 behaviorWorker.go），
// 不会调用本方法；此处兜底防止误用退化键。
func (e *FeedBehaviorEvent) ExposeDedupKey() string {
	if e.RequestID == "" {
		return "behavior:expose:evt:" + e.EventID
	}
	return fmt.Sprintf("behavior:expose:%s:%d", e.RequestID, e.FeedID)
}
