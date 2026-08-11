// Package keys 统一管理 Content 服务的业务级 Redis Key 与 TTL。
//
// Key 设计见 docs/design/agent/10-data-model.md §7：
//
//	content:analysis:lock:{feed_id}  String  分析任务互斥锁，TTL 6min（FFmpegTimeoutSec×3）
//	content:profile:{feed_id}        String  画像缓存（对外字段 JSON），TTL 1h
package keys

import "fmt"

// TTL 约定（秒）。
const (
	// TTLAnalysisLock 分析互斥锁 TTL：略大于 FFmpeg 120s + ASR/OCR/多模态预期总耗时，
	// 避免锁提前释放导致同一 feed 被两个 Worker 实例重复分析。
	TTLAnalysisLock = 6 * 60
	// TTLProfileCache 画像缓存（cache-aside，对外字段）。
	TTLProfileCache = 60 * 60
)

// AnalysisLockKey 分析任务互斥锁 key（SETNX 语义，任务结束后 DEL）。
func AnalysisLockKey(feedID int64) string {
	return fmt.Sprintf("content:analysis:lock:%d", feedID)
}

// ProfileCacheKey 画像缓存 key（仅缓存对外公开字段，不含字幕/OCR 全文）。
func ProfileCacheKey(feedID int64) string {
	return fmt.Sprintf("content:profile:%d", feedID)
}
