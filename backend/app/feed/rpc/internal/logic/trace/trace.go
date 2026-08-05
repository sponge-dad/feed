package trace

// FeedRequestTrace 一次 Timeline 请求的读取路径记录（内部诊断用）
type FeedRequestTrace struct {
	RequestID     string       `json:"request_id"`
	UserID        int64        `json:"user_id"`
	Tab           string       `json:"tab"` // follow / recommend / city
	Cursor        string       `json:"cursor"`
	PageSize      int32        `json:"page_size"`
	Sources       []SourceStat `json:"sources"`
	MergedCount   int32        `json:"merged_count"`
	ReturnedCount int32        `json:"returned_count"`
	FilteredCount int32        `json:"filtered_count"`
	CostMs        int64        `json:"cost_ms"`
}

type SourceStat struct {
	Source string `json:"source"` // FOLLOW_INBOX / VIP_OUTBOX / ...
	Count  int32  `json:"count"`
	CostMs int64  `json:"cost_ms"`
}
