package logic

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/keys"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/svc"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/logic/trace"
	"github.com/sponge-dad/feed/common/errorx"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

type GetFeedRequestTraceLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetFeedRequestTraceLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetFeedRequestTraceLogic {
	return &GetFeedRequestTraceLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetFeedRequestTrace 查询一次 Timeline 请求的完整 Trace（仅本人或内部用户可调用，见 02-request-trace §6.3）。
func (l *GetFeedRequestTraceLogic) GetFeedRequestTrace(in *feed.GetFeedRequestTraceReq) (*feed.GetFeedRequestTraceResp, error) {
	if in.RequestId == "" {
		return nil, errorx.New(errorx.ParamError)
	}

	meta, err := l.svcCtx.Redis.HgetCtx(l.ctx, keys.FeedTraceKey(in.RequestId), "meta")
	if err != nil {
		if errors.Is(err, redis.Nil) {
			// 未写入 Trace 或已过期：返回空 Trace。
			return &feed.GetFeedRequestTraceResp{}, nil
		}
		return nil, err
	}

	var t trace.FeedRequestTrace
	if err := json.Unmarshal([]byte(meta), &t); err != nil {
		return nil, errorx.NewWithMsg(errorx.ServerError, "trace meta corrupt: "+err.Error())
	}

	// 归属校验：仅本人或配置的内部用户可查询他人 Trace。
	if t.UserId != in.UserId && !isInternalUser(l.svcCtx, in.UserId) {
		return nil, errorx.New(errorx.Forbidden)
	}

	return &feed.GetFeedRequestTraceResp{Trace: &t}, nil
}

// loadTraceUserID 读取 Trace 的归属用户；ok=false 表示 key 不存在/已过期。
func loadTraceUserID(rdb *redis.Redis, ctx context.Context, requestID string) (int64, bool, error) {
	meta, err := rdb.HgetCtx(ctx, keys.FeedTraceKey(requestID), "meta")
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return 0, false, nil
		}
		return 0, false, err
	}
	var t trace.FeedRequestTrace
	if err := json.Unmarshal([]byte(meta), &t); err != nil {
		return 0, false, err
	}
	return t.UserId, true, nil
}

// isInternalUser 判断是否为配置的可越权查询的内部诊断用户。
func isInternalUser(svcCtx *svc.ServiceContext, uid int64) bool {
	for _, id := range svcCtx.Config.Trace.InternalUserIDs {
		if id == uid {
			return true
		}
	}
	return false
}
