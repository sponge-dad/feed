// Package logic 的单元测试：GetCityTimeline（同城池 ZSet 优先，DB 降级）。
package logic

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sponge-dad/feed/app/feed/model"
	"github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/keys"
)

// TestGetCityTimeline_ZSet 验证同城池命中时按 ZSet 分数（时间）倒序回填。
func TestGetCityTimeline_ZSet(t *testing.T) {
	m := newStubFeedsModel()
	m.byID[31] = mkFeed(31, 1, time.Now())
	m.byID[32] = mkFeed(32, 1, time.Now())
	svcCtx := newTestSvc(t, m, &stubRelation{})
	// 分数 32 > 31，倒序后 32 在前。
	zadd(t, svcCtx.Redis, keys.City("440300"), 32, 32)
	zadd(t, svcCtx.Redis, keys.City("440300"), 31, 31)
	l := NewGetCityTimelineLogic(context.Background(), svcCtx)

	resp, err := l.GetCityTimeline(&feed.GetCityTimelineReq{CityCode: "440300", Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, resp.Feeds, 2)
	assert.Equal(t, int64(32), resp.Feeds[0].FeedId)
	assert.Equal(t, int64(31), resp.Feeds[1].FeedId)
}

// TestGetCityTimeline_EmptyCity 验证空城市码返回空列表。
func TestGetCityTimeline_EmptyCity(t *testing.T) {
	m := newStubFeedsModel()
	svcCtx := newTestSvc(t, m, &stubRelation{})
	l := NewGetCityTimelineLogic(context.Background(), svcCtx)

	resp, err := l.GetCityTimeline(&feed.GetCityTimelineReq{CityCode: "", Page: 1, PageSize: 10})
	require.NoError(t, err)
	assert.Empty(t, resp.Feeds)
}

// TestGetCityTimeline_DegradeDB 验证同城池为空时降级 MySQL。
func TestGetCityTimeline_DegradeDB(t *testing.T) {
	m := newStubFeedsModel()
	m.byCity["440300"] = []*model.Feeds{mkFeed(33, 1, time.Now()), mkFeed(34, 1, time.Now())}
	svcCtx := newTestSvc(t, m, &stubRelation{})
	l := NewGetCityTimelineLogic(context.Background(), svcCtx)

	resp, err := l.GetCityTimeline(&feed.GetCityTimelineReq{CityCode: "440300", Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, resp.Feeds, 2)
	assert.Equal(t, int64(33), resp.Feeds[0].FeedId)
	assert.Equal(t, int64(34), resp.Feeds[1].FeedId)
}
