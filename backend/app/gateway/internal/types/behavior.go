package types

// 行为埋点批量上报（见 docs/design/agent/03-behavior-event.md §2）。

// ReportBehaviorsReq 批量上报请求，单批 1~50 条。
type ReportBehaviorsReq struct {
	Events []BehaviorItem `json:"events"`
}

// BehaviorItem 单条行为事件。
//
// 刻意不接受客户端的 user_id / author_id / event_id：
//   - user_id     取自 JWT，客户端传什么都不作数；
//   - author_id   服务端查 Feed 校正；
//   - event_id    服务端生成 uuid v4。若以客户端值做幂等键，
//     攻击者可预先用同一 ID 占位，把他人真实事件"吞掉"。
//     client_event_id 仅用于日志排查与本次响应的结果对账。
type BehaviorItem struct {
	ClientEventId string `json:"client_event_id,optional"`
	// RequestId 该行为所属 Timeline 请求的 ID（由 Feed 列表响应下发）。
	// 用于串起「哪一次刷流的哪一条内容」，也是 EXPOSE 去重的维度。
	RequestId string `json:"request_id,optional"`
	FeedId    int64  `json:"feed_id"`
	// ActionType EXPOSE / PLAY / EFFECTIVE_PLAY / FINISH / SKIP / SHARE。
	// 其中 EFFECTIVE_PLAY / FINISH / SKIP 由服务端按阈值重新判定，客户端结论不作准。
	ActionType      string `json:"action_type"`
	Position        int32  `json:"position,optional"`
	WatchDurationMs int64  `json:"watch_duration_ms,optional"`
	MediaDurationMs int64  `json:"media_duration_ms,optional"`
	Timestamp       int64  `json:"timestamp"`
}

// ReportBehaviorsResp 上报结果。部分拒绝不影响整体 200。
type ReportBehaviorsResp struct {
	Accepted int             `json:"accepted"`
	Rejected []RejectedEvent `json:"rejected"`
}

// RejectedEvent 被拒事件明细。以 index 定位，避免依赖服务端生成的 event_id 做对账。
type RejectedEvent struct {
	Index         int    `json:"index"`
	ClientEventId string `json:"client_event_id"`
	Reason        string `json:"reason"`
}
