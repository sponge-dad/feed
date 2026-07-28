// Package keys 定义 Comment 服务内部所有 Redis key 的统一命名规则与 TTL。
// logic（读写端）与 worker（like_count 同步端）共用同一批 key，集中在此避免命名漂移。
// key 设计见 docs/design/comment/01-data-model.md 与 06-cache.md。
package keys

import (
	"fmt"
	"time"
)

// CommentCountTTL 是 comment_count:{feed_id} 计数缓存的过期时间（1 小时）。
const CommentCountTTL = time.Hour

// CommentHotTTL 是 comment_hot:{feed_id} 热门评论 ZSet 的过期时间（5 分钟）。
const CommentHotTTL = 5 * time.Minute

// CommentCount 返回帖子评论总数缓存 key：comment_count:{feedID}（String，TTL 1h）。
// 值为该帖子下所有 status=1 评论数（一级 + 子回复）。
func CommentCount(feedID int64) string {
	return fmt.Sprintf("comment_count:%d", feedID)
}

// CommentHot 返回热门评论 ZSet key：comment_hot:{feedID}（TTL 5min）。
// member=comment_id，score=like_count，仅一级评论进入。
func CommentHot(feedID int64) string {
	return fmt.Sprintf("comment_hot:%d", feedID)
}
