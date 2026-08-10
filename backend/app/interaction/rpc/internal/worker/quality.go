// quality.go
//
// 职责：行为埋点的数据质量校验工具（见 docs/design/agent/03-behavior-event.md §7）。
//
// 校验 5 条规则：
//
//  1. play            ≤ expose
//  2. effective_play  ≤ play
//  3. finish          ≤ play
//  4. skip            ≤ play
//  5. 客户端时间偏差 P99 < 5min
//
// 前 4 条是播放漏斗的单调性约束，在指标落库前检查；第 5 条是客户端时钟健康度，
// 在事件消费时持续采样、随 flush 周期汇报。
//
// 校验只产出告警日志、不阻断落库：埋点数据本身允许少量丢失，
// 若因为质量问题拒绝落库，反而会把「部分不准」放大成「整桶没有」。
package worker

import (
	"fmt"
	"sort"
	"sync"

	"github.com/sponge-dad/feed/app/interaction/model"
)

// 漏斗校验容差。
//
// 为什么必须留容差：小时桶按事件时间分桶，一次浏览可能跨桶——用户 10:59 曝光、
// 11:00 才播放，则 11 点桶出现 play=1、expose=0 的「倒挂」。这属正常现象，
// 若零容差会在每个整点前后刷屏告警，淹没真正的埋点故障。
//
// 绝对容差覆盖小流量桶（分母小，跨桶波动占比高），相对容差覆盖大流量桶，
// 取两者较大值。
const (
	qualityAbsTolerance   = 5    // 允许的倒挂条数下限
	qualityRatioTolerance = 0.10 // 允许的倒挂比例
)

// QualityIssue 一条数据质量异常。
type QualityIssue struct {
	Rule   string // 被违反的规则，如 "play<=expose"
	Actual int64  // 实际值（漏斗下游计数）
	Parent int64  // 参照值（漏斗上游计数）
}

func (q QualityIssue) String() string {
	return fmt.Sprintf("%s violated (actual=%d parent=%d)", q.Rule, q.Actual, q.Parent)
}

// CheckMetricsRow 校验一个小时桶的漏斗单调性，返回全部异常项（无异常返回 nil）。
//
// 只校验播放漏斗字段；like/collect/comment 记录的是小时内净增减，
// 取消操作会让其为负数，不参与单调性校验。
func CheckMetricsRow(row *model.FeedMetricsHourly) []QualityIssue {
	if row == nil {
		return nil
	}

	checks := []struct {
		rule   string
		actual int64
		parent int64
	}{
		{"play<=expose", row.PlayCount, row.ExposeCount},
		{"effective_play<=play", row.EffectivePlayCount, row.PlayCount},
		{"finish<=play", row.FinishCount, row.PlayCount},
		{"skip<=play", row.SkipCount, row.PlayCount},
	}

	var issues []QualityIssue
	for _, c := range checks {
		if exceedsFunnel(c.actual, c.parent) {
			issues = append(issues, QualityIssue{Rule: c.rule, Actual: c.actual, Parent: c.parent})
		}
	}
	return issues
}

// exceedsFunnel 判断下游计数是否超出上游计数（含容差）。
func exceedsFunnel(actual, parent int64) bool {
	tolerance := int64(float64(parent) * qualityRatioTolerance)
	if tolerance < qualityAbsTolerance {
		tolerance = qualityAbsTolerance
	}
	return actual > parent+tolerance
}

// ---------- 客户端时钟偏差 ----------

const (
	// skewSampleCapacity 环形缓冲容量。固定容量而非无限增长，
	// 保证内存恒定，且 P99 始终反映「最近」的时钟健康度而非历史累积。
	skewSampleCapacity = 1024

	// skewP99ThresholdMs 客户端时间偏差 P99 告警阈值（5 分钟）。
	skewP99ThresholdMs = 5 * 60 * 1000

	// skewMinSamples 样本太少时 P99 无统计意义，不做判定。
	skewMinSamples = 100
)

// clockSkewTracker 采样「客户端上报时间 vs 服务端接收时间」的偏差绝对值。
//
// 偏差过大意味着客户端时钟不可信，此时依赖 timestamp 的分桶、去重都会失真，
// 需要及时发现（服务端已用 server_time 兜底分桶，但仍需监控客户端质量）。
type clockSkewTracker struct {
	mu      sync.Mutex
	samples []int64
	idx     int
	full    bool
}

func newClockSkewTracker() *clockSkewTracker {
	return &clockSkewTracker{samples: make([]int64, skewSampleCapacity)}
}

// Observe 记录一次偏差（毫秒，取绝对值）。
func (t *clockSkewTracker) Observe(skewMs int64) {
	if t == nil {
		return
	}
	if skewMs < 0 {
		skewMs = -skewMs
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	t.samples[t.idx] = skewMs
	t.idx++
	if t.idx == len(t.samples) {
		t.idx = 0
		t.full = true
	}
}

// P99 返回当前窗口的 P99 偏差与样本数。样本为空时返回 (0, 0)。
func (t *clockSkewTracker) P99() (int64, int) {
	if t == nil {
		return 0, 0
	}

	t.mu.Lock()
	n := t.idx
	if t.full {
		n = len(t.samples)
	}
	if n == 0 {
		t.mu.Unlock()
		return 0, 0
	}
	snapshot := make([]int64, n)
	copy(snapshot, t.samples[:n])
	t.mu.Unlock()

	sort.Slice(snapshot, func(i, j int) bool { return snapshot[i] < snapshot[j] })
	// 向上取整到第 99 百分位，n=1 时即取唯一样本
	rank := (n*99 + 99) / 100
	if rank > n {
		rank = n
	}
	return snapshot[rank-1], n
}

// Unhealthy 判断客户端时钟质量是否超阈值，返回 (是否异常, p99, 样本数)。
func (t *clockSkewTracker) Unhealthy() (bool, int64, int) {
	p99, n := t.P99()
	if n < skewMinSamples {
		return false, p99, n
	}
	return p99 > skewP99ThresholdMs, p99, n
}
