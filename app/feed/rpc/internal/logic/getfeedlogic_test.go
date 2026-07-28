// Package logic 的单元测试：GetFeed（cache-aside 读路径）。
package logic

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/stores/redis"

	"github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/keys"
	"github.com/sponge-dad/feed/common/errorx"
)

// TestGetFeed_Miss 验证缓存未命中时回源 DB 并填充详情。
func TestGetFeed_Miss(t *testing.T) {
	m := newStubFeedsModel()
	m.byID[31] = mkFeed(31, 100, time.Now())
	svcCtx := newTestSvc(t, m, &stubRelation{})
	l := NewGetFeedLogic(context.Background(), svcCtx)

	resp, err := l.GetFeed(&feed.GetFeedReq{FeedId: 31})
	require.NoError(t, err)
	assert.Equal(t, int64(31), resp.Feed.FeedId)
	assert.Equal(t, int64(100), resp.Feed.AuthorId)
	assert.Equal(t, 1, m.findOneCalls)
}

// TestGetFeed_Hit 验证缓存命中时直接返回，不回源 DB。
func TestGetFeed_Hit(t *testing.T) {
	m := newStubFeedsModel()
	svcCtx := newTestSvc(t, m, &stubRelation{})
	seedFeedCache(t, svcCtx.Redis, &feed.FeedInfo{FeedId: 31, AuthorId: 100, FeedType: 1, Title: "cached"})
	l := NewGetFeedLogic(context.Background(), svcCtx)

	resp, err := l.GetFeed(&feed.GetFeedReq{FeedId: 31})
	require.NoError(t, err)
	assert.Equal(t, int64(31), resp.Feed.FeedId)
	assert.Equal(t, "cached", resp.Feed.Title)
	assert.Equal(t, 0, m.findOneCalls)
}

// TestGetFeed_NotFound 验证帖子不存在时返回 FeedNotFound 业务码。
func TestGetFeed_NotFound(t *testing.T) {
	m := newStubFeedsModel()
	svcCtx := newTestSvc(t, m, &stubRelation{})
	l := NewGetFeedLogic(context.Background(), svcCtx)

	_, err := l.GetFeed(&feed.GetFeedReq{FeedId: 999})
	require.Error(t, err)
	ce, ok := err.(*errorx.CodeError)
	require.True(t, ok)
	assert.Equal(t, errorx.FeedNotFound, ce.Code)
}

// TestGetFeed_Param 验证非法 ID 返回 ParamError。
func TestGetFeed_Param(t *testing.T) {
	m := newStubFeedsModel()
	svcCtx := newTestSvc(t, m, &stubRelation{})
	l := NewGetFeedLogic(context.Background(), svcCtx)

	_, err := l.GetFeed(&feed.GetFeedReq{FeedId: 0})
	require.Error(t, err)
	ce, ok := err.(*errorx.CodeError)
	require.True(t, ok)
	assert.Equal(t, errorx.ParamError, ce.Code)
}

// seedFeedCache 同步写入 feed:{id} Hash，用于测试缓存命中路径。
func seedFeedCache(t *testing.T, rdb *redis.Redis, info *feed.FeedInfo) {
	t.Helper()
	fields := map[string]string{
		"feed_id":       strconv.FormatInt(info.FeedId, 10),
		"author_id":     strconv.FormatInt(info.AuthorId, 10),
		"feed_type":     strconv.Itoa(int(info.FeedType)),
		"title":         info.Title,
		"description":   info.Description,
		"cover_url":     info.CoverUrl,
		"media_urls":    "[]",
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
	require.NoError(t, rdb.HmsetCtx(context.Background(), keys.FeedDetail(info.FeedId), fields))
	require.NoError(t, rdb.ExpireCtx(context.Background(), keys.FeedDetail(info.FeedId), feedDetailTTLSec))
}
