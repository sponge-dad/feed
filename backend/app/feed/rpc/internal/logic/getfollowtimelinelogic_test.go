// Package logic 的单元测试：GetFollowTimeline（推拉结合 + 游标分页）。
package logic

import (
	"context"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/keys"
	"github.com/sponge-dad/feed/common/errorx"
)

// TestGetFollowTimeline 验证普通好友（inbox）+ 关注大V（outbox）合并、排序、游标翻页。
// 数据布局：
//   - inbox(1001): 51@51, 52@52（普通好友已推）
//   - outbox(200)(大V): 71@71, 72@70（实时拉）
//
// 期望全局按 (score 倒序, id 倒序)：71, 72, 52, 51；pageSize=2 取前两页。
func TestGetFollowTimeline(t *testing.T) {
	m := newStubFeedsModel()
	now := time.Now()
	m.byID[51] = mkFeed(51, 9001, now)
	m.byID[52] = mkFeed(52, 9002, now)
	m.byID[71] = mkFeed(71, 200, now)
	m.byID[72] = mkFeed(72, 200, now)

	rel := &stubRelation{
		followees: []int64{200, 201},
		vips:      map[int64]bool{200: true}, // 200 是大V，201 是普通用户（其帖子已在 inbox）
	}
	svcCtx := newTestSvc(t, m, rel)

	zadd(t, svcCtx.Redis, keys.Inbox(1001), 51, 51)
	zadd(t, svcCtx.Redis, keys.Inbox(1001), 52, 52)
	zadd(t, svcCtx.Redis, keys.Outbox(200), 71, 71)
	zadd(t, svcCtx.Redis, keys.Outbox(200), 70, 72)

	l := NewGetFollowTimelineLogic(context.Background(), svcCtx)

	// 第一页
	resp, err := l.GetFollowTimeline(&feed.GetFollowTimelineReq{UserId: 1001, PageSize: 2})
	require.NoError(t, err)
	require.Len(t, resp.Feeds, 2)
	assert.Equal(t, int64(71), resp.Feeds[0].FeedId)
	assert.Equal(t, int64(72), resp.Feeds[1].FeedId)
	require.NotEmpty(t, resp.Page.Cursor)

	// 第二页（携带上一页游标）
	resp2, err := l.GetFollowTimeline(&feed.GetFollowTimelineReq{UserId: 1001, PageSize: 2, Cursor: resp.Page.Cursor})
	require.NoError(t, err)
	require.Len(t, resp2.Feeds, 2)
	assert.Equal(t, int64(52), resp2.Feeds[0].FeedId)
	assert.Equal(t, int64(51), resp2.Feeds[1].FeedId)
}

// TestGetFollowTimeline_Param 验证非法用户 ID 与非法游标返回 ParamError。
func TestGetFollowTimeline_Param(t *testing.T) {
	m := newStubFeedsModel()
	svcCtx := newTestSvc(t, m, &stubRelation{})
	l := NewGetFollowTimelineLogic(context.Background(), svcCtx)

	_, err := l.GetFollowTimeline(&feed.GetFollowTimelineReq{UserId: 0, PageSize: 10})
	ce, ok := err.(*errorx.CodeError)
	require.True(t, ok)
	assert.Equal(t, errorx.ParamError, ce.Code)

	_, err = l.GetFollowTimeline(&feed.GetFollowTimelineReq{UserId: 1001, PageSize: 10, Cursor: "!!!not-base64!!!"})
	ce, ok = err.(*errorx.CodeError)
	require.True(t, ok)
	assert.Equal(t, errorx.ParamError, ce.Code)
}

// TestGetFollowTimeline_SourceMarking 验证关注流来源标记：
// inbox（推模式）命中 FOLLOW_INBOX，大V outbox（拉模式）命中 VIP_OUTBOX。
func TestGetFollowTimeline_SourceMarking(t *testing.T) {
	m := newStubFeedsModel()
	now := time.Now()
	m.byID[51] = mkFeed(51, 9001, now)
	m.byID[52] = mkFeed(52, 9002, now)
	m.byID[71] = mkFeed(71, 200, now)
	m.byID[72] = mkFeed(72, 200, now)

	rel := &stubRelation{
		followees: []int64{200, 201},
		vips:      map[int64]bool{200: true}, // 200 是大V，201 是普通用户（其帖子已在 inbox）
	}
	svcCtx := newTestSvc(t, m, rel)

	zadd(t, svcCtx.Redis, keys.Inbox(1001), 51, 51)
	zadd(t, svcCtx.Redis, keys.Inbox(1001), 52, 52)
	zadd(t, svcCtx.Redis, keys.Outbox(200), 71, 71)
	zadd(t, svcCtx.Redis, keys.Outbox(200), 70, 72)

	l := NewGetFollowTimelineLogic(context.Background(), svcCtx)
	resp, err := l.GetFollowTimeline(&feed.GetFollowTimelineReq{UserId: 1001, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, resp.Feeds, 4)

	srcByID := make(map[int64]feed.FeedSource, len(resp.Feeds))
	for _, f := range resp.Feeds {
		srcByID[f.FeedId] = feed.FeedSource(f.Source)
	}
	assert.Equal(t, feed.FeedSource_FEED_SOURCE_FOLLOW_INBOX, srcByID[51])
	assert.Equal(t, feed.FeedSource_FEED_SOURCE_FOLLOW_INBOX, srcByID[52])
	assert.Equal(t, feed.FeedSource_FEED_SOURCE_VIP_OUTBOX, srcByID[71])
	assert.Equal(t, feed.FeedSource_FEED_SOURCE_VIP_OUTBOX, srcByID[72])
}

// TestGetFollowTimeline_SourcePriority 验证同一 feed 被 inbox 与 bigV outbox
// 两路命中时，以 FOLLOW_INBOX 优先（推模式已确认推送）。
func TestGetFollowTimeline_SourcePriority(t *testing.T) {
	m := newStubFeedsModel()
	now := time.Now()
	m.byID[71] = mkFeed(71, 200, now)

	rel := &stubRelation{
		followees: []int64{200, 201},
		vips:      map[int64]bool{200: true},
	}
	svcCtx := newTestSvc(t, m, rel)

	// 同一帖子 71 既在 inbox（推模式）也在大V outbox（拉模式）。
	zadd(t, svcCtx.Redis, keys.Inbox(1001), 71, 71)
	zadd(t, svcCtx.Redis, keys.Outbox(200), 71, 71)

	l := NewGetFollowTimelineLogic(context.Background(), svcCtx)
	resp, err := l.GetFollowTimeline(&feed.GetFollowTimelineReq{UserId: 1001, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, resp.Feeds, 1)
	assert.Equal(t, int64(71), resp.Feeds[0].FeedId)
	assert.Equal(t, feed.FeedSource_FEED_SOURCE_FOLLOW_INBOX, feed.FeedSource(resp.Feeds[0].Source))
}

// TestGetFollowTimeline_InboxRebuild 验证 inbox 为空且用户有关注关系时，
// 由 GetFollows + 各作者 outbox 兜底重建并回写 inbox，命中标记 INBOX_REBUILD。
func TestGetFollowTimeline_InboxRebuild(t *testing.T) {
	m := newStubFeedsModel()
	now := time.Now()
	m.byID[81] = mkFeed(81, 200, now)
	m.byID[82] = mkFeed(82, 201, now)

	// inbox 为空，但关注了 200、201（其 outbox 有内容）。
	rel := &stubRelation{
		followees: []int64{200, 201},
		vips:      map[int64]bool{},
	}
	svcCtx := newTestSvc(t, m, rel)

	zadd(t, svcCtx.Redis, keys.Outbox(200), 81, 81)
	zadd(t, svcCtx.Redis, keys.Outbox(201), 82, 82)

	l := NewGetFollowTimelineLogic(context.Background(), svcCtx)
	resp, err := l.GetFollowTimeline(&feed.GetFollowTimelineReq{UserId: 1001, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, resp.Feeds, 2)

	srcByID := make(map[int64]feed.FeedSource, len(resp.Feeds))
	for _, f := range resp.Feeds {
		srcByID[f.FeedId] = feed.FeedSource(f.Source)
	}
	assert.Equal(t, feed.FeedSource_FEED_SOURCE_INBOX_REBUILD, srcByID[81])
	assert.Equal(t, feed.FeedSource_FEED_SOURCE_INBOX_REBUILD, srcByID[82])

	// inbox 应被兜底重建并回写，便于后续请求命中推模式（FOLLOW_INBOX）。
	rebuilt, err := svcCtx.Redis.ZrevrangebyscoreWithScoresAndLimitCtx(context.Background(), keys.Inbox(1001), math.MinInt64, math.MaxInt64, 0, followInboxReadCap)
	require.NoError(t, err)
	rebuiltIDs := make(map[int64]bool, len(rebuilt))
	for _, p := range rebuilt {
		id, e := strconv.ParseInt(p.Key, 10, 64)
		require.NoError(t, e)
		rebuiltIDs[id] = true
	}
	assert.True(t, rebuiltIDs[81], "inbox 应回写 outbox(200) 的帖子 81")
	assert.True(t, rebuiltIDs[82], "inbox 应回写 outbox(201) 的帖子 82")
}
