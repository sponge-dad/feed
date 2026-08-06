// Package trace 实现「请求级 Trace」的数据结构与收集器（见 02-request-trace.md §6）。
//
// 一次 Timeline 请求（关注/推荐/同城）会在上游 Gateway 生成 request_id 并透传，
// 本包在三个 timeline logic 中埋点，记录「读了多少数据源、各多少条、合并后剩多少、
// 各 feed 命中了哪个来源」，最终由 Writer 写入 Redis Hash（feed:trace:{request_id}），
// 供 GetFeedSource / GetFeedRequestTrace 两个内部 RPC 查询排障。
package trace

import (
	"sync"
	"time"

	feed "github.com/sponge-dad/feed/app/feed/rpc/feed"
)

// 直接复用 proto 生成的消息类型，保证「写入 Redis 的 JSON」与「查询 RPC 返回类型」一致，
// 避免两套结构之间的转换。
// 注意：proto 消息内部含锁，必须始终以指针方式传递（见 go vet copylocks）。
type FeedRequestTrace = feed.FeedRequestTrace
type SourceStat = feed.SourceStat
type TraceItem = feed.TraceItem

// Builder 逐步收集一次 Timeline 请求的 Trace 信息，内部加锁，可被并发路径安全调用
// （关注流中 rebuildInbox 会另起 goroutine 调用 RecordSource）。
type Builder struct {
	mu      sync.Mutex
	started time.Time
	trace   *FeedRequestTrace
}

// NewBuilder 构造 Builder。pageSize 仅用于预分配 items 容量。
func NewBuilder(requestID string, userID int64, tab, cursor string, pageSize int32) *Builder {
	return &Builder{
		started: time.Now(),
		trace: &FeedRequestTrace{
			RequestId: requestID,
			UserId:    userID,
			Tab:       tab,
			Cursor:    cursor,
			PageSize:  pageSize,
			Sources:   make([]*SourceStat, 0, 4),
			Items:     make([]*TraceItem, 0, pageSize),
		},
	}
}

// RecordSource 记录某数据源的读取条数与耗时（可并发调用）。
func (b *Builder) RecordSource(source string, count int32, costMs int64) *Builder {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.trace.Sources = append(b.trace.Sources, &SourceStat{Source: source, Count: count, CostMs: costMs})
	return b
}

// AddItem 记录一条返回 feed 的来源与位置（可并发调用）。
func (b *Builder) AddItem(feedID int64, source string, position int32, score int64) *Builder {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.trace.Items = append(b.trace.Items, &TraceItem{FeedId: feedID, Source: source, Position: position, Score: score})
	return b
}

// SetMergedCount 设置去重合并后候选数。
func (b *Builder) SetMergedCount(n int32) *Builder {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.trace.MergedCount = n
	return b
}

// SetReturnedCount 设置实际返回条数。
func (b *Builder) SetReturnedCount(n int32) *Builder {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.trace.ReturnedCount = n
	return b
}

// SetFilteredCount 设置因详情缺失被丢弃的条数。
func (b *Builder) SetFilteredCount(n int32) *Builder {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.trace.FilteredCount = n
	return b
}

// Build 返回 Trace 指针快照并计算总耗时。调用后 Builder 不应再被修改（否则会改动快照）。
func (b *Builder) Build() *FeedRequestTrace {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.trace.CostMs = time.Since(b.started).Milliseconds()
	return b.trace
}
