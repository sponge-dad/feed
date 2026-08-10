// reportBehaviors_test.go
//
// 职责：行为埋点上报的网关侧字段校验单元测试。
// 覆盖：action_type 白名单、时钟偏差、position/时长边界、request_id 字符集与长度。
package feed

import (
	"strings"
	"testing"
	"time"

	"github.com/sponge-dad/feed/app/gateway/internal/types"
	bhv "github.com/sponge-dad/feed/common/event/behavior"

	"github.com/stretchr/testify/assert"
)

func TestValidateItem(t *testing.T) {
	nowMs := time.Now().UnixMilli()

	valid := func() *types.BehaviorItem {
		return &types.BehaviorItem{
			RequestId:       "req-abc_123",
			FeedId:          1001,
			ActionType:      bhv.ActionEffectivePlay,
			Position:        3,
			WatchDurationMs: 3500,
			MediaDurationMs: 5000,
			Timestamp:       nowMs,
		}
	}

	t.Run("合法事件通过", func(t *testing.T) {
		assert.Equal(t, "", validateItem(valid(), nowMs))
	})

	t.Run("request_id 可缺省", func(t *testing.T) {
		it := valid()
		it.RequestId = ""
		assert.Equal(t, "", validateItem(it, nowMs))
	})

	tests := []struct {
		name   string
		mutate func(*types.BehaviorItem)
		want   string
	}{
		{"feed_id 为零", func(i *types.BehaviorItem) { i.FeedId = 0 }, reasonFeedInvalid},
		{"feed_id 为负", func(i *types.BehaviorItem) { i.FeedId = -1 }, reasonFeedInvalid},
		{"行为类型未知", func(i *types.BehaviorItem) { i.ActionType = "SCROLL" }, reasonActionInvalid},
		{"行为类型为互动事件", func(i *types.BehaviorItem) { i.ActionType = "LIKE" }, reasonActionInvalid},
		{"行为类型大小写不符", func(i *types.BehaviorItem) { i.ActionType = "play" }, reasonActionInvalid},

		{"时间戳超前一小时以上", func(i *types.BehaviorItem) { i.Timestamp = nowMs + 2*bhv.MaxClockSkewMs }, reasonTimestampRange},
		{"时间戳滞后一小时以上", func(i *types.BehaviorItem) { i.Timestamp = nowMs - 2*bhv.MaxClockSkewMs }, reasonTimestampRange},

		{"position 为负", func(i *types.BehaviorItem) { i.Position = -1 }, reasonPositionRange},
		{"position 超上限", func(i *types.BehaviorItem) { i.Position = bhv.MaxPosition + 1 }, reasonPositionRange},

		{"观看时长为负", func(i *types.BehaviorItem) { i.WatchDurationMs = -1 }, reasonWatchRange},
		{"观看时长超一天", func(i *types.BehaviorItem) { i.WatchDurationMs = bhv.MaxWatchDurationMs + 1 }, reasonWatchRange},
		{"媒体时长为负", func(i *types.BehaviorItem) { i.MediaDurationMs = -1 }, reasonMediaRange},
		{"媒体时长超一天", func(i *types.BehaviorItem) { i.MediaDurationMs = bhv.MaxWatchDurationMs + 1 }, reasonMediaRange},

		{"request_id 含冒号", func(i *types.BehaviorItem) { i.RequestId = "req:evil" }, reasonRequestIDInvalid},
		{"request_id 过长", func(i *types.BehaviorItem) { i.RequestId = strings.Repeat("a", 65) }, reasonRequestIDInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			it := valid()
			tt.mutate(it)
			assert.Equal(t, tt.want, validateItem(it, nowMs))
		})
	}
}

// TestIsSafeRequestID request_id 会拼进 Redis 键 behavior:expose:{request_id}:{feed_id}，
// 必须限长防打爆内存、限字符集防伪造出其他事件的去重键。
func TestIsSafeRequestID(t *testing.T) {
	ok := []string{"", "abc", "ABC-123_xyz", strings.Repeat("a", 64)}
	for _, s := range ok {
		assert.True(t, isSafeRequestID(s), "%q 应通过", s)
	}

	bad := []string{
		"req:evil",              // 冒号可伪造去重键
		"req evil",              // 空格
		"req/evil",              // 斜杠
		"req*",                  // 通配符可影响 KEYS/SCAN 类操作
		"req\n",                 // 换行
		"中文",                    // 非 ASCII
		strings.Repeat("a", 65), // 超长
	}
	for _, s := range bad {
		assert.False(t, isSafeRequestID(s), "%q 应被拒绝", s)
	}
}
