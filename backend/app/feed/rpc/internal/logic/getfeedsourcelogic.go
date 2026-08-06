package logic

import (
	"context"
	"errors"
	"strconv"

	"github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/keys"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

type GetFeedSourceLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetFeedSourceLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetFeedSourceLogic {
	return &GetFeedSourceLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetFeedSource 查询某次请求中某条 feed 的来源标记（见 02-request-trace §6.3）。
// 仅本人或内部用户可查询；未命中返回 FEED_SOURCE_UNKNOWN（未写入/已过期）。
func (l *GetFeedSourceLogic) GetFeedSource(in *feed.GetFeedSourceReq) (*feed.GetFeedSourceResp, error) {
	if in.RequestId == "" || in.FeedId == 0 {
		return nil, errorx.New(errorx.ParamError)
	}

	// 归属校验：仅本人或内部用户可查询。
	if owner, ok, err := loadTraceUserID(l.svcCtx.Redis, l.ctx, in.RequestId); err != nil {
		return nil, err
	} else if ok && owner != in.UserId && !isInternalUser(l.svcCtx, in.UserId) {
		return nil, errorx.New(errorx.Forbidden)
	}

	got, err := l.svcCtx.Redis.HgetCtx(l.ctx, keys.FeedTraceKey(in.RequestId), "f:"+strconv.FormatInt(in.FeedId, 10))
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return &feed.GetFeedSourceResp{Source: feed.FeedSource_FEED_SOURCE_UNKNOWN}, nil
		}
		return nil, err
	}

	val, ok := feed.FeedSource_value[got]
	if !ok {
		return &feed.GetFeedSourceResp{Source: feed.FeedSource_FEED_SOURCE_UNKNOWN}, nil
	}
	return &feed.GetFeedSourceResp{Source: feed.FeedSource(val)}, nil
}
