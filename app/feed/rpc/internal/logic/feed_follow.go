// Package logic 实现 Feed 服务的所有 gRPC 业务逻辑。
// feed_follow.go 提供关注流（推拉结合）的游标编解码与候选候选排序辅助逻辑。
package logic

import (
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// followOutboxPullSize 每个关注大V单次拉取的最近帖子数（outbox 取最新 N 条）。
const followOutboxPullSize = 30

// followMaxBigV 单次关注流最多拉取的大V发件箱数量，避免过度放大读放大。
const followMaxBigV = 200

// followInboxReadCap 单次关注流读取收件箱的最大条数（与 worker inboxCap 对齐）。
const followInboxReadCap = 1000

// feedScore 是候选流中的一条帖子：feedID 与其在 ZSet 中的秒级 score。
type feedScore struct {
	feedID int64
	score  int64
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
