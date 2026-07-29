// deletefeedlogic_test.go
//
// DeleteFeed RPC Logic 单元测试。对应基线 F-DEL-02、F-DEL-03、
// 事件基线 E-FD 系列、风险基线 R-P0-2（MQ 失败无补偿）。
//
// 与基线不一致的记录（详见 docs/test-implementation-report.md）：
//  1. 基线 F-DEL-02 预期删除时 Redis key feed:{id} 被 DEL；当前 logic 不做任何
//     Redis 操作（本文件以"预置 key 仍存在"固化该行为）。
//  2. Feed 不存在时当前返回原始 model.ErrNotFound（未转换为业务码 12001）。
package logic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sponge-dad/feed/app/feed/model"
	"github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"
	feedEvent "github.com/sponge-dad/feed/common/event/feed"
)

const (
	delFeedID  = uint64(555001)
	delOwnerID = uint64(10001)
)

// newDeleteTestSvc 构造 DeleteFeed 测试上下文，预置一条属主为 delOwnerID 的帖子。
func newDeleteTestSvc(t *testing.T, m *ctrlFeedsModel, pub *recordingPublisher) *svc.ServiceContext {
	t.Helper()
	ctx := newTestSvc(t, m, &errRelation{})
	ctx.Producer = pub
	return ctx
}

func seedDeletableFeed(m *ctrlFeedsModel, status int64, isVip int64) *model.Feeds {
	f := mkFeed(delFeedID, delOwnerID, time.Unix(1700000000, 0))
	f.Status = status
	f.IsVipFeed = isVip
	m.byID[delFeedID] = f
	return f
}

// Baseline: F-DEL-02（属主删除：软删除置 status=2，发 feed-deleted 事件）
func TestDeleteFeed_ByOwner_SoftDeletesRowAndPublishesEvent(t *testing.T) {
	m := newCtrlFeedsModel()
	seedDeletableFeed(m, 1, 0)
	pub := &recordingPublisher{}
	svcCtx := newDeleteTestSvc(t, m, pub)

	// 预置 Redis key，用于验证当前 logic 不做缓存删除（基线不一致项 1）
	require.NoError(t, svcCtx.Redis.Set(fmt.Sprintf("feed:%d", delFeedID), "cached"))

	l := NewDeleteFeedLogic(context.Background(), svcCtx)
	resp, err := l.DeleteFeed(&feed.DeleteFeedReq{FeedId: int64(delFeedID), UserId: int64(delOwnerID)})
	require.NoError(t, err)
	assert.True(t, resp.Success)

	// 软删除验证：记录仍在（非物理删除），status=2
	stored, ok := m.byID[delFeedID]
	require.True(t, ok, "软删除后记录必须保留，不得物理删除")
	assert.EqualValues(t, 2, stored.Status)
	assert.Equal(t, 1, m.softDeleteCalls)

	// 当前行为基线：logic 不删除 Redis 缓存（与基线 F-DEL-02 预期不一致）
	val, err := svcCtx.Redis.Get(fmt.Sprintf("feed:%d", delFeedID))
	require.NoError(t, err)
	assert.Equal(t, "cached", val, "当前 DeleteFeed 不做 Redis 删除（行为基线）")

	// MQ 事件：恰好 1 条 feed-deleted，字段逐一断言
	msgs := pub.messages()
	require.Len(t, msgs, 1)
	assert.Equal(t, feedEvent.TopicFeedDeleted, msgs[0].Topic)

	var ev feedEvent.EventFeedDeleted
	require.NoError(t, json.Unmarshal(msgs[0].Body, &ev))
	assert.Len(t, ev.EventID, 36)
	assert.Equal(t, feedEvent.TopicFeedDeleted, ev.EventType)
	assert.Equal(t, int64(delFeedID), ev.FeedID)
	assert.Equal(t, int64(delOwnerID), ev.UserID)
	assert.False(t, ev.IsVipFeed)
	assert.Equal(t, "440300", ev.CityCode)
}

// Baseline: F-DEL-02 派生（VIP 帖删除事件携带 is_vip_feed=true）
func TestDeleteFeed_VipFeed_EventCarriesVipFlag(t *testing.T) {
	m := newCtrlFeedsModel()
	seedDeletableFeed(m, 1, 1)
	pub := &recordingPublisher{}
	svcCtx := newDeleteTestSvc(t, m, pub)

	l := NewDeleteFeedLogic(context.Background(), svcCtx)
	_, err := l.DeleteFeed(&feed.DeleteFeedReq{FeedId: int64(delFeedID), UserId: int64(delOwnerID)})
	require.NoError(t, err)

	var ev feedEvent.EventFeedDeleted
	require.NoError(t, json.Unmarshal(pub.messages()[0].Body, &ev))
	assert.True(t, ev.IsVipFeed)
}

// Baseline: F-DEL-01（RPC 层非属主删除 → 12002，无任何副作用）
func TestDeleteFeed_ByNonOwner_ReturnsNoPermissionWithoutSideEffects(t *testing.T) {
	m := newCtrlFeedsModel()
	seedDeletableFeed(m, 1, 0)
	pub := &recordingPublisher{}
	svcCtx := newDeleteTestSvc(t, m, pub)

	l := NewDeleteFeedLogic(context.Background(), svcCtx)
	resp, err := l.DeleteFeed(&feed.DeleteFeedReq{FeedId: int64(delFeedID), UserId: 99999})

	require.Nil(t, resp)
	var codeErr *errorx.CodeError
	require.ErrorAs(t, err, &codeErr)
	assert.Equal(t, errorx.FeedNoPermission, codeErr.Code) // 12002
	assert.EqualValues(t, 1, m.byID[delFeedID].Status, "非属主删除不得修改记录")
	assert.Equal(t, 0, m.softDeleteCalls)
	assert.Equal(t, 0, pub.callCount())
}

// Baseline: F-DEL-02 派生（行为基线：Feed 不存在返回原始 ErrNotFound，而非 12001）
func TestDeleteFeed_FeedNotFound_CurrentlyReturnsRawErrNotFoundBaseline(t *testing.T) {
	m := newCtrlFeedsModel()
	pub := &recordingPublisher{}
	svcCtx := newDeleteTestSvc(t, m, pub)

	l := NewDeleteFeedLogic(context.Background(), svcCtx)
	resp, err := l.DeleteFeed(&feed.DeleteFeedReq{FeedId: 404, UserId: int64(delOwnerID)})

	require.Nil(t, resp)
	require.ErrorIs(t, err, model.ErrNotFound,
		"当前实现透传 model.ErrNotFound，未转换为业务码 12001（行为基线）")
	var codeErr *errorx.CodeError
	assert.False(t, errors.As(err, &codeErr))
	assert.Equal(t, 0, pub.callCount())
}

// Baseline: F-DEL-03（重复删除幂等：返回成功，不再 UPDATE、不再发事件）
func TestDeleteFeed_AlreadyDeleted_IdempotentSuccessWithoutSecondEvent(t *testing.T) {
	m := newCtrlFeedsModel()
	seedDeletableFeed(m, 2, 0) // 已删除
	pub := &recordingPublisher{}
	svcCtx := newDeleteTestSvc(t, m, pub)

	l := NewDeleteFeedLogic(context.Background(), svcCtx)
	resp, err := l.DeleteFeed(&feed.DeleteFeedReq{FeedId: int64(delFeedID), UserId: int64(delOwnerID)})

	require.NoError(t, err)
	assert.True(t, resp.Success, "重复删除幂等返回成功")
	assert.Equal(t, 0, m.softDeleteCalls, "已删除状态不得再次 UPDATE")
	assert.Equal(t, 0, pub.callCount(), "重复删除不得重发事件")
}

// Baseline: F-DEL-02 派生（参数非法 → code=2）
func TestDeleteFeed_InvalidParams_ReturnsParamError(t *testing.T) {
	cases := []struct {
		name   string
		feedID int64
		userID int64
	}{
		{"feed_id为0", 0, 10001},
		{"user_id为0", 555001, 0},
		{"均为负数", -1, -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newCtrlFeedsModel()
			pub := &recordingPublisher{}
			svcCtx := newDeleteTestSvc(t, m, pub)
			l := NewDeleteFeedLogic(context.Background(), svcCtx)

			resp, err := l.DeleteFeed(&feed.DeleteFeedReq{FeedId: tc.feedID, UserId: tc.userID})
			require.Nil(t, resp)
			var codeErr *errorx.CodeError
			require.ErrorAs(t, err, &codeErr)
			assert.Equal(t, errorx.ParamError, codeErr.Code)
		})
	}
}

// Baseline: F-DEL-02 派生（MySQL 删除失败 → 返回错误且不发事件）
func TestDeleteFeed_SoftDeleteFails_ReturnsErrorWithoutEvent(t *testing.T) {
	m := newCtrlFeedsModel()
	seedDeletableFeed(m, 1, 0)
	dbErr := errors.New("mysql: lock wait timeout")
	m.softDeleteErr = dbErr
	pub := &recordingPublisher{}
	svcCtx := newDeleteTestSvc(t, m, pub)

	l := NewDeleteFeedLogic(context.Background(), svcCtx)
	resp, err := l.DeleteFeed(&feed.DeleteFeedReq{FeedId: int64(delFeedID), UserId: int64(delOwnerID)})

	assert.Nil(t, resp)
	assert.ErrorIs(t, err, dbErr)
	assert.Equal(t, 0, pub.callCount(), "删除失败不得发事件")
}

// Baseline: F-DEL-03 派生（UPDATE 影响 0 行：返回成功但不发事件——并发竞态兜底）
func TestDeleteFeed_SoftDeleteAffectsZeroRows_SuccessWithoutEvent(t *testing.T) {
	m := newCtrlFeedsModel()
	seedDeletableFeed(m, 1, 0)
	m.softDeleteRes = false
	pub := &recordingPublisher{}
	svcCtx := newDeleteTestSvc(t, m, pub)

	l := NewDeleteFeedLogic(context.Background(), svcCtx)
	resp, err := l.DeleteFeed(&feed.DeleteFeedReq{FeedId: int64(delFeedID), UserId: int64(delOwnerID)})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, 0, pub.callCount())
}

// Baseline: F-DEL-02 派生 / Risk baseline: R-P0-2（消息一致性基线）
// 当前行为：数据库软删除成功但 MQ 发送失败时，接口仍返回成功，
// status 已置 2，feed-deleted 消息永久丢失（仅 1 次尝试，无补偿），
// 时间线将残留已删除帖子。
func TestDeleteFeed_WhenMQSendFails_CurrentlyKeepsSoftDeleteAndReturnsSuccess(t *testing.T) {
	m := newCtrlFeedsModel()
	seedDeletableFeed(m, 1, 0)
	pub := &recordingPublisher{sendErr: errors.New("rocketmq: broker unavailable")}
	svcCtx := newDeleteTestSvc(t, m, pub)

	l := NewDeleteFeedLogic(context.Background(), svcCtx)
	resp, err := l.DeleteFeed(&feed.DeleteFeedReq{FeedId: int64(delFeedID), UserId: int64(delOwnerID)})

	// 接口当前返回成功
	require.NoError(t, err, "MQ 失败不阻塞主流程（当前行为基线）")
	assert.True(t, resp.Success)

	// MySQL 最终状态：已软删除
	assert.EqualValues(t, 2, m.byID[delFeedID].Status)

	// Producer 恰好 1 次调用：无重试无补偿，消息丢失
	assert.Equal(t, 1, pub.callCount())
	assert.Empty(t, pub.messages(), "feed-deleted 消息永久丢失（行为基线）")
}
