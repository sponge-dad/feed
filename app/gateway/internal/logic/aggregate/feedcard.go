// Package aggregate 提供 Gateway BFF 聚合的公共能力：
//   - FeedCard 批量组装（User / Interaction 并行批量调用，禁止 N+1）；
//   - 信息流 cursor 分页与页码 cursor 的互转；
//   - page_size 边界约束。
//
// 被 logic/feed、logic/interaction、logic/comment 等模块包复用。
package aggregate

import (
	"context"
	"strconv"

	feedClient "github.com/sponge-dad/feed/app/feed/rpc/feedclient"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	interactionClient "github.com/sponge-dad/feed/app/interaction/rpc/interactionClient"
	userClient "github.com/sponge-dad/feed/app/user/rpc/userClient"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
	"golang.org/x/sync/errgroup"
)

// FeedItem FeedCard 组装所需的帖子基础字段，
// 由 FeedBrief（信息流）或 FeedInfo（批量详情）转换而来。
type FeedItem struct {
	FeedID       int64
	AuthorID     int64
	FeedType     int32
	Title        string
	CoverURL     string
	LikeCount    int64 // Feed 服务镜像计数，Interaction 批量失败时降级使用
	CommentCount int64
	CollectCount int64
	CreatedAt    int64
}

// ItemFromBrief 将 Feed RPC 的 FeedBrief 转为 FeedItem。
func ItemFromBrief(b *feedClient.FeedBrief) FeedItem {
	return FeedItem{
		FeedID:       b.FeedId,
		AuthorID:     b.AuthorId,
		FeedType:     b.FeedType,
		Title:        b.Title,
		CoverURL:     b.CoverUrl,
		LikeCount:    b.LikeCount,
		CommentCount: b.CommentCount,
		CreatedAt:    b.CreatedAt,
	}
}

// ItemFromInfo 将 Feed RPC 的 FeedInfo 转为 FeedItem。
func ItemFromInfo(f *feedClient.FeedInfo) FeedItem {
	return FeedItem{
		FeedID:       f.FeedId,
		AuthorID:     f.AuthorId,
		FeedType:     f.FeedType,
		Title:        f.Title,
		CoverURL:     f.CoverUrl,
		LikeCount:    f.LikeCount,
		CommentCount: f.CommentCount,
		CollectCount: f.CollectCount,
		CreatedAt:    f.CreatedAt,
	}
}

// ItemsFromBriefs 批量转换 FeedBrief。
func ItemsFromBriefs(briefs []*feedClient.FeedBrief) []FeedItem {
	items := make([]FeedItem, 0, len(briefs))
	for _, b := range briefs {
		if b == nil {
			continue
		}
		items = append(items, ItemFromBrief(b))
	}
	return items
}

// BuildFeedCards 将帖子基础数据聚合为 FeedCard 列表（BFF 核心）。
//
// 并行批量调用：
//   - User.BatchGetUsers    获取作者昵称/头像（失败则整体失败，卡片缺作者无意义）；
//   - Interaction.BatchGetFeedStats           获取点赞/收藏计数（失败降级为 Feed 镜像计数）；
//   - Interaction.BatchGetUserInteractionStatus 获取当前用户点赞/收藏状态（失败降级为 false）。
//
// 作者已不存在（注销）的帖子会被跳过。
func BuildFeedCards(ctx context.Context, svcCtx *svc.ServiceContext, viewerID int64, items []FeedItem) ([]types.FeedCard, error) {
	if len(items) == 0 {
		return []types.FeedCard{}, nil
	}

	feedIDs := make([]int64, 0, len(items))
	authorSet := make(map[int64]struct{}, len(items))
	authorIDs := make([]int64, 0, len(items))
	for _, it := range items {
		feedIDs = append(feedIDs, it.FeedID)
		if _, ok := authorSet[it.AuthorID]; !ok {
			authorSet[it.AuthorID] = struct{}{}
			authorIDs = append(authorIDs, it.AuthorID)
		}
	}

	var (
		userMap     = make(map[int64]*userClient.UserBrief, len(authorIDs))
		statsMap    = make(map[int64]*interactionClient.FeedStats, len(feedIDs))
		stateMap    = make(map[int64]*interactionClient.UserInteractionStatus, len(feedIDs))
		commentsMap = make(map[int64]int64, len(feedIDs))
	)

	g, gctx := errgroup.WithContext(ctx)

	// 作者信息：失败则整体失败。
	g.Go(func() error {
		resp, err := svcCtx.UserRpc.BatchGetUsers(gctx, &userClient.BatchGetUsersReq{UserIds: authorIDs})
		if err != nil {
			return err
		}
		for _, u := range resp.Users {
			if u != nil {
				userMap[u.Id] = u
			}
		}
		return nil
	})

	// 点赞/收藏计数：失败降级为 Feed 镜像计数。
	g.Go(func() error {
		resp, err := svcCtx.InteractionRpc.BatchGetFeedStats(gctx, &interactionClient.BatchGetFeedStatsReq{FeedIds: feedIDs})
		if err != nil {
			logx.WithContext(ctx).Errorf("aggregate: BatchGetFeedStats degrade to mirror counts: %v", err)
			return nil
		}
		for _, s := range resp.StatsList {
			if s != nil {
				statsMap[s.FeedId] = s
			}
		}
		return nil
	})

	// 评论计数：直接取 feeds.comment_count 镜像值（由 Feed Worker 消费 comment-event 增量维护）。
	// 无需额外调用 Comment RPC，避免循环依赖。
	for _, it := range items {
		commentsMap[it.FeedID] = it.CommentCount
	}

	// 当前用户互动状态：失败降级为 false。
	g.Go(func() error {
		resp, err := svcCtx.InteractionRpc.BatchGetUserInteractionStatus(gctx, &interactionClient.BatchGetUserInteractionStatusReq{
			UserId:  viewerID,
			FeedIds: feedIDs,
		})
		if err != nil {
			logx.WithContext(ctx).Errorf("aggregate: BatchGetUserInteractionStatus degrade to false: %v", err)
			return nil
		}
		for _, s := range resp.StatusList {
			if s != nil {
				stateMap[s.FeedId] = s
			}
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	cards := make([]types.FeedCard, 0, len(items))
	for _, it := range items {
		author, ok := userMap[it.AuthorID]
		if !ok {
			logx.WithContext(ctx).Infof("aggregate: skip feed %d, author %d not found", it.FeedID, it.AuthorID)
			continue
		}

		card := types.FeedCard{
			ID:       it.FeedID,
			FeedType: it.FeedType,
			Title:    it.Title,
			CoverURL: it.CoverURL,
			Author: types.FeedAuthor{
				ID:       author.Id,
				Nickname: author.Nickname,
				Avatar:   author.Avatar,
			},
			Stats: types.FeedStatsInfo{
				LikeCount:    it.LikeCount,
				CommentCount: it.CommentCount,
				CollectCount: it.CollectCount,
			},
			CreatedAt: it.CreatedAt,
		}
		if s, ok := statsMap[it.FeedID]; ok {
			card.Stats.LikeCount = s.LikeCount
			card.Stats.CollectCount = s.CollectCount
		}
		if c, ok := commentsMap[it.FeedID]; ok {
			card.Stats.CommentCount = c
		}
		if st, ok := stateMap[it.FeedID]; ok {
			card.Interaction = types.FeedInteractionInfo{
				IsLiked:     st.IsLiked,
				IsCollected: st.IsCollected,
			}
		}
		cards = append(cards, card)
	}
	return cards, nil
}

// ClampPageSize 约束 page_size：非法或缺省取 def，超过 max 取 max。
func ClampPageSize(size, def, max int64) int64 {
	if size <= 0 {
		return def
	}
	if size > max {
		return max
	}
	return size
}

// PageFromCursor 将信息流 cursor 转换为页码（下游为 page 分页的场景）。
// 空 cursor 表示第一页；非法 cursor 返回参数错误。
func PageFromCursor(cursor string) (int64, error) {
	if cursor == "" {
		return 1, nil
	}
	page, err := strconv.ParseInt(cursor, 10, 64)
	if err != nil || page <= 0 {
		return 0, errorx.NewWithMsg(errorx.ParamError, "cursor 非法")
	}
	return page, nil
}

// NextPageCursor 根据当前页码与是否还有下一页生成 next_cursor。
func NextPageCursor(page int64, hasMore bool) string {
	if !hasMore {
		return ""
	}
	return strconv.FormatInt(page+1, 10)
}
