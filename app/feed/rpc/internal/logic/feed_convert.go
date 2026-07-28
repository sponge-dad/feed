// Package logic 实现 Feed 服务的所有 gRPC 业务逻辑。
// feed_convert.go 负责 model.Feeds 实体与 proto FeedInfo / FeedBrief 之间的互转，
// 以及媒体 URL（JSON 数组）的序列化与反序列化。
package logic

import (
	"encoding/json"
	"strconv"

	"github.com/zeromicro/go-zero/core/stores/redis"

	"github.com/sponge-dad/feed/app/feed/model"
	feed "github.com/sponge-dad/feed/app/feed/rpc/feed"
)

const (
	// defaultUserFeedPageSize 个人主页 / 时间线默认每页条数。
	defaultUserFeedPageSize = 20
	// maxUserFeedPageSize 单页上限，防止一次拉取过多。
	maxUserFeedPageSize = 50
)

// parseMediaUrls 将数据库存储的 JSON 字符串解析为媒体 URL 列表。
// 空值或非法 JSON 统一返回空切片，避免上层空指针。
func parseMediaUrls(f *model.Feeds) []string {
	if !f.MediaUrls.Valid || f.MediaUrls.String == "" {
		return nil
	}
	var urls []string
	if err := json.Unmarshal([]byte(f.MediaUrls.String), &urls); err != nil {
		return nil
	}
	return urls
}

// toFeedBrief 将持久化实体转换为对外列表项 FeedBrief。
// 列表场景只返回轻量字段，降低网络开销（字段以 feed.proto 的 FeedBrief 为准）。
func toFeedBrief(f *model.Feeds) *feed.FeedBrief {
	return &feed.FeedBrief{
		FeedId:       int64(f.Id),
		AuthorId:     int64(f.UserId),
		FeedType:     int32(f.FeedType),
		Title:        f.Title,
		CoverUrl:     f.CoverUrl,
		CityCode:     f.CityCode,
		LikeCount:    int64(f.LikeCount),
		CommentCount: int64(f.CommentCount),
		CreatedAt:    f.CreatedAt.UnixMilli(),
	}
}

// toFeedInfo 将持久化实体转换为帖子详情 FeedInfo（含更新时间与更多字段）。
func toFeedInfo(f *model.Feeds) *feed.FeedInfo {
	return &feed.FeedInfo{
		FeedId:       int64(f.Id),
		AuthorId:     int64(f.UserId),
		FeedType:     int32(f.FeedType),
		Title:        f.Title,
		Description:  f.Description,
		CoverUrl:     f.CoverUrl,
		MediaUrls:    parseMediaUrls(f),
		CityCode:     f.CityCode,
		CityName:     f.CityName,
		IpLocation:   f.IpLocation,
		IsVipFeed:    f.IsVipFeed == 1,
		LikeCount:    int64(f.LikeCount),
		CommentCount: int64(f.CommentCount),
		CollectCount: int64(f.CollectCount),
		CreatedAt:    f.CreatedAt.UnixMilli(),
		UpdatedAt:    f.UpdatedAt.UnixMilli(),
		Status:       int32(f.Status),
	}
}

// zPairsToFeedIDs 将 ZSet 带分值的成员对解析为帖子 ID 列表。
// 成员即帖子 ID 字符串；解析失败的成员被忽略。
func zPairsToFeedIDs(pairs []redis.Pair) []uint64 {
	ids := make([]uint64, 0, len(pairs))
	for _, p := range pairs {
		id, err := strconv.ParseInt(p.Key, 10, 64)
		if err != nil {
			continue
		}
		ids = append(ids, uint64(id))
	}
	return ids
}

// briefsInPairOrder 按 ZSet 成员顺序（score 倒序）将实体填充为 FeedBrief 列表。
// FindByIds 返回顺序不确定，故以 pairs 顺序为准，保证时间线严格按时间倒序。
func briefsInPairOrder(pairs []redis.Pair, byID map[uint64]*model.Feeds) []*feed.FeedBrief {
	briefs := make([]*feed.FeedBrief, 0, len(pairs))
	for _, p := range pairs {
		id, err := strconv.ParseInt(p.Key, 10, 64)
		if err != nil {
			continue
		}
		if f, ok := byID[uint64(id)]; ok {
			briefs = append(briefs, toFeedBrief(f))
		}
	}
	return briefs
}
