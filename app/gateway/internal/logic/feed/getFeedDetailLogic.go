// getfeeddetaillogic.go
//
// 职责：帖子详情页 BFF 聚合。
// 先取帖子基础数据，再用 errgroup 并行聚合：
//   - User.GetUser                        作者信息（失败则整体失败）；
//   - Relation.IsFollow                   viewer 是否关注作者（失败降级 false）；
//   - Interaction.GetFeedStats            点赞/收藏计数（失败降级 Feed 镜像值）；
//   - Interaction.GetUserInteractionStatus viewer 互动状态（失败降级 false）。
package feed

import (
	"context"

	feedClient "github.com/sponge-dad/feed/app/feed/rpc/feedclient"
	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	interactionClient "github.com/sponge-dad/feed/app/interaction/rpc/interactionClient"
	relationClient "github.com/sponge-dad/feed/app/relation/rpc/relationclient"
	userClient "github.com/sponge-dad/feed/app/user/rpc/userClient"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
	"golang.org/x/sync/errgroup"
)

type GetFeedDetailLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetFeedDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetFeedDetailLogic {
	return &GetFeedDetailLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// GetFeedDetail 帖子详情。
func (l *GetFeedDetailLogic) GetFeedDetail(req *types.GetFeedDetailReq) (*types.FeedDetail, error) {
	me := middleware.MustGetUserID(l.ctx)
	if me == 0 {
		return nil, errorx.New(errorx.Unauthorized)
	}
	if req.FeedID <= 0 {
		return nil, errorx.NewWithMsg(errorx.ParamError, "feedId 非法")
	}

	getResp, err := l.svcCtx.FeedRpc.GetFeed(l.ctx, &feedClient.GetFeedReq{
		FeedId: req.FeedID,
		UserId: me,
	})
	if err != nil {
		return nil, err
	}
	f := getResp.Feed
	if f == nil {
		return nil, errorx.New(errorx.FeedNotFound)
	}

	detail := &types.FeedDetail{
		ID:          f.FeedId,
		FeedType:    f.FeedType,
		Title:       f.Title,
		Description: f.Description,
		MediaUrls:   f.MediaUrls,
		CoverURL:    f.CoverUrl,
		CityName:    f.CityName,
		IPLocation:  f.IpLocation,
		CreatedAt:   f.CreatedAt,
		// 计数镜像值兜底，Interaction 成功后覆盖
		Stats: types.FeedStatsInfo{
			LikeCount:    f.LikeCount,
			CommentCount: f.CommentCount,
			CollectCount: f.CollectCount,
		},
	}

	g, gctx := errgroup.WithContext(l.ctx)

	// 作者信息：失败则整体失败。
	g.Go(func() error {
		resp, uerr := l.svcCtx.UserRpc.GetUser(gctx, &userClient.GetUserReq{UserId: f.AuthorId})
		if uerr != nil {
			return uerr
		}
		if resp.User != nil {
			detail.Author.ID = resp.User.Id
			detail.Author.Nickname = resp.User.Nickname
			detail.Author.Avatar = resp.User.Avatar
		}
		return nil
	})

	// 是否关注作者：失败降级 false。
	g.Go(func() error {
		resp, ferr := l.svcCtx.RelationRpc.IsFollow(gctx, &relationClient.IsFollowReq{
			FollowerId:  me,
			FolloweeIds: []int64{f.AuthorId},
		})
		if ferr != nil {
			l.Errorf("feed: IsFollow degrade to false: %v", ferr)
			return nil
		}
		detail.Author.IsFollowing = resp.Results[f.AuthorId]
		return nil
	})

	// 点赞/收藏计数：失败降级镜像值。
	g.Go(func() error {
		resp, serr := l.svcCtx.InteractionRpc.GetFeedStats(gctx, &interactionClient.GetFeedStatsReq{FeedId: f.FeedId})
		if serr != nil {
			l.Errorf("feed: GetFeedStats degrade to mirror counts: %v", serr)
			return nil
		}
		if resp.Stats != nil {
			detail.Stats.LikeCount = resp.Stats.LikeCount
			detail.Stats.CollectCount = resp.Stats.CollectCount
		}
		return nil
	})

	// viewer 互动状态：失败降级 false。
	g.Go(func() error {
		resp, serr := l.svcCtx.InteractionRpc.GetUserInteractionStatus(gctx, &interactionClient.GetUserInteractionStatusReq{
			UserId: me,
			FeedId: f.FeedId,
		})
		if serr != nil {
			l.Errorf("feed: GetUserInteractionStatus degrade to false: %v", serr)
			return nil
		}
		if resp.Status != nil {
			detail.Interaction.IsLiked = resp.Status.IsLiked
			detail.Interaction.IsCollected = resp.Status.IsCollected
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}
	return detail, nil
}
