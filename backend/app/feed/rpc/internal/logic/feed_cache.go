// Package logic 实现 Feed 服务的所有 gRPC 业务逻辑。
// feed_cache.go 负责帖子详情业务缓存（feed:{id} Hash）的 cache-aside 读写，
// 规范见 docs/design/feed/06-cache-strategy.md。
package logic

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"

	feed "github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/keys"
)

// feedDetailTTLSec 帖子详情缓存过期时间（30 天）。
// 详情为读多写少场景，长 TTL 可大幅降低 DB 压力；写/删帖时由 DeleteFeed 全量 DEL 重建。
const feedDetailTTLSec = 30 * 24 * 3600

// cacheFeedDetail 将帖子详情异步写入业务缓存 feed:{id} Hash。
// 采用异步 goroutine 回写，避免缓存写入阻塞读主流程；写入失败仅记录日志，不向上抛错。
func cacheFeedDetail(rdb *redis.Redis, ctx context.Context, info *feed.FeedInfo) {
	go func() {
		fields := map[string]string{
			"feed_id":       strconv.FormatInt(info.FeedId, 10),
			"author_id":     strconv.FormatInt(info.AuthorId, 10),
			"feed_type":     strconv.Itoa(int(info.FeedType)),
			"title":         info.Title,
			"description":   info.Description,
			"cover_url":     info.CoverUrl,
			"media_urls":    mustJSON(info.MediaUrls),
			"city_code":     info.CityCode,
			"city_name":     info.CityName,
			"ip_location":   info.IpLocation,
			"is_vip_feed":   boolToIntStr(info.IsVipFeed),
			"like_count":    strconv.FormatInt(info.LikeCount, 10),
			"comment_count": strconv.FormatInt(info.CommentCount, 10),
			"collect_count": strconv.FormatInt(info.CollectCount, 10),
			"created_at":    strconv.FormatInt(info.CreatedAt, 10),
			"updated_at":    strconv.FormatInt(info.UpdatedAt, 10),
			"status":        strconv.Itoa(int(info.Status)),
		}
		key := keys.FeedDetail(info.FeedId)
		if err := rdb.HmsetCtx(ctx, key, fields); err != nil {
			logx.WithContext(ctx).Errorf("cacheFeedDetail Hmset failed feedId=%d err=%v", info.FeedId, err)
			return
		}
		if err := rdb.ExpireCtx(ctx, key, feedDetailTTLSec); err != nil {
			logx.WithContext(ctx).Errorf("cacheFeedDetail Expire failed feedId=%d err=%v", info.FeedId, err)
		}
	}()
}

// loadFeedDetail 从业务缓存 feed:{id} Hash 读取帖子详情。
// 命中且字段完整时返回 (info, true)；未命中或字段非法时返回 (nil, false)。
func loadFeedDetail(rdb *redis.Redis, ctx context.Context, feedID int64) (*feed.FeedInfo, bool) {
	fields, err := rdb.HgetallCtx(ctx, keys.FeedDetail(feedID))
	if err != nil || len(fields) == 0 {
		return nil, false
	}
	feedID2, err := strconv.ParseInt(fields["feed_id"], 10, 64)
	if err != nil {
		return nil, false
	}
	authorID, _ := strconv.ParseInt(fields["author_id"], 10, 64)
	feedType, _ := strconv.Atoi(fields["feed_type"])
	likeCount, _ := strconv.ParseInt(fields["like_count"], 10, 64)
	commentCount, _ := strconv.ParseInt(fields["comment_count"], 10, 64)
	collectCount, _ := strconv.ParseInt(fields["collect_count"], 10, 64)
	createdAt, _ := strconv.ParseInt(fields["created_at"], 10, 64)
	updatedAt, _ := strconv.ParseInt(fields["updated_at"], 10, 64)
	status, _ := strconv.Atoi(fields["status"])
	isVip, _ := strconv.Atoi(fields["is_vip_feed"])

	info := &feed.FeedInfo{
		FeedId:       feedID2,
		AuthorId:     authorID,
		FeedType:     int32(feedType),
		Title:        fields["title"],
		Description:  fields["description"],
		CoverUrl:     fields["cover_url"],
		MediaUrls:    parseJSONURLs(fields["media_urls"]),
		CityCode:     fields["city_code"],
		CityName:     fields["city_name"],
		IpLocation:   fields["ip_location"],
		IsVipFeed:    isVip == 1,
		LikeCount:    likeCount,
		CommentCount: commentCount,
		CollectCount: collectCount,
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
		Status:       int32(status),
	}
	return info, true
}

// mustJSON 将媒体 URL 列表序列化为 JSON 字符串，失败兜底为 "[]"。
func mustJSON(v []string) string {
	if v == nil {
		return "[]"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// parseJSONURLs 将缓存中的 JSON 字符串解析为媒体 URL 列表，非法时返回 nil。
func parseJSONURLs(s string) []string {
	if s == "" {
		return nil
	}
	var urls []string
	if err := json.Unmarshal([]byte(s), &urls); err != nil {
		return nil
	}
	return urls
}

// boolToIntStr 将布尔值转为 "1"/"0" 字符串，便于存入 Hash。
func boolToIntStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
