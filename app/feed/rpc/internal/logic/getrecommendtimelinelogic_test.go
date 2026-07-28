// Package logic 的单元测试：GetRecommendTimeline（推荐池 ZSet 读取）。
package logic

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/keys"
)

// TestGetRecommendTimeline 验证推荐池命中时按 ZSet 分数（时间）倒序回填。
func TestGetRecommendTimeline(t *testing.T) {
	m := newStubFeedsModel()
	m.byID[41] = mkFeed(41, 1, time.Now())
	m.byID[42] = mkFeed(42, 1, time.Now())
	svcCtx := newTestSvc(t, m, &stubRelation{})
	// 分数 42 > 41，倒序后 42 在前。
	zadd(t, svcCtx.Redis, keys.Recommend(), 42, 42)
	zadd(t, svcCtx.Redis, keys.Recommend(), 41, 41)
	l := NewGetRecommendTimelineLogic(context.Background(), svcCtx)

	resp, err := l.GetRecommendTimeline(&feed.GetRecommendTimelineReq{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, resp.Feeds, 2)
	assert.Equal(t, int64(42), resp.Feeds[0].FeedId)
	assert.Equal(t, int64(41), resp.Feeds[1].FeedId)
}

// TestGetRecommendTimeline_Empty 验证推荐池为空时返回空列表。
func TestGetRecommendTimeline_Empty(t *testing.T) {
	m := newStubFeedsModel()
	svcCtx := newTestSvc(t, m, &stubRelation{})
	l := NewGetRecommendTimelineLogic(context.Background(), svcCtx)

	resp, err := l.GetRecommendTimeline(&feed.GetRecommendTimelineReq{Page: 1, PageSize: 10})
	require.NoError(t, err)
	assert.Empty(t, resp.Feeds)
}
