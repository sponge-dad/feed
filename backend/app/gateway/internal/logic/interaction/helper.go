// helper.go
//
// 职责：interaction 模块 logic 的公共逻辑。
//   - fetchStats：写操作（赞/收藏）成功后 best-effort 回查最新计数；
//   - buildFeedCardsByIDs：「我的赞/我的收藏」由 feed_id 列表批量取帖子后聚合 FeedCard。
package interaction

import (
	"context"

	feedClient "github.com/sponge-dad/feed/app/feed/rpc/feedclient"
	"github.com/sponge-dad/feed/app/gateway/internal/logic/aggregate"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	interactionClient "github.com/sponge-dad/feed/app/interaction/rpc/interactionClient"

	"github.com/zeromicro/go-zero/core/logx"
)

// fetchStats best-effort 获取帖子最新点赞/收藏计数，失败返回 nil 并记录日志。
func fetchStats(ctx context.Context, svcCtx *svc.ServiceContext, feedID int64) *interactionClient.FeedStats {
	resp, err := svcCtx.InteractionRpc.GetFeedStats(ctx, &interactionClient.GetFeedStatsReq{FeedId: feedID})
	if err != nil {
		logx.WithContext(ctx).Errorf("interaction: GetFeedStats of %d degrade to 0: %v", feedID, err)
		return nil
	}
	return resp.Stats
}

// buildFeedCardsByIDs 按 feed_id 列表批量获取帖子并聚合为 FeedCard 列表。
// 已删除的帖子（BatchGetFeeds 未返回）会被跳过，顺序与 feedIDs 保持一致。
func buildFeedCardsByIDs(ctx context.Context, svcCtx *svc.ServiceContext, viewerID int64, feedIDs []int64) ([]types.FeedCard, error) {
	if len(feedIDs) == 0 {
		return []types.FeedCard{}, nil
	}

	batchResp, err := svcCtx.FeedRpc.BatchGetFeeds(ctx, &feedClient.BatchGetFeedsReq{
		FeedIds: feedIDs,
		UserId:  viewerID,
	})
	if err != nil {
		return nil, err
	}

	items := make([]aggregate.FeedItem, 0, len(feedIDs))
	for _, id := range feedIDs {
		f, ok := batchResp.Feeds[id]
		if !ok || f == nil {
			logx.WithContext(ctx).Infof("interaction: skip feed %d, not found (maybe deleted)", id)
			continue
		}
		items = append(items, aggregate.ItemFromInfo(f))
	}

	return aggregate.BuildFeedCards(ctx, svcCtx, viewerID, items)
}
