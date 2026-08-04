// createfeedlogic_test.go
//
// CreateFeed RPC Logic 单元测试。对应基线 F-CRT-01 ~ F-CRT-04、
// 事件基线 E-FC 系列、风险基线 R-P0-2（MQ 失败不回滚、无补偿）。
//
// 与基线不一致的记录（详见 docs/test-implementation-report.md）：
// 基线 F-CRT-03 预期 IsVip 失败时降级 is_vip=false 继续发布；
// 当前代码（createfeedlogic.go:58-62）实际直接返回错误。本文件按代码实际行为断言。
package logic

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"
	feedEvent "github.com/sponge-dad/feed/common/event/feed"
)

const testFeedID = int64(424242)

// newCreateTestSvc 构造带可控 IdGen 与可记录 Producer 的 ServiceContext。
func newCreateTestSvc(t *testing.T, m *ctrlFeedsModel, rel *errRelation, pub *recordingPublisher) *svc.ServiceContext {
	t.Helper()
	ctx := newTestSvc(t, m, rel)
	ctx.Producer = pub
	ctx.IdGen = func() int64 { return testFeedID }
	return ctx
}

func validImageReq() *feed.CreateFeedReq {
	return &feed.CreateFeedReq{
		AuthorId:    10001,
		FeedType:    int32(feed.FeedType_FEED_TYPE_IMAGE),
		Title:       "标题",
		Description: "正文内容",
		MediaUrls:   []string{"http://cdn/1.jpg", "http://cdn/2.jpg"},
		CityCode:    "440300",
		CityName:    "深圳",
		IpLocation:  "广东",
	}
}

// Baseline: F-CRT-01（图文发布：响应、DB、MQ 消息逐字段断言）
func TestCreateFeed_ImageFeedByNormalUser_InsertsRowAndPublishesEvent(t *testing.T) {
	m := newCtrlFeedsModel()
	pub := &recordingPublisher{}
	svcCtx := newCreateTestSvc(t, m, &errRelation{isVip: false}, pub)
	l := NewCreateFeedLogic(context.Background(), svcCtx)

	before := time.Now().UnixMilli()
	resp, err := l.CreateFeed(validImageReq())
	after := time.Now().UnixMilli()
	require.NoError(t, err)
	require.NotNil(t, resp.Feed)

	// 响应字段
	assert.Equal(t, testFeedID, resp.Feed.FeedId)
	assert.Equal(t, int64(10001), resp.Feed.AuthorId)
	assert.EqualValues(t, feed.FeedType_FEED_TYPE_IMAGE, resp.Feed.FeedType)
	assert.Equal(t, "标题", resp.Feed.Title)
	assert.Equal(t, "正文内容", resp.Feed.Description)
	assert.Equal(t, []string{"http://cdn/1.jpg", "http://cdn/2.jpg"}, resp.Feed.MediaUrls)
	assert.EqualValues(t, 1, resp.Feed.Status)
	assert.False(t, resp.Feed.IsVipFeed)
	assert.Zero(t, resp.Feed.LikeCount)

	// 数据库副作用：status=1、is_vip=0、media JSON
	stored, ok := m.byID[uint64(testFeedID)]
	require.True(t, ok, "feeds 表应新增记录")
	assert.EqualValues(t, 10001, stored.UserId)
	assert.EqualValues(t, 1, stored.Status)
	assert.EqualValues(t, 0, stored.IsVipFeed)
	require.True(t, stored.MediaUrls.Valid)
	assert.JSONEq(t, `["http://cdn/1.jpg","http://cdn/2.jpg"]`, stored.MediaUrls.String)
	assert.Equal(t, "440300", stored.CityCode)
	assert.Equal(t, 1, m.insertCalls)

	// MQ 消息：恰好 1 条，Topic 与消息体逐字段断言
	msgs := pub.messages()
	require.Len(t, msgs, 1)
	assert.Equal(t, feedEvent.TopicFeedCreated, msgs[0].Topic)

	var ev feedEvent.EventFeedCreate
	require.NoError(t, json.Unmarshal(msgs[0].Body, &ev))
	assert.Len(t, ev.EventID, 36, "event_id 应为 UUID v4 格式")
	assert.Equal(t, feedEvent.TopicFeedCreated, ev.EventType)
	assert.Equal(t, testFeedID, ev.FeedID)
	assert.Equal(t, int64(10001), ev.UserID)
	assert.False(t, ev.IsVipFeed)
	assert.Equal(t, "440300", ev.CityCode)
	assert.GreaterOrEqual(t, ev.CreatedAt, before)
	assert.LessOrEqual(t, ev.CreatedAt, after)
}

// Baseline: F-CRT-01（视频发布：带封面成功）
func TestCreateFeed_VideoFeedWithCover_Succeeds(t *testing.T) {
	m := newCtrlFeedsModel()
	pub := &recordingPublisher{}
	svcCtx := newCreateTestSvc(t, m, &errRelation{}, pub)
	l := NewCreateFeedLogic(context.Background(), svcCtx)

	req := validImageReq()
	req.FeedType = int32(feed.FeedType_FEED_TYPE_VIDEO)
	req.MediaUrls = []string{"http://cdn/v.mp4"}
	req.CoverUrl = "http://cdn/cover.jpg"

	resp, err := l.CreateFeed(req)
	require.NoError(t, err)
	assert.Equal(t, "http://cdn/cover.jpg", resp.Feed.CoverUrl)
	assert.EqualValues(t, 2, m.byID[uint64(testFeedID)].FeedType)
	assert.Equal(t, 1, pub.callCount())
}

// Baseline: F-CRT-02（参数矩阵：非法入参不产生任何 DB/MQ 副作用）
func TestCreateFeed_InvalidParams_ReturnBizCodeWithoutSideEffects(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(*feed.CreateFeedReq)
		wantCode int
	}{
		{"作者ID为0", func(r *feed.CreateFeedReq) { r.AuthorId = 0 }, errorx.ParamError},
		{"作者ID为负数", func(r *feed.CreateFeedReq) { r.AuthorId = -1 }, errorx.ParamError},
		{"不支持的媒体类型3", func(r *feed.CreateFeedReq) { r.FeedType = 3 }, errorx.ParamError},
		{"媒体类型为0", func(r *feed.CreateFeedReq) { r.FeedType = 0 }, errorx.ParamError},
		{"正文为空", func(r *feed.CreateFeedReq) { r.Description = "" }, errorx.FeedEmptyContent},
		{"媒体列表为空", func(r *feed.CreateFeedReq) { r.MediaUrls = nil }, errorx.FeedEmptyMedia},
		{"视频缺封面", func(r *feed.CreateFeedReq) {
			r.FeedType = int32(feed.FeedType_FEED_TYPE_VIDEO)
			r.CoverUrl = ""
		}, errorx.FeedEmptyMedia},
		{"内容与媒体同时为空（先报内容空）", func(r *feed.CreateFeedReq) {
			r.Description = ""
			r.MediaUrls = nil
		}, errorx.FeedEmptyContent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newCtrlFeedsModel()
			pub := &recordingPublisher{}
			svcCtx := newCreateTestSvc(t, m, &errRelation{}, pub)
			l := NewCreateFeedLogic(context.Background(), svcCtx)

			req := validImageReq()
			tc.mutate(req)
			resp, err := l.CreateFeed(req)

			require.Nil(t, resp)
			var codeErr *errorx.CodeError
			require.ErrorAs(t, err, &codeErr)
			assert.Equal(t, tc.wantCode, codeErr.Code)
			assert.Equal(t, 0, m.insertCalls, "参数错误不得写库")
			assert.Equal(t, 0, pub.callCount(), "参数错误不得发消息")
		})
	}
}

// Baseline: F-CRT-01 派生（VIP 用户发布：DB is_vip=1，事件 is_vip_feed=true）
func TestCreateFeed_VipAuthor_MarksVipInRowAndEvent(t *testing.T) {
	m := newCtrlFeedsModel()
	pub := &recordingPublisher{}
	svcCtx := newCreateTestSvc(t, m, &errRelation{isVip: true}, pub)
	l := NewCreateFeedLogic(context.Background(), svcCtx)

	resp, err := l.CreateFeed(validImageReq())
	require.NoError(t, err)

	assert.True(t, resp.Feed.IsVipFeed)
	assert.EqualValues(t, 1, m.byID[uint64(testFeedID)].IsVipFeed)

	var ev feedEvent.EventFeedCreate
	require.NoError(t, json.Unmarshal(pub.messages()[0].Body, &ev))
	assert.True(t, ev.IsVipFeed)
}

// Baseline: F-CRT-03（行为基线，与基线文档不一致）
// 基线预期：IsVip 失败降级 is_vip=false 继续发布。
// 当前代码实际行为：IsVip 调用失败 → 整个请求失败，不写库、不发消息。
func TestCreateFeed_IsVipFails_CurrentlyFailsWholeRequestBaseline(t *testing.T) {
	m := newCtrlFeedsModel()
	pub := &recordingPublisher{}
	rpcErr := errors.New("relation rpc: connection refused")
	svcCtx := newCreateTestSvc(t, m, &errRelation{isVipErr: rpcErr}, pub)
	l := NewCreateFeedLogic(context.Background(), svcCtx)

	resp, err := l.CreateFeed(validImageReq())
	assert.Nil(t, resp)
	require.ErrorIs(t, err, rpcErr, "当前实现直接透传 IsVip 错误（与基线降级预期不一致，行为基线）")
	assert.Equal(t, 0, m.insertCalls, "IsVip 失败时不应写库")
	assert.Equal(t, 0, pub.callCount())
}

// Baseline: F-CRT-01 派生（MySQL 写入失败 → 返回错误且不发消息）
func TestCreateFeed_InsertFails_ReturnsErrorWithoutMessage(t *testing.T) {
	m := newCtrlFeedsModel()
	dbErr := errors.New("mysql: disk full")
	m.insertErr = dbErr
	pub := &recordingPublisher{}
	svcCtx := newCreateTestSvc(t, m, &errRelation{}, pub)
	l := NewCreateFeedLogic(context.Background(), svcCtx)

	resp, err := l.CreateFeed(validImageReq())
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, dbErr)
	assert.Equal(t, 0, pub.callCount(), "写库失败后不得发送 feed-created 消息")
	assert.Empty(t, m.byID)
}

// Baseline: F-CRT-04 / Risk baseline: R-P0-2（消息一致性基线）
// 当前行为：MySQL 写入成功但 MQ 发送失败时，接口仍返回成功，
// 数据库记录保留，消息永久丢失（仅 1 次发送尝试，无补偿/重试/本地消息表）。
func TestCreateFeed_WhenMQSendFails_CurrentlyKeepsDatabaseRecordAndReturnsSuccess(t *testing.T) {
	m := newCtrlFeedsModel()
	pub := &recordingPublisher{sendErr: errors.New("rocketmq: broker unavailable")}
	svcCtx := newCreateTestSvc(t, m, &errRelation{}, pub)
	l := NewCreateFeedLogic(context.Background(), svcCtx)

	resp, err := l.CreateFeed(validImageReq())

	// 接口当前返回成功（部分成功行为基线）
	require.NoError(t, err, "MQ 失败不阻塞主流程（当前行为基线）")
	require.NotNil(t, resp.Feed)
	assert.Equal(t, testFeedID, resp.Feed.FeedId)

	// MySQL 最终状态：记录保留，status=1
	stored, ok := m.byID[uint64(testFeedID)]
	require.True(t, ok, "DB 记录保留")
	assert.EqualValues(t, 1, stored.Status)

	// Producer 恰好被调用 1 次：无重试、无补偿，消息丢失
	assert.Equal(t, 1, pub.callCount(), "仅一次发送尝试，无重试")
	assert.Empty(t, pub.messages(), "消息实际未发出（永久丢失）")
}
