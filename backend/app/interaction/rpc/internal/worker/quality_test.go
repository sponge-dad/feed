// quality_test.go
//
// 职责：数据质量校验规则的单元测试。
// 覆盖漏斗单调性（含跨小时桶容差）与客户端时钟偏差 P99 判定。
package worker

import (
	"testing"

	"github.com/sponge-dad/feed/app/interaction/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckMetricsRowHealthy(t *testing.T) {
	row := &model.FeedMetricsHourly{
		ExposeCount:        1000,
		PlayCount:          800,
		EffectivePlayCount: 500,
		FinishCount:        200,
		SkipCount:          300,
	}
	assert.Empty(t, CheckMetricsRow(row))
}

func TestCheckMetricsRowNil(t *testing.T) {
	assert.Nil(t, CheckMetricsRow(nil))
}

// TestCheckMetricsRowTolerance 跨小时桶造成的小幅倒挂不应告警。
func TestCheckMetricsRowTolerance(t *testing.T) {
	// 小流量桶：play 比 expose 多 5，落在绝对容差内
	assert.Empty(t, CheckMetricsRow(&model.FeedMetricsHourly{ExposeCount: 3, PlayCount: 8}))

	// 大流量桶：play 比 expose 多 10%，落在相对容差内
	assert.Empty(t, CheckMetricsRow(&model.FeedMetricsHourly{ExposeCount: 1000, PlayCount: 1100}))
}

func TestCheckMetricsRowViolations(t *testing.T) {
	tests := []struct {
		name string
		row  *model.FeedMetricsHourly
		rule string
	}{
		{
			name: "play 超过 expose",
			row:  &model.FeedMetricsHourly{ExposeCount: 100, PlayCount: 500},
			rule: "play<=expose",
		},
		{
			name: "effective_play 超过 play",
			row:  &model.FeedMetricsHourly{ExposeCount: 1000, PlayCount: 100, EffectivePlayCount: 500},
			rule: "effective_play<=play",
		},
		{
			name: "finish 超过 play",
			row:  &model.FeedMetricsHourly{ExposeCount: 1000, PlayCount: 100, FinishCount: 500},
			rule: "finish<=play",
		},
		{
			name: "skip 超过 play",
			row:  &model.FeedMetricsHourly{ExposeCount: 1000, PlayCount: 100, SkipCount: 500},
			rule: "skip<=play",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issues := CheckMetricsRow(tt.row)
			require.Len(t, issues, 1)
			assert.Equal(t, tt.rule, issues[0].Rule)
			assert.Contains(t, issues[0].String(), tt.rule)
		})
	}
}

// TestCheckMetricsRowIgnoresNetCounters like/collect/comment 记录净增减，
// 允许为负，不应被判定为质量问题。
func TestCheckMetricsRowIgnoresNetCounters(t *testing.T) {
	row := &model.FeedMetricsHourly{
		ExposeCount:  100,
		PlayCount:    50,
		LikeCount:    -3,
		CollectCount: -1,
		CommentCount: -2,
	}
	assert.Empty(t, CheckMetricsRow(row))
}

func TestClockSkewTrackerEmpty(t *testing.T) {
	tr := newClockSkewTracker()
	p99, n := tr.P99()
	assert.Zero(t, p99)
	assert.Zero(t, n)

	unhealthy, _, _ := tr.Unhealthy()
	assert.False(t, unhealthy)
}

// TestClockSkewTrackerAbs 偏差取绝对值：客户端时钟走快走慢都算异常。
func TestClockSkewTrackerAbs(t *testing.T) {
	tr := newClockSkewTracker()
	tr.Observe(-1000)
	p99, n := tr.P99()
	assert.EqualValues(t, 1000, p99)
	assert.Equal(t, 1, n)
}

// TestClockSkewTrackerMinSamples 样本不足时不做判定，避免个别异常客户端触发误报。
func TestClockSkewTrackerMinSamples(t *testing.T) {
	tr := newClockSkewTracker()
	for i := 0; i < skewMinSamples-1; i++ {
		tr.Observe(skewP99ThresholdMs * 10)
	}
	unhealthy, _, n := tr.Unhealthy()
	assert.False(t, unhealthy)
	assert.Equal(t, skewMinSamples-1, n)
}

func TestClockSkewTrackerUnhealthy(t *testing.T) {
	tr := newClockSkewTracker()
	for i := 0; i < skewMinSamples; i++ {
		tr.Observe(skewP99ThresholdMs + 1)
	}
	unhealthy, p99, n := tr.Unhealthy()
	assert.True(t, unhealthy)
	assert.EqualValues(t, skewP99ThresholdMs+1, p99)
	assert.Equal(t, skewMinSamples, n)
}

// TestClockSkewTrackerP99IgnoresTail 1% 的极端值不应把 P99 拉高。
func TestClockSkewTrackerP99IgnoresTail(t *testing.T) {
	tr := newClockSkewTracker()
	for i := 0; i < 990; i++ {
		tr.Observe(100)
	}
	for i := 0; i < 9; i++ {
		tr.Observe(skewP99ThresholdMs * 100)
	}

	p99, n := tr.P99()
	assert.Equal(t, 999, n)
	assert.EqualValues(t, 100, p99, "P99 应落在正常样本内")

	unhealthy, _, _ := tr.Unhealthy()
	assert.False(t, unhealthy)
}

// TestClockSkewTrackerRingBuffer 超出容量后覆盖最旧样本，
// 保证 P99 反映最近窗口而非历史累积。
func TestClockSkewTrackerRingBuffer(t *testing.T) {
	tr := newClockSkewTracker()
	for i := 0; i < skewSampleCapacity; i++ {
		tr.Observe(skewP99ThresholdMs * 10)
	}
	unhealthy, _, n := tr.Unhealthy()
	require.True(t, unhealthy)
	assert.Equal(t, skewSampleCapacity, n)

	// 写满一整轮正常样本后应恢复健康
	for i := 0; i < skewSampleCapacity; i++ {
		tr.Observe(50)
	}
	unhealthy, p99, n := tr.Unhealthy()
	assert.False(t, unhealthy)
	assert.EqualValues(t, 50, p99)
	assert.Equal(t, skewSampleCapacity, n)
}
