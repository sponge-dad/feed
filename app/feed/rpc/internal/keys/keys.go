// Package keys 定义 Feed 服务内部所有 Redis / 本地缓存 key 的统一命名规则。
// 由于 worker（写端）与 logic（读端）都会用到同一批 key，集中在此可避免命名漂移。
package keys

import "fmt"

// Inbox 返回「推模式」收件箱 key：inbox:{userID}。
// 存储普通好友（非大V）的帖子 ID，score 为发布时间的秒级时间戳。
func Inbox(userID int64) string {
	return fmt.Sprintf("inbox:%d", userID)
}

// Outbox 返回「拉模式」发件箱 key：outbox:{userID}。
// 存储大V的全部帖子 ID，score 为发布时间的秒级时间戳。
func Outbox(userID int64) string {
	return fmt.Sprintf("outbox:%d", userID)
}

// City 返回同城池 key：feed:city:{cityCode}。
// 存储该城市全部帖子 ID，score 为发布时间的秒级时间戳。
func City(cityCode string) string {
	return "feed:city:" + cityCode
}

// Recommend 返回推荐池 key：feed:recommend。
// 存储进入推荐池的帖子 ID，score 为发布时间的秒级时间戳。
func Recommend() string {
	return "feed:recommend"
}

// FeedDetail 返回帖子详情业务缓存 key：feed:{feedID}。
// 以 Hash 结构存储帖子各字段，TTL 30 天（见 docs/design/feed/06-cache-strategy.md）。
func FeedDetail(feedID int64) string {
	return fmt.Sprintf("feed:%d", feedID)
}

// Timeline 返回时间线热点缓存 key：timeline:{tab}:{page}（或带 userID）。
// 该层为读优化缓存，TTL 较短（推荐 5min / 关注 60s / 同城 60s）。
func Timeline(tab string, userID, page int64) string {
	if userID > 0 {
		return fmt.Sprintf("timeline:%s:%d:%d", tab, userID, page)
	}
	return fmt.Sprintf("timeline:%s:%d", tab, page)
}
