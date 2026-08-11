// Package types 定义 Gateway 对外请求/响应结构（内容画像相关）。
package types

// GetContentProfileReq 查询内容画像（feedId 来自路径参数）。
type GetContentProfileReq struct {
	FeedId int64 `path:"feedId"`
}

// ContentProfile 内容画像（分级返回：非作者/非内部用户不返回 transcript/ocr_text/error_message）。
type ContentProfile struct {
	FeedId          int64    `json:"feed_id"`
	AuthorId        int64    `json:"author_id"`
	Category        string   `json:"category"`
	Summary         string   `json:"summary"`
	Topics          []string `json:"topics,omitempty"`
	Objects         []string `json:"objects,omitempty"`
	Scenes          []string `json:"scenes,omitempty"`
	Styles          []string `json:"styles,omitempty"`
	Transcript      string   `json:"transcript,omitempty"`
	OcrText         string   `json:"ocr_text,omitempty"`
	Language        string   `json:"language,omitempty"`
	MediaDurationMs int64    `json:"media_duration_ms"`
	KeyFrameCount   int32    `json:"key_frame_count"`
	AnalysisStatus  int32    `json:"analysis_status"`
	Degraded        bool     `json:"degraded"`
	ModelVersion    string   `json:"model_version,omitempty"`
	ErrorMessage    string   `json:"error_message,omitempty"`
	AnalyzedAt      int64    `json:"analyzed_at"`
}

// GetContentProfileResp 内容画像响应。
type GetContentProfileResp struct {
	Profile *ContentProfile `json:"profile"`
}

// SubmitProfileFeedbackReq 创作者纠错反馈（只记录，不改画像）。
type SubmitProfileFeedbackReq struct {
	FeedId  int64  `path:"feedId"`
	Field   string `json:"field"`
	Comment string `json:"comment"`
}

// SubmitProfileFeedbackResp 反馈响应。
type SubmitProfileFeedbackResp struct {
	OK bool `json:"ok"`
}
