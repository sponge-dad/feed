// Package interest 用户兴趣画像（US5）：
//
// 行为 → 兴趣分数（ZSet user:interest:{user_id}），规则权重 + 时间衰减（不训练模型），
// 见 docs/design/agent/06-user-interest.md。
//
//   - ApplyEvent：行为事件累加（同 feed 同行为 SETNX 去重 24h；EXPOSE 权重 0 不入画像）
//   - Decay：每日半衰期 14 天指数衰减（daily_factor ≈ 0.9517）
//   - BuildSnapshot：从 ZSet 生成快照（供 MySQL 落盘与查询兜底）
//
// 标签来源：先读 Redis content:profile:{feed_id} 缓存，未命中调 Content RPC
// BatchGetContentProfile（仅取 COMPLETED 画像，缺失/未完成直接跳过，不猜测标签）。
package interest

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	contentpb "github.com/sponge-dad/feed/app/content/rpc/content"
	contentClient "github.com/sponge-dad/feed/app/content/rpc/contentClient"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

// ---- Key 约定（06-user-interest.md §4）----

// InterestKey 兴趣 ZSet key：user:interest:{user_id}。
func InterestKey(userID int64) string {
	return "user:interest:" + strconv.FormatInt(userID, 10)
}

// ActiveKey 当日活跃用户集合 key：interest:active:{yyyyMMdd}（TTL 7 天）。
func ActiveKey(t time.Time) string {
	return "interest:active:" + t.Format("20060102")
}

// DedupKey 同 feed 同行为去重 key：interest:dedup:{uid}:{feed}:{action}（TTL 24h）。
func DedupKey(userID, feedID int64, action string) string {
	return "interest:dedup:" + strconv.FormatInt(userID, 10) + ":" +
		strconv.FormatInt(feedID, 10) + ":" + action
}

// ProfileCacheKey 复用内容画像公开缓存 key（content:profile:{feed_id}）。
func ProfileCacheKey(feedID int64) string {
	return "content:profile:" + strconv.FormatInt(feedID, 10)
}

// ---- 行为权重（06-user-interest.md §2）----

// ActionWeights 行为 → 兴趣分数增量；EXPOSE 权重 0（不入画像）。
var ActionWeights = map[string]int64{
	"FINISH":          5,
	"COLLECT":         4,
	"LIKE":            3,
	"CREATE":          3,
	"SHARE":           3,
	"EFFECTIVE_PLAY":  2,
	"SKIP":            -2,
	"UNCOLLECT":       -3,
	"UNLIKE":          -2,
	// EXPOSE 0：不入画像，仅统计曝光基数
}

// ---- 常量 ----

const (
	// dedupTTL 去重 key 保留时长（24h）。
	dedupTTL = 24 * 3600
	// zsetTTL 兴趣 ZSet 保留时长（90 天，每次更新刷新）。
	zsetTTL = 90 * 24 * 3600
	// activeTTL 活跃集合保留时长（7 天）。
	activeTTL = 7 * 24 * 3600
	// maxTopicsPerFeed 每 feed 入画像的标签上限（取前 5）。
	maxTopicsPerFeed = 5
	// maxMembersTop 兴趣 ZSet Top 200 裁剪。
	maxMembersTop = 200
	// lowScoreTrim 低分清理阈值。
	lowScoreTrim = 0.1
	// dailyFactor 14 天半衰期日衰减因子 = 0.5^(1/14)。
	dailyFactor = 0.9517
	// SnapshotWindowDays 快照统计窗口（天）。
	SnapshotWindowDays = 30
)

// contentProfileCache 内容画像公开字段（只解析兴趣需要的字段）。
type contentProfileCache struct {
	Category string   `json:"category"`
	Topics   []string `json:"topics,omitempty"`
}

// ApplyEvent 行为事件累加兴趣（幂等：同 feed 同行为 24h 内只计一次）。
//
// 返回 nil 表示「已处理或跳过」；返回 error 表示处理失败（调用方决定是否重投）。
func ApplyEvent(ctx context.Context, rds *redis.Redis, contentRpc contentClient.Content,
	userID, feedID int64, action string) error {

	weight, ok := ActionWeights[action]
	if !ok || weight == 0 {
		return nil // 未知行为 / EXPOSE（0）不入画像
	}
	// 去重：同 feed 同行为 24h 只计一次。
	dedup := DedupKey(userID, feedID, action)
	added, err := rds.SetnxEx(dedup, "1", dedupTTL)
	if err != nil {
		return err
	}
	if !added {
		return nil
	}

	// 取内容画像标签（缓存优先 → RPC 兜底）；缺失/未完成直接跳过，不猜测标签。
	profile, err := fetchProfile(ctx, rds, contentRpc, feedID)
	if err != nil {
		// 保留去重 key：RPC 失败不重试累加（下次行为事件再试）。
		return nil
	}
	if profile == nil || profile.Category == "" {
		return nil
	}

	// 构造 ZINCRBY 参数：member/delta 对。
	args := make([]string, 0, 2+maxTopicsPerFeed*2)
	args = append(args, "c:"+profile.Category, strconv.FormatInt(weight, 10))
	for i, tp := range profile.Topics {
		if i >= maxTopicsPerFeed {
			break
		}
		args = append(args, "t:"+tp, strconv.FormatInt(weight, 10))
	}
	if err := incrScript(ctx, rds, userID, args); err != nil {
		return err
	}
	return nil
}

// fetchProfile 读取画像标签：先读 Redis 缓存，未命中调 Content RPC（BatchGetContentProfile）。
func fetchProfile(ctx context.Context, rds *redis.Redis, contentRpc contentClient.Content,
	feedID int64) (*contentProfileCache, error) {

	if val, err := rds.Get(ProfileCacheKey(feedID)); err == nil && val != "" {
		var c contentProfileCache
		if err := json.Unmarshal([]byte(val), &c); err == nil {
			return &c, nil
		}
	}
	resp, err := contentRpc.BatchGetContentProfile(ctx, &contentpb.BatchGetContentProfileReq{
		FeedIds: []int64{feedID},
	})
	if err != nil {
		logx.WithContext(ctx).Errorf("interest fetch profile failed feed_id=%d err=%v", feedID, err)
		return nil, err
	}
	if resp == nil || len(resp.Profiles) == 0 {
		return nil, nil
	}
	p := resp.Profiles[0]
	return &contentProfileCache{Category: p.Category, Topics: p.Topics}, nil
}

// incrScriptLua ZINCRBY + 负分 clamp 0 + EXPIRE + SADD 活跃集合 + Top 200 裁剪 + 低分清理（原子）。
//
//	KEYS[1] = user:interest:{uid}
//	KEYS[2] = interest:active:{yyyyMMdd}
//	ARGV[1] = user_id
//	ARGV[2] = TTL
//	ARGV[3..] = member/delta 对
var incrScriptLua = fmt.Sprintf(`
local zkey = KEYS[1]
local akey = KEYS[2]
local uid = ARGV[1]
local ttl = tonumber(ARGV[2])
local i = 3
while i <= #ARGV do
  local member = ARGV[i]
  local delta = tonumber(ARGV[i + 1])
  local s = redis.call('ZINCRBY', zkey, delta, member)
  if s < 0 then
    redis.call('ZADD', zkey, 0, member)
  end
  i = i + 2
end
redis.call('SADD', akey, uid)
redis.call('EXPIRE', akey, tonumber(ARGV[#ARGV - 1]))
redis.call('EXPIRE', zkey, ttl)
redis.call('ZREMRANGEBYRANK', zkey, 0, -%d)
redis.call('ZREMRANGEBYSCORE', zkey, '-inf', %v)
return 1
`, maxMembersTop+1, lowScoreTrim)

func incrScript(ctx context.Context, rds *redis.Redis, userID int64, members []string) error {
	args := append([]string{strconv.FormatInt(userID, 10), strconv.Itoa(zsetTTL)}, members...)
	args = append(args, strconv.Itoa(activeTTL))
	_, err := rds.EvalCtx(ctx, incrScriptLua, []string{InterestKey(userID), ActiveKey(time.Now())}, args)
	return err
}

// decayScript 时间衰减：全部成员乘以 daily_factor，清理低分成员。
//
//	KEYS[1] = user:interest:{uid}
//	ARGV[1] = daily_factor
var decayScript = fmt.Sprintf(`
local zkey = KEYS[1]
local factor = tonumber(ARGV[1])
local members = redis.call('ZRANGE', zkey, 0, -1)
for _, m in ipairs(members) do
  local s = redis.call('ZSCORE', zkey, m)
  redis.call('ZADD', zkey, s * factor, m)
end
redis.call('ZREMRANGEBYSCORE', zkey, '-inf', %v)
return #members
`, lowScoreTrim)

// Decay 对单个用户执行时间衰减（14 天半衰期）。
func Decay(ctx context.Context, rds *redis.Redis, userID int64) error {
	_, err := rds.EvalCtx(ctx, decayScript, []string{InterestKey(userID)}, []string{
		strconv.FormatFloat(dailyFactor, 'f', 4, 64),
	})
	return err
}

// Item 快照中的单条兴趣项。
type Item struct {
	Name  string  `json:"name"`
	Score float64 `json:"score"`
}

// Snapshot 从 ZSet 生成的兴趣快照。
type Snapshot struct {
	Categories   []Item
	Topics       []Item
	TotalActions int64
	CalculatedAt time.Time
}

// BuildSnapshot 从 Redis ZSet 生成快照（categories 按 c: 前缀、topics 按 t: 前缀拆分，按 score 降序）。
// ZRANGE 默认升序，需在应用层降序排列后返回（ZREVRANGE 在多版本 go-zero 上行为不一）。
func BuildSnapshot(ctx context.Context, rds *redis.Redis, userID int64) (*Snapshot, error) {
	key := InterestKey(userID)
	pairs, err := rds.ZrangeWithScoresByFloat(key, 0, -1)
	if err != nil {
		return nil, err
	}
	total, err := rds.Zcard(key)
	if err != nil {
		return nil, err
	}
	snap := &Snapshot{TotalActions: int64(total), CalculatedAt: time.Now()}
	for _, p := range pairs {
		name := p.Key
		switch {
		case len(name) > 2 && name[:2] == "c:":
			snap.Categories = append(snap.Categories, Item{Name: name[2:], Score: p.Score})
		case len(name) > 2 && name[:2] == "t:":
			snap.Topics = append(snap.Topics, Item{Name: name[2:], Score: p.Score})
		}
	}
	sort.Slice(snap.Categories, func(i, j int) bool { return snap.Categories[i].Score > snap.Categories[j].Score })
	sort.Slice(snap.Topics, func(i, j int) bool { return snap.Topics[i].Score > snap.Topics[j].Score })
	return snap, nil
}

// SnapshotJSON 序列化快照为 interest_json（06-user-interest.md §4 结构）。
func (s *Snapshot) SnapshotJSON() string {
	payload := map[string]any{
		"categories":   s.Categories,
		"topics":       s.Topics,
		"total_actions": s.TotalActions,
		"window_days":  SnapshotWindowDays,
	}
	b, _ := json.Marshal(payload)
	return string(b)
}
