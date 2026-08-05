// Package logic 实现 Feed 服务的所有 gRPC 业务逻辑。
// feed_follow.go 提供关注流（推拉结合）的游标编解码与候选候选排序辅助逻辑。
package logic

import (
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/sponge-dad/feed/app/feed/rpc/feed"
)

// followOutboxPullSize 每个关注大V单次拉取的最近帖子数（outbox 取最新 N 条）。
const followOutboxPullSize = 30

// followMaxBigV 单次关注流最多拉取的大V发件箱数量，避免过度放大读放大。
const followMaxBigV = 200

// followInboxReadCap 单次关注流读取收件箱的最大条数（与 worker inboxCap 对齐）。
const followInboxReadCap = 1000

// 信息流来源标记常量（对应 feed.proto 的 FeedSource 枚举）。
// 用于在关注流中区分推模式（inbox）与拉模式（大V outbox），以及兜底重建来源，便于排障。
const (
	feedSourceFollowInbox   = feed.FeedSource_FEED_SOURCE_FOLLOW_INBOX
	feedSourceVipOutbox     = feed.FeedSource_FEED_SOURCE_VIP_OUTBOX
	feedSourceInboxRebuild  = feed.FeedSource_FEED_SOURCE_INBOX_REBUILD
	feedSourceCityPool      = feed.FeedSource_FEED_SOURCE_CITY_POOL
	feedSourceRecommendPool = feed.FeedSource_FEED_SOURCE_RECOMMEND_POOL
)

// feedScore 是候选流中的一条帖子：feedID、其在 ZSet 中的秒级 score，以及来源标记 source。
type feedScore struct {
	feedID int64
	score  int64
	source feed.FeedSource
}

// feedSourceRank 返回来源优先级（数值越小优先级越高）。
// 规则：FOLLOW_INBOX（推模式已确认推送）最高，其次 INBOX_REBUILD（兜底重建），
// 其余（如 VIP_OUTBOX）最低。同一 feed 被多路命中时取高优先级来源，保证标记稳定。
func feedSourceRank(s feed.FeedSource) int {
	switch s {
	case feedSourceFollowInbox:
		return 0
	case feedSourceInboxRebuild:
		return 1
	default:
		return 2
	}
}

// mergeFeedScore 多路命中时合并候选：来源按优先级收敛（FOLLOW_INBOX 优先），
// score 取较大值（同源时分数一致，这里做防御性处理）。
// 仅当新来源优先级更高时才覆盖 source，避免高优先级来源被低优先级覆盖。
func mergeFeedScore(m map[int64]*feedScore, id, score int64, src feed.FeedSource) {
	if cur, ok := m[id]; ok {
		if feedSourceRank(src) < feedSourceRank(cur.source) {
			cur.source = src
		}
		if score > cur.score {
			cur.score = score
		}
		return
	}
	m[id] = &feedScore{feedID: id, score: score, source: src}
}

// parseFollowCursor 解析 base64(score_sec ":" feed_id) 形式的游标。
// 空游标表示首页；解析失败返回 ok=false，调用方应拒绝非法游标。
func parseFollowCursor(cursor string) (scoreSec, feedID int64, ok bool) {
	if cursor == "" {
		return 0, 0, true
	}
	raw, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return 0, 0, false
	}
	parts := strings.SplitN(string(raw), ":", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	s, err1 := strconv.ParseInt(parts[0], 10, 64)
	id, err2 := strconv.ParseInt(parts[1], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return s, id, true
}

// encodeFollowCursor 将当前游标位置编码为 base64(score_sec ":" feed_id)。
func encodeFollowCursor(scoreSec, feedID int64) string {
	return base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%d:%d", scoreSec, feedID)))
}

// beforeFollowCursor 判断候选 (s,id) 是否严格位于游标 (cs,cid) 之前。
// 首页（cs==0）恒为真；否则按 (score 降序, id 降序) 字典序比较。
func beforeFollowCursor(s, id, cs, cid int64) bool {
	if cs == 0 {
		return true
	}
	if s != cs {
		return s < cs
	}
	return id < cid
}

// sortFeedScores 按 (score 倒序, id 倒序) 稳定排序候选流。
func sortFeedScores(items []feedScore) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].score != items[j].score {
			return items[i].score > items[j].score
		}
		return items[i].feedID > items[j].feedID
	})
}

// strconvParseFeedID 将 ZSet 成员（帖子 ID 字符串）解析为 int64。
func strconvParseFeedID(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}
