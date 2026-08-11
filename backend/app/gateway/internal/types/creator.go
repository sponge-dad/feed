// Package types 定义 Gateway 对外请求/响应结构（创作者数据 US5）。
package types

// GetCreatorMetricsReq 创作者查看自己作品的指标。
type GetCreatorMetricsReq struct {
	FeedId int64  `path:"feedId"`
	Window string `form:"window,optional"` // 1h / 24h / 7d / all，默认 24h
}

// FeedMetricsRaw 原子指标。
type FeedMetricsRaw struct {
	Expose          int64 `json:"expose"`
	Play            int64 `json:"play"`
	EffectivePlay   int64 `json:"effective_play"`
	Finish          int64 `json:"finish"`
	Skip            int64 `json:"skip"`
	WatchDurationMs int64 `json:"watch_duration_ms"`
	Like            int64 `json:"like"`
	Collect         int64 `json:"collect"`
	Comment         int64 `json:"comment"`
	Share           int64 `json:"share"`
}

// FeedMetricsRate 派生率（分母为 0 时为 null）。
type FeedMetricsRate struct {
	PlayRate          *float64 `json:"play_rate"`
	EffectivePlayRate *float64 `json:"effective_play_rate"`
	FinishRate        *float64 `json:"finish_rate"`
	SkipRate          *float64 `json:"skip_rate"`
	AvgWatchMs        *float64 `json:"avg_watch_ms"`
	LikeRate          *float64 `json:"like_rate"`
	CollectRate       *float64 `json:"collect_rate"`
	CommentRate       *float64 `json:"comment_rate"`
	ShareRate         *float64 `json:"share_rate"`
}

// FeedMetrics 作品指标（创作者视角）。
type FeedMetrics struct {
	FeedId        int64            `json:"feed_id"`
	Window        string           `json:"window"`
	PublishedAt   int64            `json:"published_at"`
	Raw           *FeedMetricsRaw  `json:"raw"`
	Rate          *FeedMetricsRate `json:"rate"`
	DataComplete  bool             `json:"data_complete"`
}

// GetCreatorMetricsResp 创作者指标响应。
type GetCreatorMetricsResp struct {
	Metrics *FeedMetrics `json:"metrics"`
}
