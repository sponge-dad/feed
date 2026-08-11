package logic

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/sponge-dad/feed/app/content/rpc/content"
	"github.com/sponge-dad/feed/app/content/model"
)

// 画像分析状态字符串（与 feed_content_profiles.analysis_status 一致）。
const (
	statusPending   = "PENDING"
	statusDownload  = "DOWNLOADING"
	statusExtract   = "EXTRACTING"
	statusASR       = "ASR_RUNNING"
	statusOCR       = "OCR_RUNNING"
	statusVision    = "VISION_RUNNING"
	statusIndexing  = "INDEXING"
	statusCompleted = "COMPLETED"
	statusFailed    = "FAILED"
	statusDisabled  = "DISABLED"
)

// analysisStatusToPB 将 DB 状态字符串映射为 proto 枚举。
func analysisStatusToPB(s string) content.AnalysisStatus {
	switch s {
	case statusPending:
		return content.AnalysisStatus_PENDING
	case statusDownload:
		return content.AnalysisStatus_DOWNLOADING
	case statusExtract:
		return content.AnalysisStatus_EXTRACTING
	case statusASR:
		return content.AnalysisStatus_ASR_RUNNING
	case statusOCR:
		return content.AnalysisStatus_OCR_RUNNING
	case statusVision:
		return content.AnalysisStatus_VISION_RUNNING
	case statusIndexing:
		return content.AnalysisStatus_INDEXING
	case statusCompleted:
		return content.AnalysisStatus_COMPLETED
	case statusFailed:
		return content.AnalysisStatus_FAILED
	case statusDisabled:
		return content.AnalysisStatus_DISABLED
	default:
		return content.AnalysisStatus_ANALYSIS_UNKNOWN
	}
}

// isRunningStatus 判断状态是否为「分析进行中」（PENDING~INDEXING）。
func isRunningStatus(s string) bool {
	switch s {
	case statusPending, statusDownload, statusExtract, statusASR, statusOCR, statusVision, statusIndexing:
		return true
	}
	return false
}

// parseStringArray 解析 JSON 数组列（topics/objects/scenes/styles）。
func parseStringArray(col sql.NullString) []string {
	if !col.Valid || col.String == "" {
		return nil
	}
	var arr []string
	if err := json.Unmarshal([]byte(col.String), &arr); err != nil {
		return nil
	}
	return arr
}

// profileToPB 将 DB 画像转换为 proto 消息。
// full=true 时携带分级字段（transcript/ocr_text/error_message），仅作者本人或内部用户可见。
func profileToPB(data *model.FeedContentProfiles, full bool) *content.ContentProfile {
	p := &content.ContentProfile{
		FeedId:          data.FeedId,
		AuthorId:        data.AuthorId,
		Category:        data.Category,
		Summary:         data.Summary.String,
		Topics:          parseStringArray(data.Topics),
		Objects:         parseStringArray(data.Objects),
		Scenes:          parseStringArray(data.Scenes),
		Styles:          parseStringArray(data.Styles),
		Language:        data.Language,
		MediaDurationMs: data.MediaDurationMs,
		KeyFrameCount:   int32(data.KeyFrameCount),
		AnalysisStatus:  analysisStatusToPB(data.AnalysisStatus),
		Degraded:        data.Degraded == 1,
		ModelVersion:    data.ModelVersion,
	}
	if full {
		p.Transcript = data.Transcript.String
		p.OcrText = data.OcrText.String
		p.ErrorMessage = data.ErrorMessage
	}
	if data.AnalyzedAt.Valid {
		p.AnalyzedAt = data.AnalyzedAt.Time.UnixMilli()
	}
	return p
}

// publicProfileCache 对外公开字段缓存（不含字幕/OCR 全文），写入 Redis content:profile:{feed_id}。
type publicProfileCache struct {
	FeedID          int64    `json:"feed_id"`
	AuthorID        int64    `json:"author_id"`
	Category        string   `json:"category"`
	Summary         string   `json:"summary"`
	Topics          []string `json:"topics,omitempty"`
	Objects         []string `json:"objects,omitempty"`
	Scenes          []string `json:"scenes,omitempty"`
	Styles          []string `json:"styles,omitempty"`
	Language        string   `json:"language,omitempty"`
	MediaDurationMs int64    `json:"media_duration_ms"`
	KeyFrameCount   int32    `json:"key_frame_count"`
	AnalysisStatus  string   `json:"analysis_status"`
	Degraded        bool     `json:"degraded"`
	ModelVersion    string   `json:"model_version,omitempty"`
	AnalyzedAt      int64    `json:"analyzed_at,omitempty"`
}

// toCache 将 DB 画像转换为公开字段缓存。
func toCache(data *model.FeedContentProfiles) *publicProfileCache {
	c := &publicProfileCache{
		FeedID:          data.FeedId,
		AuthorID:        data.AuthorId,
		Category:        data.Category,
		Summary:         data.Summary.String,
		Topics:          parseStringArray(data.Topics),
		Objects:         parseStringArray(data.Objects),
		Scenes:          parseStringArray(data.Scenes),
		Styles:          parseStringArray(data.Styles),
		Language:        data.Language,
		MediaDurationMs: data.MediaDurationMs,
		KeyFrameCount:   int32(data.KeyFrameCount),
		AnalysisStatus:  data.AnalysisStatus,
		Degraded:        data.Degraded == 1,
		ModelVersion:    data.ModelVersion,
	}
	if data.AnalyzedAt.Valid {
		c.AnalyzedAt = data.AnalyzedAt.Time.UnixMilli()
	}
	return c
}

// fromCache 将公开字段缓存转换为 proto 消息。
func fromCache(c *publicProfileCache) *content.ContentProfile {
	return &content.ContentProfile{
		FeedId:          c.FeedID,
		AuthorId:        c.AuthorID,
		Category:        c.Category,
		Summary:         c.Summary,
		Topics:          c.Topics,
		Objects:         c.Objects,
		Scenes:          c.Scenes,
		Styles:          c.Styles,
		Language:        c.Language,
		MediaDurationMs: c.MediaDurationMs,
		KeyFrameCount:   c.KeyFrameCount,
		AnalysisStatus:  analysisStatusToPB(c.AnalysisStatus),
		Degraded:        c.Degraded,
		ModelVersion:    c.ModelVersion,
		AnalyzedAt:      c.AnalyzedAt,
	}
}

// timeNow 便于测试注入。
var timeNow = time.Now
