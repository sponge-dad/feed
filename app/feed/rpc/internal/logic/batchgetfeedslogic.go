// Package logic 实现 Feed 服务的所有 gRPC 业务逻辑。
// batchgetfeedslogic.go 提供帖子批量查询，用于主页流/评论等场景的 N+1 查询优化。
package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/svc"
	"github.com/zeromicro/go-zero/core/logx"
)

// maxBatchFeedIDs 批量查询单次上限，防止一次传入过多 ID 拖垮 DB。
const maxBatchFeedIDs = 100

// BatchGetFeedsLogic 封装 BatchGetFeeds 请求所需的上下文与依赖。
type BatchGetFeedsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

// NewBatchGetFeedsLogic 构造 BatchGetFeedsLogic。
func NewBatchGetFeedsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BatchGetFeedsLogic {
	return &BatchGetFeedsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// BatchGetFeeds 批量查询帖子详情，返回 feedId -> FeedInfo 的映射。
// 未找到（不存在或已删除）的 ID 不会出现在结果中；空请求返回空映射。
func (l *BatchGetFeedsLogic) BatchGetFeeds(in *feed.BatchGetFeedsReq) (*feed.BatchGetFeedsResp, error) {
	ids := in.FeedIds
	if len(ids) == 0 {
		return &feed.BatchGetFeedsResp{Feeds: map[int64]*feed.FeedInfo{}}, nil
	}
	if len(ids) > maxBatchFeedIDs {
		ids = ids[:maxBatchFeedIDs]
	}
	uintIDs := make([]uint64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		uintIDs = append(uintIDs, uint64(id))
	}
	if len(uintIDs) == 0 {
		return &feed.BatchGetFeedsResp{Feeds: map[int64]*feed.FeedInfo{}}, nil
	}

	feeds, err := l.svcCtx.FeedModel.FindByIds(l.ctx, uintIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[int64]*feed.FeedInfo, len(feeds))
	for _, f := range feeds {
		result[int64(f.Id)] = toFeedInfo(f)
	}
	return &feed.BatchGetFeedsResp{Feeds: result}, nil
}
