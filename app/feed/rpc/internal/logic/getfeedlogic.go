// Package logic 实现 Feed 服务的所有 gRPC 业务逻辑。
// getfeedlogic.go 提供单个帖子详情查询，采用 feed:{id} Hash 的 cache-aside 读取。
package logic

import (
	"context"
	"errors"

	"github.com/sponge-dad/feed/app/feed/model"
	"github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"
	"github.com/zeromicro/go-zero/core/logx"
)

// GetFeedLogic 封装 GetFeed 请求所需的上下文与依赖。
type GetFeedLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

// NewGetFeedLogic 构造 GetFeedLogic。
func NewGetFeedLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetFeedLogic {
	return &GetFeedLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetFeed 查询单个帖子详情。
// 读路径：先查业务缓存 feed:{id} Hash（cache-aside），未命中再回源 DB 并异步回写。
func (l *GetFeedLogic) GetFeed(in *feed.GetFeedReq) (*feed.GetFeedResp, error) {
	if in.FeedId <= 0 {
		return nil, errorx.New(errorx.ParamError)
	}

	// 1. 缓存命中直接返回（详见 06-cache-strategy.md §3.1）。
	if info, hit := loadFeedDetail(l.svcCtx.Redis, l.ctx, in.FeedId); hit {
		return &feed.GetFeedResp{Feed: info}, nil
	}

	// 2. 缓存未命中，回源 DB。
	f, err := l.svcCtx.FeedModel.FindOne(l.ctx, uint64(in.FeedId))
	if err == nil {
		info := toFeedInfo(f)
		// 3. 异步回写缓存，不阻塞主流程。
		cacheFeedDetail(l.svcCtx.Redis, l.ctx, info)
		return &feed.GetFeedResp{Feed: info}, nil
	}

	// 帖子不存在：转换为业务错误码（禁止裸 errors.New 透传）。
	if errors.Is(err, model.ErrNotFound) {
		return nil, errorx.New(errorx.FeedNotFound)
	}
	return nil, err
}
