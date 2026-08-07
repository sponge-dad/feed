package feed

import (
	"context"
	"net/http"
	"time"

	feedpb "github.com/sponge-dad/feed/app/feed/rpc/feed"
	feedClient "github.com/sponge-dad/feed/app/feed/rpc/feedclient"
	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	"github.com/sponge-dad/feed/common/errorx"
	bhv "github.com/sponge-dad/feed/common/event/behavior"
	"github.com/sponge-dad/feed/common/ipx"
	"github.com/sponge-dad/feed/common/requestid"

	"github.com/zeromicro/go-zero/core/logx"
)

// ReportBehaviorsLogic 行为埋点上报逻辑。
// 见 docs/design/agent/03-behavior-event.md §2.1
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

// clientMetaKey 用于在 context 中透传客户端 IP / UA / req_id（由 handler 注入）。
type clientMetaKey struct{}

type clientMeta struct {
	IP, UA, ReqID string
}

// WithClientMeta 把请求的客户端元信息注入 context，供 logic 读取。
func WithClientMeta(ctx context.Context, r *http.Request) context.Context {
	reqID := requestid.FromContext(ctx)
	return context.WithValue(ctx, clientMetaKey{}, &clientMeta{
		IP:    ipx.ClientIPFromContext(ctx),
		UA:    r.UserAgent(),
		ReqID: reqID,
	})
}

func clientMetaFromCtx(ctx context.Context) *clientMeta {
	if m, ok := ctx.Value(clientMetaKey{}).(*clientMeta); ok {
		return m
	}
	return &clientMeta{}
}

// 客户端字段取值范围与白名单（见 03-behavior-event.md §3）。
const (
	timeDeviationMs = int64(3600 * 1000)      // 行为时间与服务端偏差不超过 1h
	maxPos          = int32(1000)             // 列表位置上限
	maxDurationMs   = int64(24 * 3600 * 1000) // 停留时长上限 24h
)

var (
	validScenes = map[string]struct{}{
		bhv.SceneFeedDetail:    {},
		bhv.SceneFeedList:      {},
		bhv.SceneCommentDetail: {},
		bhv.SceneUserProfile:   {},
		bhv.SceneSearch:        {},
	}
	validPages = map[string]struct{}{
		bhv.PageFeedDetail:    {},
		bhv.PageFeedList:      {},
		bhv.PageCommentDetail: {},
		bhv.PageUserProfile:   {},
		bhv.PageSearch:        {},
	}
)

// validateClientFields 在 ev.Validate() 基础上补充客户端字段的范围/白名单校验。
// 返回拒绝原因（空串表示通过）。
func validateClientFields(ev *bhv.FeedBehaviorEvent) string {
	now := time.Now().UnixMilli()
	diff := now - ev.Timestamp
	if diff < -timeDeviationMs || diff > timeDeviationMs {
		return "timestamp_out_of_range"
	}
	if ev.Pos < 0 || ev.Pos > maxPos {
		return "pos_out_of_range"
	}
	if ev.Duration < 0 || ev.Duration > maxDurationMs {
		return "duration_out_of_range"
	}
	if ev.Scene != "" {
		if _, ok := validScenes[ev.Scene]; !ok {
			return "scene_invalid"
		}
	}
	if ev.Page != "" {
		if _, ok := validPages[ev.Page]; !ok {
			return "page_invalid"
		}
	}
	return ""
}

// ReportBehaviors 批量上报行为埋点。
//
// 网关职责（强校验）：
//  1. 鉴权（JWT，中间件已完成），取 user_id；
//  2. 批量大小 1~50（超限整批拒绝，参数级错误）；
//  3. 事件字段校验（基础 + 客户端字段范围/白名单）；
//  4. 校验 feed 存在且 status==NORMAL（批量调 Feed RPC）；
//  5. SHARE 不做 owner 校验：任何登录用户均可分享任意视频（含他人视频）；
//  6. 逐条 SendSync 到 feed-behavior-event。
//
// 无论是否部分拒绝，均返回 200，body 中携带 accepted / rejected。
// 频率限制（强校验）放在 Interaction Worker 侧兜底（见 behaviorWorker.go）。
func (l *ReportBehaviorsLogic) ReportBehaviors(req *types.ReportBehaviorsReq) (*types.ReportBehaviorsResp, error) {
	me := middleware.MustGetUserID(l.ctx)
	if me == 0 {
		return nil, errorx.New(errorx.Unauthorized)
	}
	// 批量大小限制：超限整批拒绝（参数级错误，区别于单条部分拒绝）。
	if n := len(req.Events); n < 1 || n > 50 {
		return nil, errorx.NewWithMsg(errorx.BehaviorInvalid, "批量行为事件数量需在 1~50 之间")
	}
	meta := clientMetaFromCtx(l.ctx)

	type pending struct {
		eventID string
		ev      *bhv.FeedBehaviorEvent
	}
	pendings := make([]pending, 0, len(req.Events))
	feedIDSet := make(map[int64]struct{})
	accepted := make([]string, 0, len(req.Events))
	rejected := make(map[string]string)

	for _, it := range req.Events {
		ev := bhv.NewEvent(me, it.FeedId, it.Action, it.TargetId, it.Timestamp, it.Scene, it.Page, it.Pos, it.Duration, meta.IP, meta.UA, meta.ReqID, it.Ext)
		if err := ev.Validate(); err != nil {
			rejected[ev.EventID] = "invalid"
			continue
		}
		if reason := validateClientFields(ev); reason != "" {
			rejected[ev.EventID] = reason
			continue
		}
		feedIDSet[it.FeedId] = struct{}{}
		pendings = append(pendings, pending{eventID: ev.EventID, ev: ev})
	}

	// 批量校验 feed 存在且 status==NORMAL（SHARE 不做 owner 校验）。
	// Feed RPC 失败属基础设施错误，直接向上传播（区别于 feed_not_found 的局部拒绝）。
	feedInfo, err := l.batchGetNormalFeeds(feedIDSet)
	if err != nil {
		l.Errorf("report behaviors batch get feeds failed err=%v", err)
		return nil, err
	}

	for _, p := range pendings {
		info, ok := feedInfo[p.ev.FeedID]
		if !ok || feedpb.FeedStatus(info.Status) != feedpb.FeedStatus_FEED_STATUS_NORMAL {
			rejected[p.eventID] = "feed_not_found"
			continue
		}
		body, err := p.ev.ToJSON()
		if err != nil {
			rejected[p.eventID] = "marshal"
			continue
		}
		if err := l.svcCtx.Producer.SendSync(bhv.TopicFeedBehaviorEvent, body); err != nil {
			l.Errorf("report behaviors mq send failed uid=%d action=%s feed=%d err=%v", me, p.ev.Action, p.ev.FeedID, err)
			rejected[p.eventID] = "mq_error"
			continue
		}
		accepted = append(accepted, p.eventID)
	}

	return &types.ReportBehaviorsResp{Accepted: accepted, Rejected: rejected}, nil
}

// feedBatchLimit 单次 BatchGetFeeds 下游允许的最大 feed 数（见 batchgetfeedslogic.go）。
const feedBatchLimit = 100

func (l *ReportBehaviorsLogic) batchGetNormalFeeds(set map[int64]struct{}) (map[int64]*feedClient.FeedInfo, error) {
	if len(set) == 0 {
		return nil, nil
	}
	ids := make([]int64, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	out := make(map[int64]*feedClient.FeedInfo, len(ids))
	// 分批调用，避免超过下游 BatchGetFeeds 的批量上限。
	for start := 0; start < len(ids); start += feedBatchLimit {
		end := start + feedBatchLimit
		if end > len(ids) {
			end = len(ids)
		}
		resp, err := l.svcCtx.FeedRpc.BatchGetFeeds(l.ctx, &feedClient.BatchGetFeedsReq{
			FeedIds: ids[start:end],
		})
		if err != nil {
			return nil, err
		}
		for k, v := range resp.Feeds {
			out[k] = v
		}
	}
	return out, nil
}
