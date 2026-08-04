// Package keys 统一管理 Interaction 服务的业务级 Redis Key 与 TTL。
//
// Key 设计见 docs/design/interaction/01-data-model.md 与 06-cache.md：
//
//	like:feed:{feed_id}      Set    帖子点赞用户集合，TTL 7 天
//	collect:feed:{feed_id}   Set    帖子收藏用户集合，TTL 7 天
//	user:likes:{user_id}     ZSet   用户点赞列表（score=点赞时间秒），TTL 30 天
//	user:collects:{user_id}  ZSet   用户收藏列表，TTL 30 天
//	feed:stats:{feed_id}     Hash   like_count / collect_count，TTL 1 小时
package keys

import "fmt"

// TTL 约定（秒）。写路径与重建路径统一刷新，保证 key 可通过过期自愈。
const (
	TTLFeedSet   = 604800  // like:feed / collect:feed，7 天
	TTLUserZSet  = 2592000 // user:likes / user:collects，30 天
	TTLFeedStats = 3600    // feed:stats，1 小时
)

// feed:stats Hash 字段名。
const (
	FieldLikeCount    = "like_count"
	FieldCollectCount = "collect_count"
)

// SetSentinel like:feed / collect:feed Set 的「已加载」哨兵成员。
//
// 背景：Redis 会自动删除空集合 key。若某帖子最后一个点赞被取消，
// Set 变空即被删除，下一次状态查询会把「已加载但为空」误判为「缓存未加载」，
// 回源 MySQL；此时若 unlike 事件尚未被 MQ 消费，MySQL 仍是旧状态，
// 查询会错误返回已点赞（详见 tests 中 Baseline I-EMPTY-*）。
//
// 约定：
//  1. 重建与写路径都会确保哨兵存在，使「已加载的空集合」key 仍然存在；
//  2. 哨兵为非数字字符串，永远不会与真实用户 ID（十进制数字串）冲突，
//     SISMEMBER <userID> 不受影响；
//  3. 计数一律以 feed:stats Hash 或 MySQL COUNT 为准，禁止用 SCARD，
//     哨兵不会计入点赞/收藏数。
const SetSentinel = "__loaded__"

// LikeFeed 帖子点赞用户集合 key。
func LikeFeed(feedID int64) string {
	return fmt.Sprintf("like:feed:%d", feedID)
}

// CollectFeed 帖子收藏用户集合 key。
func CollectFeed(feedID int64) string {
	return fmt.Sprintf("collect:feed:%d", feedID)
}

// UserLikes 用户点赞列表 ZSet key。
func UserLikes(userID int64) string {
	return fmt.Sprintf("user:likes:%d", userID)
}

// UserCollects 用户收藏列表 ZSet key。
func UserCollects(userID int64) string {
	return fmt.Sprintf("user:collects:%d", userID)
}

// FeedStats 帖子互动计数 Hash key。
func FeedStats(feedID int64) string {
	return fmt.Sprintf("feed:stats:%d", feedID)
}
