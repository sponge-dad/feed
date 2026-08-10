// behaviorWorker_test.go
//
// 职责：Feed 行为事件消费者的纯逻辑单元测试。
// 覆盖：服务端行为重判（含客户端谎报场景）、脏集合成员解析、
// 行为类型到小时桶字段的映射、曝光采样策略。
//
// 这几个函数不依赖 Redis / MySQL / MQ，可直接构造 worker 断言，
// 消费链路的集成行为由 tests/ 下的集成用例覆盖。
package worker

import (
	"testing"
	"time"

	"github.com/sponge-dad/feed/app/interaction/rpc/internal/config"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/keys"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"
	bhv "github.com/sponge-dad/feed/common/event/behavior"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestBehaviorWorker 构造只填充判定阈值的 worker（默认口径：3s / 0.5 / 0.95 / 3s）。
func newTestBehaviorWorker() *BehaviorWorker {
	rule := config.BehaviorRule{}
	rule.Fill()
	return &BehaviorWorker{rule: rule}
}

func TestRejudge(t *testing.T) {
	bw := newTestBehaviorWorker()

	tests := []struct {
		name     string
		declared string
		watchMs  int64
		mediaMs  int64
		want     string
		wantKeep bool
	}{
		// 无时长口径的三类不参与重判
		{"曝光原样保留", bhv.ActionExpose, 0, 0, bhv.ActionExpose, true},
		{"起播原样保留", bhv.ActionPlay, 0, 4000, bhv.ActionPlay, true},
		{"分享原样保留", bhv.ActionShare, 0, 4000, bhv.ActionShare, true},

		// 有效播放：绝对阈值 3000ms 或 比例阈值 0.5
		{"短视频按比例判有效播放", bhv.ActionEffectivePlay, 2900, 4000, bhv.ActionEffectivePlay, true},
		{"长视频按绝对阈值判有效播放", bhv.ActionEffectivePlay, 3000, 30000, bhv.ActionEffectivePlay, true},
		{"媒体时长未知时退化为绝对阈值", bhv.ActionEffectivePlay, 5000, 0, bhv.ActionEffectivePlay, true},

		// 完播：>= 0.95 * media
		{"达到完播比例判完播", bhv.ActionEffectivePlay, 3900, 4000, bhv.ActionFinish, true},
		{"恰好等于完播阈值判完播", bhv.ActionEffectivePlay, 3800, 4000, bhv.ActionFinish, true},

		// 快划：两个阈值都不满足
		{"长视频看两秒判快划", bhv.ActionEffectivePlay, 2000, 30000, bhv.ActionSkip, true},

		// 关键：客户端结论一律不作准
		{"客户端谎报完播被纠正为快划", bhv.ActionFinish, 100, 60000, bhv.ActionSkip, true},
		{"客户端谎报快划被纠正为完播", bhv.ActionSkip, 4000, 4000, bhv.ActionFinish, true},

		// SKIP 必须携带真实 watch_duration_ms，否则丢弃
		{"零时长快划被丢弃", bhv.ActionSkip, 0, 30000, bhv.ActionSkip, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := &bhv.FeedBehaviorEvent{
				ActionType:      tt.declared,
				WatchDurationMs: tt.watchMs,
				MediaDurationMs: tt.mediaMs,
			}
			got, keep := bw.rejudge(ev)
			assert.Equal(t, tt.wantKeep, keep, "保留决策不符")
			if tt.wantKeep {
				assert.Equal(t, tt.want, got, "重判结果不符")
			}
		})
	}
}

// TestRejudgeUsesConfiguredThresholds 阈值来自配置而非硬编码，
// 保证口径调整无需发版改代码。
func TestRejudgeUsesConfiguredThresholds(t *testing.T) {
	bw := &BehaviorWorker{rule: config.BehaviorRule{
		EffectivePlayMs:    10000,
		EffectivePlayRatio: 0.9,
		FinishRatio:        0.99,
		SkipMs:             10000,
	}}

	// 在默认口径下 3000ms/4000ms 会判有效播放，收紧后应判快划
	ev := &bhv.FeedBehaviorEvent{
		ActionType:      bhv.ActionEffectivePlay,
		WatchDurationMs: 3000,
		MediaDurationMs: 4000,
	}
	got, keep := bw.rejudge(ev)
	require.True(t, keep)
	assert.Equal(t, bhv.ActionSkip, got)
}

// TestBehaviorRuleFillDefaults 零值配置必须回落到默认阈值，
// 否则 0 阈值会让所有事件都命中完播。
func TestBehaviorRuleFillDefaults(t *testing.T) {
	var r config.BehaviorRule
	r.Fill()

	assert.Equal(t, int64(3000), r.EffectivePlayMs)
	assert.Equal(t, 0.5, r.EffectivePlayRatio)
	assert.Equal(t, 0.95, r.FinishRatio)
	assert.Equal(t, int64(3000), r.SkipMs)
}

func TestParseDirtyMember(t *testing.T) {
	t.Run("正常解析", func(t *testing.T) {
		feedID, statHour, ok := parseDirtyMember("12345:2026080714")
		require.True(t, ok)
		assert.Equal(t, int64(12345), feedID)

		want := time.Date(2026, 8, 7, 14, 0, 0, 0, time.Local)
		assert.True(t, want.Equal(statHour), "期望 %v，实际 %v", want, statHour)
	})

	bad := []struct {
		name   string
		member string
	}{
		{"空串", ""},
		{"无分隔符", "12345"},
		{"缺少 feed_id", ":2026080714"},
		{"feed_id 非数字", "abc:2026080714"},
		{"feed_id 为零", "0:2026080714"},
		{"小时格式非法", "12345:not-an-hour"},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			_, _, ok := parseDirtyMember(tt.member)
			assert.False(t, ok)
		})
	}
}

func TestBehaviorField(t *testing.T) {
	mapping := map[string]string{
		bhv.ActionExpose:        keys.FieldExpose,
		bhv.ActionPlay:          keys.FieldPlay,
		bhv.ActionEffectivePlay: keys.FieldEffectivePlay,
		bhv.ActionFinish:        keys.FieldFinish,
		bhv.ActionSkip:          keys.FieldSkip,
		bhv.ActionShare:         keys.FieldShare,
	}
	for action, want := range mapping {
		got, ok := behaviorField(action)
		require.True(t, ok, "行为 %s 应有对应字段", action)
		assert.Equal(t, want, got)
	}

	_, ok := behaviorField("NOT_AN_ACTION")
	assert.False(t, ok)
}

func TestShouldPersist(t *testing.T) {
	newBW := func(rate float64) *BehaviorWorker {
		bw := newTestBehaviorWorker()
		bw.svcCtx = &svc.ServiceContext{}
		bw.svcCtx.Config.Behavior.ExposeSampleRate = rate
		return bw
	}

	t.Run("非曝光行为全量落库", func(t *testing.T) {
		bw := newBW(0) // 即使曝光采样率为 0
		for _, a := range []string{bhv.ActionPlay, bhv.ActionEffectivePlay, bhv.ActionFinish, bhv.ActionSkip, bhv.ActionShare} {
			assert.True(t, bw.shouldPersist(a), "行为 %s 应全量落库", a)
		}
	})

	t.Run("采样率为零时曝光不落库", func(t *testing.T) {
		assert.False(t, newBW(0).shouldPersist(bhv.ActionExpose))
	})

	t.Run("采样率为一时曝光全量落库", func(t *testing.T) {
		assert.True(t, newBW(1).shouldPersist(bhv.ActionExpose))
	})
}

// TestEventTimePrefersServerTime 服务端接收时间优先于客户端时间，
// 避免客户端时钟漂移把事件算进错误的小时桶。
func TestEventTimePrefersServerTime(t *testing.T) {
	serverMs := time.Date(2026, 8, 7, 14, 30, 0, 0, time.Local).UnixMilli()
	clientMs := time.Date(2026, 8, 7, 9, 30, 0, 0, time.Local).UnixMilli()

	got := eventTime(&bhv.FeedBehaviorEvent{ServerTime: serverMs, Timestamp: clientMs})
	assert.Equal(t, serverMs, got.UnixMilli())

	// 无服务端时间时退回客户端时间
	got = eventTime(&bhv.FeedBehaviorEvent{Timestamp: clientMs})
	assert.Equal(t, clientMs, got.UnixMilli())
}

func TestIsDuplicateErr(t *testing.T) {
	assert.False(t, isDuplicateErr(nil))
	assert.True(t, isDuplicateErr(errString("Error 1062: Duplicate entry 'x' for key 'uk_event_id'")))
	assert.False(t, isDuplicateErr(errString("connection refused")))
}

type errString string

func (e errString) Error() string { return string(e) }
