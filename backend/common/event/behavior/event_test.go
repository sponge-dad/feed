// event_test.go
//
// 职责：Feed 行为事件契约单元测试。
// 覆盖：行为白名单、服务端生成 event_id、字段边界校验、序列化往返、Redis 键格式。
package behavior

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsValidAction(t *testing.T) {
	for _, a := range []string{ActionExpose, ActionPlay, ActionEffectivePlay, ActionFinish, ActionSkip, ActionShare} {
		assert.True(t, IsValidAction(a), "%s 应为合法行为", a)
	}
	for _, a := range []string{"", "LIKE", "COLLECT", "COMMENT", "expose", "FINISHED"} {
		assert.False(t, IsValidAction(a), "%q 不应为合法行为", a)
	}
}

// TestNewEventGeneratesServerSideID event_id 必须由服务端生成且互不相同。
// 若改用客户端传入的 ID 作幂等键，攻击者可预先占位把他人真实事件吞掉。
func TestNewEventGeneratesServerSideID(t *testing.T) {
	seen := make(map[string]struct{}, 100)
	for i := 0; i < 100; i++ {
		ev := NewEvent("req-1", 10, 20, 30, ActionPlay, 0, 0, 0, 1, 1, "client-fixed-id")
		require.NotEmpty(t, ev.EventID)
		assert.NotEqual(t, "client-fixed-id", ev.EventID, "不得采用客户端 ID")

		_, dup := seen[ev.EventID]
		require.False(t, dup, "event_id 出现重复：%s", ev.EventID)
		seen[ev.EventID] = struct{}{}
	}
}

func TestNewEventFields(t *testing.T) {
	ev := NewEvent("req-9", 11, 22, 33, ActionFinish, 7, 4000, 4200, 1700000000000, 1700000000123, "cli-1")

	assert.Equal(t, "req-9", ev.RequestID)
	assert.Equal(t, int64(11), ev.UserID)
	assert.Equal(t, int64(22), ev.FeedID)
	assert.Equal(t, int64(33), ev.AuthorID)
	assert.Equal(t, ActionFinish, ev.ActionType)
	assert.Equal(t, int32(7), ev.Position)
	assert.Equal(t, int64(4000), ev.WatchDurationMs)
	assert.Equal(t, int64(4200), ev.MediaDurationMs)
	assert.Equal(t, int64(1700000000000), ev.Timestamp)
	assert.Equal(t, int64(1700000000123), ev.ServerTime)
	assert.Equal(t, "cli-1", ev.ClientEventID)
}

func TestValidate(t *testing.T) {
	valid := func() *FeedBehaviorEvent {
		return NewEvent("req-1", 1, 2, 3, ActionPlay, 0, 100, 1000, 1, 1, "")
	}

	t.Run("合法事件通过", func(t *testing.T) {
		require.NoError(t, valid().Validate())
	})

	tests := []struct {
		name   string
		mutate func(*FeedBehaviorEvent)
		want   error
	}{
		{"event_id 为空", func(e *FeedBehaviorEvent) { e.EventID = "" }, ErrEventIDEmpty},
		{"user_id 为零", func(e *FeedBehaviorEvent) { e.UserID = 0 }, ErrUserIDZero},
		{"user_id 为负", func(e *FeedBehaviorEvent) { e.UserID = -1 }, ErrUserIDZero},
		{"feed_id 为零", func(e *FeedBehaviorEvent) { e.FeedID = 0 }, ErrFeedIDZero},
		{"position 为负", func(e *FeedBehaviorEvent) { e.Position = -1 }, ErrPositionRange},
		{"position 超上限", func(e *FeedBehaviorEvent) { e.Position = MaxPosition + 1 }, ErrPositionRange},
		{"观看时长为负", func(e *FeedBehaviorEvent) { e.WatchDurationMs = -1 }, ErrWatchRange},
		{"观看时长超上限", func(e *FeedBehaviorEvent) { e.WatchDurationMs = MaxWatchDurationMs + 1 }, ErrWatchRange},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := valid()
			tt.mutate(ev)
			assert.ErrorIs(t, ev.Validate(), tt.want)
		})
	}

	t.Run("行为类型非法", func(t *testing.T) {
		ev := valid()
		ev.ActionType = "LIKE"
		assert.ErrorIs(t, ev.Validate(), ErrActionUnknown)
	})
}

func TestJSONRoundTrip(t *testing.T) {
	ev := NewEvent("req-2", 5, 6, 7, ActionEffectivePlay, 3, 3500, 5000, 111, 222, "cli")

	body, err := ev.ToJSON()
	require.NoError(t, err)

	got, err := FromJSON(body)
	require.NoError(t, err)
	assert.Equal(t, ev, got)
}

func TestFromJSONRejectsBadPayload(t *testing.T) {
	t.Run("非法 JSON", func(t *testing.T) {
		_, err := FromJSON([]byte("{not json"))
		assert.ErrorIs(t, err, ErrEventInvalid)
	})

	t.Run("字段不合法", func(t *testing.T) {
		// user_id 缺失 → 校验失败，避免脏事件进入统计
		_, err := FromJSON([]byte(`{"event_id":"e1","feed_id":2,"action_type":"PLAY"}`))
		assert.ErrorIs(t, err, ErrUserIDZero)
	})
}

func TestKeys(t *testing.T) {
	ev := NewEvent("req-abc", 1, 42, 3, ActionExpose, 0, 0, 0, 1, 1, "")
	ev.EventID = "evt-1"

	assert.Equal(t, "behavior_event:evt-1", ev.IdemKey())
	assert.Equal(t, "behavior:expose:req-abc:42", ev.ExposeDedupKey())
}

// TestExposeDedupKeyWithoutRequestID 空 request_id 时去重键必须按事件唯一，
// 不能退化为 {feed_id} 维度——否则不同用户/不同请求的曝光会互相去重。
func TestExposeDedupKeyWithoutRequestID(t *testing.T) {
	first := NewEvent("", 1, 42, 3, ActionExpose, 0, 0, 0, 1, 1, "")
	first.EventID = "evt-1"
	second := NewEvent("", 2, 42, 3, ActionExpose, 0, 0, 0, 1, 1, "")
	second.EventID = "evt-2"

	assert.NotEqual(t, first.ExposeDedupKey(), second.ExposeDedupKey(),
		"空 request_id 时不同事件的去重键必须互不相同")
	assert.Equal(t, "behavior:expose:evt:evt-1", first.ExposeDedupKey())
}
