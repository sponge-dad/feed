package feed

import (
	"context"
	"strconv"
	"time"

	feedpb "github.com/sponge-dad/feed/app/feed/rpc/feed"
	feedClient "github.com/sponge-dad/feed/app/feed/rpc/feedclient"
	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	"github.com/sponge-dad/feed/common/errorx"
	bhv "github.com/sponge-dad/feed/common/event/behavior"

	"github.com/zeromicro/go-zero/core/logx"
)

// ReportBehaviorsLogic 行为埋点上报逻辑。
// 见 docs/design/agent/03-behavior-event.md §3
type ReportBehaviorsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewReportBehaviorsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ReportBehaviorsLogic {
	return &ReportBehaviorsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// 拒绝原因（返回给客户端，便于自查埋点实现）。
const (
	reasonFeedInvalid      = "invalid_feed_id"
	reasonActionInvalid    = "invalid_action_type"
	reasonRequestIDInvalid = "invalid_request_id"
	reasonTimestampRange   = "timestamp_out_of_range"
	reasonPositionRange    = "position_out_of_range"
	reasonWatchRange       = "watch_duration_out_of_range"
	reasonMediaRange       = "media_duration_out_of_range"
	reasonFeedNotFound     = "feed_not_found"
	reasonMarshal          = "marshal_error"
	reasonMQ               = "mq_error"
)

// behaviorRateScript 单用户上报限流：按事件条数原子累加并在首次写入时设置窗口 TTL。
//
// 用 INCRBY 一次性加 N（而不是逐条 Take），单批只需一次 Redis 往返；
// 超限时计数仍然累加，使得超限用户在本窗口内持续被拒，避免被反复冲击。
// 返回 1 放行，0 超限。
const behaviorRateScript = `
local n = tonumber(ARGV[1])
local quota = tonumber(ARGV[2])
local ttl = tonumber(ARGV[3])
local current = redis.call('INCRBY', KEYS[1], n)
if current == n then
  redis.call('EXPIRE', KEYS[1], ttl)
end
if current > quota then
  return 0
end
return 1
`

// ReportBehaviors 批量上报行为埋点。
//
// 网关职责（强校验，见 03-behavior-event.md §3）：
//  1. 鉴权（JWT 中间件已完成），取 user_id——客户端传的 user_id 一律忽略；
//  2. 批量大小 1~50，超限整批拒绝；
//  3. 单用户限流 300 条/分钟，超限整批拒绝（429）；
//  4. 字段范围校验（action_type 白名单、时间偏差、position、时长）；
//  5. 校验 feed 存在且 status==NORMAL，并用服务端 author_id 覆盖；
//     SHARE 不做 owner 校验：任何登录用户均可分享他人视频，否则分享指标恒为 0；
//  6. 服务端生成 event_id 与 server_time，逐条 SendSync 到 feed-behavior-event。
//
// 单条不合法只做局部拒绝并返回 200，避免一条脏数据导致整批埋点丢失。
func (l *ReportBehaviorsLogic) ReportBehaviors(req *types.ReportBehaviorsReq) (*types.ReportBehaviorsResp, error) {
	me := middleware.MustGetUserID(l.ctx)
	if me == 0 {
		return nil, errorx.New(errorx.Unauthorized)
	}

	n := len(req.Events)
	if n < 1 || n > bhv.MaxBatchSize {
		return nil, errorx.NewWithMsg(errorx.BehaviorInvalid,
			"批量行为事件数量需在 1~"+strconv.Itoa(bhv.MaxBatchSize)+" 之间")
	}

	allowed, err := l.allowRate(me, n)
	if err != nil {
		// 限流依赖不可用时放行，避免 Redis 抖动导致埋点整体不可用（埋点非关键路径）
		l.Errorf("report behaviors rate limit check failed uid=%d err=%v", me, err)
	} else if !allowed {
		return nil, errorx.New(errorx.TooManyReq)
	}

	now := time.Now().UnixMilli()

	type pending struct {
		index int
		item  *types.BehaviorItem
		ev    *bhv.FeedBehaviorEvent
	}
	pendings := make([]pending, 0, n)
	rejected := make([]types.RejectedEvent, 0)
	feedIDSet := make(map[int64]struct{}, n)

	for i := range req.Events {
		it := &req.Events[i]
		if reason := validateItem(it, now); reason != "" {
			rejected = append(rejected, types.RejectedEvent{
				Index: i, ClientEventId: it.ClientEventId, Reason: reason,
			})
			continue
		}
		// author_id 先留空，待批量查 Feed 后校正
		ev := bhv.NewEvent(it.RequestId, me, it.FeedId, 0, it.ActionType, it.Position,
			it.WatchDurationMs, it.MediaDurationMs, it.Timestamp, now, it.ClientEventId)
		feedIDSet[it.FeedId] = struct{}{}
		pendings = append(pendings, pending{index: i, item: it, ev: ev})
	}

	// 批量校验 feed 存在且 status==NORMAL。
	// Feed RPC 失败属基础设施错误，直接向上传播（区别于 feed_not_found 的局部拒绝）。
	feedInfo, err := l.batchGetNormalFeeds(feedIDSet)
	if err != nil {
		l.Errorf("report behaviors batch get feeds failed uid=%d err=%v", me, err)
		return nil, err
	}

	accepted := 0
	for _, p := range pendings {
		info, ok := feedInfo[p.ev.FeedID]
		if !ok || feedpb.FeedStatus(info.Status) != feedpb.FeedStatus_FEED_STATUS_NORMAL {
			rejected = append(rejected, types.RejectedEvent{
				Index: p.index, ClientEventId: p.item.ClientEventId, Reason: reasonFeedNotFound,
			})
			continue
		}
		// 以服务端权威值覆盖，客户端上报的作者信息一律不采信
		p.ev.AuthorID = info.AuthorId

		body, err := p.ev.ToJSON()
		if err != nil {
			rejected = append(rejected, types.RejectedEvent{
				Index: p.index, ClientEventId: p.item.ClientEventId, Reason: reasonMarshal,
			})
			continue
		}
		if err := l.svcCtx.Producer.SendSync(bhv.TopicFeedBehaviorEvent, body); err != nil {
			l.Errorf("report behaviors mq send failed uid=%d action=%s feed=%d err=%v",
				me, p.ev.ActionType, p.ev.FeedID, err)
			rejected = append(rejected, types.RejectedEvent{
				Index: p.index, ClientEventId: p.item.ClientEventId, Reason: reasonMQ,
			})
			continue
		}
		accepted++
	}

	return &types.ReportBehaviorsResp{Accepted: accepted, Rejected: rejected}, nil
}

// behaviorRateKey 单用户上报限流键：behavior:ratelimit:{user_id}。
func behaviorRateKey(userID int64) string {
	return "behavior:ratelimit:" + strconv.FormatInt(userID, 10)
}

// allowRate 单用户按条数限流。
func (l *ReportBehaviorsLogic) allowRate(userID int64, n int) (bool, error) {
	res, err := l.svcCtx.BehaviorRedis.EvalCtx(l.ctx, behaviorRateScript, []string{behaviorRateKey(userID)},
		n, l.svcCtx.BehaviorRateLimit, svc.BehaviorRateWindowSec)
	if err != nil {
		return false, err
	}
	v, ok := res.(int64)
	if !ok {
		return true, nil
	}
	return v == 1, nil
}

// validateItem 校验单条上报字段，返回拒绝原因（空串表示通过）。
func validateItem(it *types.BehaviorItem, nowMs int64) string {
	if it.FeedId <= 0 {
		return reasonFeedInvalid
	}
	if !bhv.IsValidAction(it.ActionType) {
		return reasonActionInvalid
	}
	if !isSafeRequestID(it.RequestId) {
		return reasonRequestIDInvalid
	}
	// 时间偏差超过 ±1h 视为客户端时钟异常或重放
	if diff := nowMs - it.Timestamp; diff < -bhv.MaxClockSkewMs || diff > bhv.MaxClockSkewMs {
		return reasonTimestampRange
	}
	if it.Position < 0 || it.Position > bhv.MaxPosition {
		return reasonPositionRange
	}
	if it.WatchDurationMs < 0 || it.WatchDurationMs > bhv.MaxWatchDurationMs {
		return reasonWatchRange
	}
	if it.MediaDurationMs < 0 || it.MediaDurationMs > bhv.MaxWatchDurationMs {
		return reasonMediaRange
	}
	return ""
}

// isSafeRequestID 限制 request_id 长度与字符集。
//
// request_id 会拼进 Redis 键 behavior:expose:{request_id}:{feed_id}，
// 必须限长防止超长键打爆内存，限字符集防止冒号等分隔符伪造出其他事件的去重键。
func isSafeRequestID(s string) bool {
	if s == "" {
		return true // 可选字段
	}
	if len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

// batchGetNormalFeeds 一次性取回本批涉及的所有 feed。
//
// 单批事件数上限 50，去重后必然 ≤ 50，小于下游 BatchGetFeeds 的 100 上限
// （超限会被静默截断，见 batchgetfeedslogic.go），因此无需分页。
func (l *ReportBehaviorsLogic) batchGetNormalFeeds(set map[int64]struct{}) (map[int64]*feedClient.FeedInfo, error) {
	if len(set) == 0 {
		return nil, nil
	}
	ids := make([]int64, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	resp, err := l.svcCtx.FeedRpc.BatchGetFeeds(l.ctx, &feedClient.BatchGetFeedsReq{FeedIds: ids})
	if err != nil {
		return nil, err
	}
	return resp.Feeds, nil
}
