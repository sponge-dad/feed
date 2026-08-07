package types

// 行为埋点批量上报
type ReportBehaviorsReq struct {
	Events []BehaviorItem `json:"events"`
}

type BehaviorItem struct {
	EventId   string            `json:"event_id"`
	Action    string            `json:"action"`
	FeedId    int64             `json:"feed_id"`
	TargetId  int64             `json:"target_id,optional"`
	Timestamp int64             `json:"timestamp"`
	Scene     string            `json:"scene,optional"`
	Page      string            `json:"page,optional"`
	Pos       int32             `json:"pos,optional"`
	Duration  int64             `json:"duration,optional"`
	Ext       map[string]string `json:"ext,optional"`
}

type ReportBehaviorsResp struct {
	Accepted []string          `json:"accepted"`
	Rejected map[string]string `json:"rejected"`
}
