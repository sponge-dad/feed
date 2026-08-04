// interactionHelper.go
//
// 职责：点赞 / 收藏两类互动的共享核心逻辑（两者同构，仅 key、计数字段、模型不同）。
// 实现「Redis 先行 + MQ 异步落库」写路径与 Cache-Aside 读路径，
// 设计见 docs/design/interaction/02-like.md、03-collect.md、06-cache.md。
//
// 相对设计文档的加固点（背景见 06-cache.md §4.2 未覆盖的场景）：
//  1. 冷 key 先重建再增量：SADD/ZADD/HINCRBY 直接写不存在的 key 会创建"部分数据"结构，
//     毒化后续读路径（SISMEMBER 误报、列表缺项、计数从 1 起步）。因此写路径发现
//     key 不存在时，先从 MySQL 回源重建，再做增量更新。
//  2. 取消操作同样先重建：否则 SREM 对冷 key 返回 0 会被误判为"本来就没点赞"，
//     导致取消事件永久丢失。
//  3. stats 重建使用 HSETNX：只补缺失字段，不覆盖并发的 HINCRBY 增量。
//  4. 写路径统一刷新 TTL：保证所有 key 都能通过过期自愈。
//  5. 写路径使用 Lua 原子化「SADD/SREM 翻转 + ZADD/ZREM + 计数增减」：
//     若分步执行，SADD 成功与 HINCRBY 之间存在窗口，同用户并发点赞/取消时
//     非负保护会读到中间态，导致计数与集合基数漂移（集成并发测试可复现）。
package logic

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sponge-dad/feed/app/interaction/rpc/internal/keys"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"
	event "github.com/sponge-dad/feed/common/event/interaction"
	"github.com/sponge-dad/feed/common/requestid"

	red "github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

// 互动种类：点赞 / 收藏。
type interactKind int

const (
	kindLike interactKind = iota
	kindCollect
)

const (
	// rebuildListLimit 重建 user:likes / user:collects ZSet 时最多回源的记录数（06-cache.md §3.3 LIMIT N）。
	rebuildListLimit = 1000
	// defaultPageSize 列表接口默认页大小。
	defaultPageSize = 20
	// maxPageSize 列表接口单页上限。
	maxPageSize = 100
	// maxBatchSize 批量查询接口单次上限（proto 注释：建议单次不超过 100 个）。
	maxBatchSize = 100
	// statusLiked 记录有效状态（1:已点赞/已收藏）。
	statusLiked = 1
)

// interactHelper 绑定一次请求上下文的互动核心逻辑执行器。
type interactHelper struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	kind   interactKind
	logger logx.Logger
}

// newInteractHelper 创建互动逻辑执行器。
func newInteractHelper(ctx context.Context, svcCtx *svc.ServiceContext, kind interactKind) *interactHelper {
	return &interactHelper{
		ctx:    ctx,
		svcCtx: svcCtx,
		kind:   kind,
		logger: logx.WithContext(ctx),
	}
}

// ---------- key / 字段 / 动作映射 ----------

func (h *interactHelper) setKey(feedID int64) string {
	if h.kind == kindLike {
		return keys.LikeFeed(feedID)
	}
	return keys.CollectFeed(feedID)
}

func (h *interactHelper) zsetKey(userID int64) string {
	if h.kind == kindLike {
		return keys.UserLikes(userID)
	}
	return keys.UserCollects(userID)
}

func (h *interactHelper) statsField() string {
	if h.kind == kindLike {
		return keys.FieldLikeCount
	}
	return keys.FieldCollectCount
}

func (h *interactHelper) addAction() int32 {
	if h.kind == kindLike {
		return event.ActionLike
	}
	return event.ActionCollect
}

func (h *interactHelper) removeAction() int32 {
	if h.kind == kindLike {
		return event.ActionUnlike
	}
	return event.ActionUncollect
}

// ---------- MySQL 回源（按 kind 分发到对应 model） ----------

func (h *interactHelper) countByFeed(feedID int64) (int64, error) {
	if h.kind == kindLike {
		return h.svcCtx.LikesModel.CountByFeedId(h.ctx, uint64(feedID))
	}
	return h.svcCtx.CollectionsModel.CountByFeedId(h.ctx, uint64(feedID))
}

func (h *interactHelper) countByFeeds(feedIDs []int64) (map[int64]int64, error) {
	ids := make([]uint64, 0, len(feedIDs))
	for _, id := range feedIDs {
		ids = append(ids, uint64(id))
	}
	var (
		raw map[uint64]int64
		err error
	)
	if h.kind == kindLike {
		raw, err = h.svcCtx.LikesModel.CountByFeedIds(h.ctx, ids)
	} else {
		raw, err = h.svcCtx.CollectionsModel.CountByFeedIds(h.ctx, ids)
	}
	if err != nil {
		return nil, err
	}
	result := make(map[int64]int64, len(raw))
	for id, cnt := range raw {
		result[int64(id)] = cnt
	}
	return result, nil
}

func (h *interactHelper) userIDsByFeed(feedID int64) ([]uint64, error) {
	if h.kind == kindLike {
		return h.svcCtx.LikesModel.FindUserIdsByFeedId(h.ctx, uint64(feedID))
	}
	return h.svcCtx.CollectionsModel.FindUserIdsByFeedId(h.ctx, uint64(feedID))
}

// listEntry 用户互动列表条目（重建 ZSet 用）。
type listEntry struct {
	feedID int64
	score  int64 // 互动时间，秒级 Unix
}

func (h *interactHelper) listEntriesByUser(userID int64, limit int) ([]listEntry, error) {
	entries := make([]listEntry, 0, limit)
	if h.kind == kindLike {
		rows, err := h.svcCtx.LikesModel.FindValidByUserId(h.ctx, uint64(userID), limit)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			entries = append(entries, listEntry{feedID: int64(r.FeedId), score: r.CreatedAt.Unix()})
		}
		return entries, nil
	}
	rows, err := h.svcCtx.CollectionsModel.FindValidByUserId(h.ctx, uint64(userID), limit)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		entries = append(entries, listEntry{feedID: int64(r.FeedId), score: r.CreatedAt.Unix()})
	}
	return entries, nil
}

// statusMapByUserFeeds 从 MySQL 批量查询用户对多个帖子的有效互动状态。
func (h *interactHelper) statusMapByUserFeeds(userID int64, feedIDs []int64) (map[int64]bool, error) {
	ids := make([]uint64, 0, len(feedIDs))
	for _, id := range feedIDs {
		ids = append(ids, uint64(id))
	}
	result := make(map[int64]bool, len(feedIDs))
	if h.kind == kindLike {
		rows, err := h.svcCtx.LikesModel.FindByUserIdFeedIds(h.ctx, uint64(userID), ids)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			result[int64(r.FeedId)] = r.Status == statusLiked
		}
		return result, nil
	}
	rows, err := h.svcCtx.CollectionsModel.FindByUserIdFeedIds(h.ctx, uint64(userID), ids)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		result[int64(r.FeedId)] = r.Status == statusLiked
	}
	return result, nil
}

// ---------- 缓存重建（冷 key 先重建再增量） ----------

// ensureSet 保证 like:feed / collect:feed Set 可用。
// 返回 existed=true 表示 key 已存在（成员以 Redis 为准）；
// existed=false 表示刚从 MySQL 重建，members 为回源得到的成员集合。
//
// 重建时无论回源结果是否为空，都会写入 keys.SetSentinel 哨兵成员并设置 TTL：
//  1. 「已加载但为空」的集合 key 依然存在，读路径不会把空集合误判为冷缓存
//     再次回源 MySQL（修复取消点赞后 MQ 未消费窗口内误报已点赞的竞态）；
//  2. 空集合首查后即建 key，后续并发查询直接命中 Redis，不会重复回源击穿。
func (h *interactHelper) ensureSet(feedID int64) (existed bool, members map[int64]struct{}, err error) {
	key := h.setKey(feedID)
	exists, err := h.svcCtx.Redis.ExistsCtx(h.ctx, key)
	if err != nil {
		return false, nil, err
	}
	if exists {
		return true, nil, nil
	}
	userIDs, err := h.userIDsByFeed(feedID)
	if err != nil {
		return false, nil, err
	}
	members = make(map[int64]struct{}, len(userIDs))
	values := make([]any, 0, len(userIDs)+1)
	values = append(values, keys.SetSentinel)
	for _, uid := range userIDs {
		members[int64(uid)] = struct{}{}
		values = append(values, strconv.FormatUint(uid, 10))
	}
	if _, err = h.svcCtx.Redis.SaddCtx(h.ctx, key, values...); err != nil {
		return false, nil, err
	}
	if err = h.svcCtx.Redis.ExpireCtx(h.ctx, key, keys.TTLFeedSet); err != nil {
		h.logger.Errorf("interaction: expire %s failed: %v", key, err)
	}
	return false, members, nil
}

// ensureZSet 保证 user:likes / user:collects ZSet 可用（key 不存在时从 MySQL 重建）。
func (h *interactHelper) ensureZSet(userID int64) error {
	key := h.zsetKey(userID)
	exists, err := h.svcCtx.Redis.ExistsCtx(h.ctx, key)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	entries, err := h.listEntriesByUser(userID, rebuildListLimit)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	pairs := make([]redis.Pair, 0, len(entries))
	for _, e := range entries {
		pairs = append(pairs, redis.Pair{Key: strconv.FormatInt(e.feedID, 10), Score: e.score})
	}
	if _, err = h.svcCtx.Redis.ZaddsCtx(h.ctx, key, pairs...); err != nil {
		return err
	}
	if err = h.svcCtx.Redis.ExpireCtx(h.ctx, key, keys.TTLUserZSet); err != nil {
		h.logger.Errorf("interaction: expire %s failed: %v", key, err)
	}
	return nil
}

// ensureStats 保证 feed:stats Hash 可用。
// 重建使用 HSETNX 只补缺失字段，不覆盖并发写入的 HINCRBY 增量（06-cache.md §4.2 加固）。
func (h *interactHelper) ensureStats(feedID int64) error {
	key := keys.FeedStats(feedID)
	fields, err := h.svcCtx.Redis.HgetallCtx(h.ctx, key)
	if err != nil {
		return err
	}
	_, hasLike := fields[keys.FieldLikeCount]
	_, hasCollect := fields[keys.FieldCollectCount]
	if hasLike && hasCollect {
		return nil
	}
	if !hasLike {
		cnt, err := h.svcCtx.LikesModel.CountByFeedId(h.ctx, uint64(feedID))
		if err != nil {
			return err
		}
		if _, err = h.svcCtx.Redis.HsetnxCtx(h.ctx, key, keys.FieldLikeCount, strconv.FormatInt(cnt, 10)); err != nil {
			return err
		}
	}
	if !hasCollect {
		cnt, err := h.svcCtx.CollectionsModel.CountByFeedId(h.ctx, uint64(feedID))
		if err != nil {
			return err
		}
		if _, err = h.svcCtx.Redis.HsetnxCtx(h.ctx, key, keys.FieldCollectCount, strconv.FormatInt(cnt, 10)); err != nil {
			return err
		}
	}
	if err = h.svcCtx.Redis.ExpireCtx(h.ctx, key, keys.TTLFeedStats); err != nil {
		h.logger.Errorf("interaction: expire %s failed: %v", key, err)
	}
	return nil
}

// ---------- 写路径 ----------

// addScript 原子执行点赞/收藏翻转：SADD 成功才 ZADD + HINCRBY。
// 先补写哨兵成员（幂等），保证 Set 永远携带「已加载」标记（兼容历史无哨兵 key）。
// KEYS[1]=set KEYS[2]=zset KEYS[3]=stats
// ARGV[1]=userID ARGV[2]=feedID ARGV[3]=score ARGV[4]=statsField ARGV[5]=sentinel
// 返回 1 表示新增互动，0 表示重复操作。
var addScript = redis.NewScript(`
redis.call('SADD', KEYS[1], ARGV[5])
local added = redis.call('SADD', KEYS[1], ARGV[1])
if added == 1 then
  redis.call('ZADD', KEYS[2], ARGV[3], ARGV[2])
  redis.call('HINCRBY', KEYS[3], ARGV[4], 1)
end
return added
`)

// removeScript 原子执行取消点赞/取消收藏：SREM 成功才扣减计数（带非负保护），
// ZREM 无条件执行保证列表不残留。
// 先补写哨兵成员：移除最后一个真实成员后 Set 仍持有哨兵而不会被 Redis 删除，
// 「已加载的空集合」不会在下次查询时被误判为冷缓存回源到（MQ 尚未落库的）旧 MySQL 状态。
// KEYS/ARGV 含义同 addScript。返回 1 表示确实取消，0 表示本无互动。
var removeScript = redis.NewScript(`
redis.call('SADD', KEYS[1], ARGV[5])
redis.call('ZREM', KEYS[2], ARGV[2])
local removed = redis.call('SREM', KEYS[1], ARGV[1])
if removed == 1 then
  local cur = tonumber(redis.call('HGET', KEYS[3], ARGV[4]) or '0')
  if cur > 0 then
    redis.call('HINCRBY', KEYS[3], ARGV[4], -1)
  end
end
return removed
`)

// flip 以 Lua 原子执行集合翻转与计数增减（见文件头说明第 5 点）。
func (h *interactHelper) flip(script *redis.Script, userID, feedID int64) (bool, error) {
	resp, err := h.svcCtx.Redis.ScriptRunCtx(h.ctx, script,
		[]string{h.setKey(feedID), h.zsetKey(userID), keys.FeedStats(feedID)},
		strconv.FormatInt(userID, 10),
		strconv.FormatInt(feedID, 10),
		strconv.FormatInt(time.Now().Unix(), 10),
		h.statsField(),
		keys.SetSentinel,
	)
	if err != nil {
		return false, err
	}
	flipped, ok := resp.(int64)
	if !ok {
		return false, fmt.Errorf("interaction: unexpected script result %T", resp)
	}
	return flipped == 1, nil
}

// add 执行点赞/收藏 Redis 先行写（02-like.md §1.4）。返回是否为新增互动。
func (h *interactHelper) add(userID, feedID int64) (bool, error) {
	// 冷 key 先重建，避免部分写毒化（见文件头说明第 1 点）。
	if _, _, err := h.ensureSet(feedID); err != nil {
		return false, err
	}
	if err := h.ensureZSet(userID); err != nil {
		return false, err
	}
	if err := h.ensureStats(feedID); err != nil {
		return false, err
	}

	added, err := h.flip(addScript, userID, feedID)
	if err != nil {
		return false, err
	}
	h.refreshTTL(userID, feedID)
	return added, nil
}

// remove 执行取消点赞/取消收藏 Redis 先行写（02-like.md §2.2）。返回是否确实取消。
func (h *interactHelper) remove(userID, feedID int64) (bool, error) {
	// 冷 key 必须先重建：否则 SREM 对不存在的 key 返回 0，
	// 会被误判为"本来就没互动"而丢失取消事件（见文件头说明第 2 点）。
	if _, _, err := h.ensureSet(feedID); err != nil {
		return false, err
	}
	if err := h.ensureZSet(userID); err != nil {
		return false, err
	}
	if err := h.ensureStats(feedID); err != nil {
		return false, err
	}

	removed, err := h.flip(removeScript, userID, feedID)
	if err != nil {
		return false, err
	}
	h.refreshTTL(userID, feedID)
	return removed, nil
}

// currentStats 读取 feed:stats 中某字段当前值，字段缺失按 0 处理。
func (h *interactHelper) currentStats(feedID int64, field string) (int64, error) {
	fields, err := h.svcCtx.Redis.HgetallCtx(h.ctx, keys.FeedStats(feedID))
	if err != nil {
		return 0, err
	}
	raw, ok := fields[field]
	if !ok {
		return 0, nil
	}
	val, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, nil
	}
	return val, nil
}

// refreshTTL 写路径统一刷新三个 key 的 TTL，保证可通过过期自愈；失败仅记日志。
func (h *interactHelper) refreshTTL(userID, feedID int64) {
	if err := h.svcCtx.Redis.ExpireCtx(h.ctx, h.setKey(feedID), keys.TTLFeedSet); err != nil {
		h.logger.Errorf("interaction: expire %s failed: %v", h.setKey(feedID), err)
	}
	if err := h.svcCtx.Redis.ExpireCtx(h.ctx, h.zsetKey(userID), keys.TTLUserZSet); err != nil {
		h.logger.Errorf("interaction: expire %s failed: %v", h.zsetKey(userID), err)
	}
	if err := h.svcCtx.Redis.ExpireCtx(h.ctx, keys.FeedStats(feedID), keys.TTLFeedStats); err != nil {
		h.logger.Errorf("interaction: expire %s failed: %v", keys.FeedStats(feedID), err)
	}
}

// publish 发送互动事件到 RocketMQ（异步发送，失败只记日志不阻塞返回，02-like.md §1.5 方案 B）。
func (h *interactHelper) publish(userID, feedID int64, action int32) {
	if h.svcCtx.Producer == nil {
		h.logger.Errorf("interaction: producer not configured, event lost user=%d feed=%d action=%d",
			userID, feedID, action)
		return
	}
	ev := event.NewEvent(userID, feedID, action, requestid.FromContext(h.ctx))
	body, err := json.Marshal(ev)
	if err != nil {
		h.logger.Errorf("interaction: marshal event failed: %v", err)
		return
	}
	if err := h.svcCtx.Producer.SendSync(event.TopicInteractionEvent, body); err != nil {
		// MQ 失败不回滚 Redis：用户可见状态以 Redis 为准，
		// MySQL 通过 TTL 过期回源 + 定时校准最终收敛（06-cache.md §1）。
		h.logger.Errorf("interaction: send event failed user=%d feed=%d action=%d err=%v",
			userID, feedID, action, err)
	}
}

// ---------- 读路径 ----------

// isMember 查询用户是否已点赞/收藏某帖子（Cache-Aside：Set 未命中回源重建）。
func (h *interactHelper) isMember(userID, feedID int64) (bool, error) {
	existed, members, err := h.ensureSet(feedID)
	if err != nil {
		return false, err
	}
	if !existed {
		_, ok := members[userID]
		return ok, nil
	}
	return h.svcCtx.Redis.SismemberCtx(h.ctx, h.setKey(feedID), strconv.FormatInt(userID, 10))
}

// batchMember 批量查询用户互动状态。
// 命中的 key 用 SISMEMBER；未命中的 feed 直接回源 MySQL 批量查询，
// 不做单成员回填（部分 Set 会毒化其他用户的查询，见文件头说明）。
func (h *interactHelper) batchMember(userID int64, feedIDs []int64) (map[int64]bool, error) {
	result := make(map[int64]bool, len(feedIDs))
	if len(feedIDs) == 0 {
		return result, nil
	}
	member := strconv.FormatInt(userID, 10)

	existsCmds := make(map[int64]*red.IntCmd, len(feedIDs))
	memberCmds := make(map[int64]*red.BoolCmd, len(feedIDs))
	err := h.svcCtx.Redis.PipelinedCtx(h.ctx, func(pipe red.Pipeliner) error {
		for _, feedID := range feedIDs {
			key := h.setKey(feedID)
			existsCmds[feedID] = pipe.Exists(h.ctx, key)
			memberCmds[feedID] = pipe.SIsMember(h.ctx, key, member)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	var missed []int64
	for _, feedID := range feedIDs {
		if existsCmds[feedID].Val() > 0 {
			result[feedID] = memberCmds[feedID].Val()
		} else {
			missed = append(missed, feedID)
		}
	}
	if len(missed) > 0 {
		fromDB, err := h.statusMapByUserFeeds(userID, missed)
		if err != nil {
			return nil, err
		}
		for _, feedID := range missed {
			result[feedID] = fromDB[feedID]
		}
	}
	return result, nil
}

// pageResult 列表分页结果。
type pageResult struct {
	feedIDs    []int64
	nextCursor string
	total      int64
}

// zsetItem ZSet 条目。
type zsetItem struct {
	feedID int64
	score  int64
}

// page 游标分页读取用户点赞/收藏列表（05-user-list.md）。
// 游标格式：base64("score:feed_id")；同分（同秒）条目以 feed_id 数值降序为第二排序键。
func (h *interactHelper) page(userID int64, pageSize int32, cursor string) (*pageResult, error) {
	size := int(pageSize)
	if size <= 0 {
		size = defaultPageSize
	}
	if size > maxPageSize {
		size = maxPageSize
	}

	if err := h.ensureZSet(userID); err != nil {
		return nil, err
	}
	key := h.zsetKey(userID)
	total, err := h.svcCtx.Redis.ZcardCtx(h.ctx, key)
	if err != nil {
		return nil, err
	}
	if total == 0 {
		return &pageResult{total: 0}, nil
	}

	var (
		hasCursor    bool
		cursorScore  int64
		cursorFeedID int64
	)
	if cursor != "" {
		cursorScore, cursorFeedID, err = decodeCursor(cursor)
		if err != nil {
			return nil, err
		}
		hasCursor = true
	}

	// 第一轮：取候选。
	//   无游标：直接取前 size 条。
	//   有游标：A = score 严格小于游标分的前 size 条；B = 与游标同分的全部条目（数量通常很小），
	//           取其中 feed_id 数值小于游标 feed_id 的部分。
	var mainCmd, tieCmd *red.ZSliceCmd
	err = h.svcCtx.Redis.PipelinedCtx(h.ctx, func(pipe red.Pipeliner) error {
		if hasCursor {
			mainCmd = pipe.ZRevRangeByScoreWithScores(h.ctx, key, &red.ZRangeBy{
				Min: "-inf", Max: "(" + strconv.FormatInt(cursorScore, 10), Count: int64(size),
			})
			tieCmd = pipe.ZRevRangeByScoreWithScores(h.ctx, key, &red.ZRangeBy{
				Min: strconv.FormatInt(cursorScore, 10), Max: strconv.FormatInt(cursorScore, 10),
			})
		} else {
			mainCmd = pipe.ZRevRangeByScoreWithScores(h.ctx, key, &red.ZRangeBy{
				Min: "-inf", Max: "+inf", Count: int64(size),
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	seen := make(map[int64]struct{})
	var candidates []zsetItem
	appendItem := func(z red.Z) {
		feedID, perr := strconv.ParseInt(fmt.Sprint(z.Member), 10, 64)
		if perr != nil {
			return
		}
		if _, ok := seen[feedID]; ok {
			return
		}
		seen[feedID] = struct{}{}
		candidates = append(candidates, zsetItem{feedID: feedID, score: int64(z.Score)})
	}
	if hasCursor && tieCmd != nil {
		for _, z := range tieCmd.Val() {
			feedID, perr := strconv.ParseInt(fmt.Sprint(z.Member), 10, 64)
			if perr != nil || feedID >= cursorFeedID {
				continue
			}
			appendItem(z)
		}
	}
	for _, z := range mainCmd.Val() {
		appendItem(z)
	}
	if len(candidates) == 0 {
		return &pageResult{total: int64(total)}, nil
	}

	// 排序：score 降序，同分按 feed_id 数值降序（Redis 同分是字典序，长度不同的数字串会乱序）。
	sortItemsDesc(candidates)

	// 边界补齐：若截断点落在同分组中间，补取该分值的全部条目，
	// 保证游标 (score, feed_id) 与下一页过滤条件数值一致，不丢条目。
	cutIdx := size
	if cutIdx > len(candidates) {
		cutIdx = len(candidates)
	}
	cutScore := candidates[cutIdx-1].score
	tail, err := h.fetchScoreTies(key, cutScore)
	if err != nil {
		return nil, err
	}
	for _, z := range tail {
		feedID, perr := strconv.ParseInt(fmt.Sprint(z.Member), 10, 64)
		if perr != nil {
			continue
		}
		if hasCursor && cutScore == cursorScore && feedID >= cursorFeedID {
			continue
		}
		appendItem(z)
	}
	sortItemsDesc(candidates)

	if len(candidates) > size {
		candidates = candidates[:size]
	}
	feedIDs := make([]int64, 0, len(candidates))
	for _, it := range candidates {
		feedIDs = append(feedIDs, it.feedID)
	}
	nextCursor := ""
	if len(candidates) == size {
		last := candidates[len(candidates)-1]
		nextCursor = encodeCursor(last.score, last.feedID)
	}
	return &pageResult{feedIDs: feedIDs, nextCursor: nextCursor, total: int64(total)}, nil
}

// fetchScoreTies 取某个分值下的全部条目。
func (h *interactHelper) fetchScoreTies(key string, score int64) ([]red.Z, error) {
	var cmd *red.ZSliceCmd
	err := h.svcCtx.Redis.PipelinedCtx(h.ctx, func(pipe red.Pipeliner) error {
		cmd = pipe.ZRevRangeByScoreWithScores(h.ctx, key, &red.ZRangeBy{
			Min: strconv.FormatInt(score, 10), Max: strconv.FormatInt(score, 10),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return cmd.Val(), nil
}

// sortItemsDesc 按 (score 降序, feed_id 降序) 排序。
func sortItemsDesc(items []zsetItem) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].score != items[j].score {
			return items[i].score > items[j].score
		}
		return items[i].feedID > items[j].feedID
	})
}

// encodeCursor 编码游标：base64("score:feed_id")。
func encodeCursor(score, feedID int64) string {
	return base64.StdEncoding.EncodeToString(
		[]byte(strconv.FormatInt(score, 10) + ":" + strconv.FormatInt(feedID, 10)))
}

// decodeCursor 解析游标；非法游标返回参数错误。
func decodeCursor(cursor string) (score, feedID int64, err error) {
	raw, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return 0, 0, errorx.NewWithMsg(errorx.ParamError, "非法游标")
	}
	parts := strings.SplitN(string(raw), ":", 2)
	if len(parts) != 2 {
		return 0, 0, errorx.NewWithMsg(errorx.ParamError, "非法游标")
	}
	score, err1 := strconv.ParseInt(parts[0], 10, 64)
	feedID, err2 := strconv.ParseInt(parts[1], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, errorx.NewWithMsg(errorx.ParamError, "非法游标")
	}
	return score, feedID, nil
}
