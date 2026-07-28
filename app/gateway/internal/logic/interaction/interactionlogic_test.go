// interactionlogic_test.go
//
// 职责：interaction 模块 logic 单元测试：
// 点赞幂等语义（下游成功即 success）、计数回查降级、我的赞列表（已删帖跳过、cursor 透传）。
package interaction

import (
	"context"
	"testing"

	feedClient "github.com/sponge-dad/feed/app/feed/rpc/feedclient"
	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/mocks"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	interactionClient "github.com/sponge-dad/feed/app/interaction/rpc/interactionClient"
	userClient "github.com/sponge-dad/feed/app/user/rpc/userClient"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/golang/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func newInteractionEnv(ctrl *gomock.Controller, uid int64) (context.Context, *svc.ServiceContext, *mocks.MockUser, *mocks.MockFeed, *mocks.MockInteraction) {
	userRpc := mocks.NewMockUser(ctrl)
	feedRpc := mocks.NewMockFeed(ctrl)
	interactionRpc := mocks.NewMockInteraction(ctrl)
	svcCtx := &svc.ServiceContext{UserRpc: userRpc, FeedRpc: feedRpc, InteractionRpc: interactionRpc}
	return middleware.WithUserID(context.Background(), uid), svcCtx, userRpc, feedRpc, interactionRpc
}

func TestLikeFeed_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx, svcCtx, _, _, interactionRpc := newInteractionEnv(ctrl, 1)

	interactionRpc.EXPECT().LikeFeed(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, req *interactionClient.LikeFeedReq, _ ...interface{}) (*interactionClient.LikeFeedResp, error) {
			if req.UserId != 1 || req.FeedId != 101 {
				t.Errorf("unexpected req: %+v", req)
			}
			return &interactionClient.LikeFeedResp{}, nil
		})
	interactionRpc.EXPECT().GetFeedStats(gomock.Any(), gomock.Any()).Return(&interactionClient.GetFeedStatsResp{
		Stats: &interactionClient.FeedStats{FeedId: 101, LikeCount: 9},
	}, nil)

	resp, err := NewLikeFeedLogic(ctx, svcCtx).LikeFeed(&types.LikeFeedReq{FeedID: 101})
	if err != nil || !resp.Success || resp.LikeCount != 9 {
		t.Fatalf("unexpected resp: %+v err %v", resp, err)
	}
}

func TestLikeFeed_StatsDegrade(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx, svcCtx, _, _, interactionRpc := newInteractionEnv(ctrl, 1)

	interactionRpc.EXPECT().LikeFeed(gomock.Any(), gomock.Any()).Return(&interactionClient.LikeFeedResp{}, nil)
	interactionRpc.EXPECT().GetFeedStats(gomock.Any(), gomock.Any()).Return(nil, status.Error(codes.Unavailable, "down"))

	resp, err := NewLikeFeedLogic(ctx, svcCtx).LikeFeed(&types.LikeFeedReq{FeedID: 101})
	if err != nil || !resp.Success || resp.LikeCount != 0 {
		t.Fatalf("stats failure should degrade to 0, got %+v err %v", resp, err)
	}
}

func TestLikeFeed_RpcFail(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx, svcCtx, _, _, interactionRpc := newInteractionEnv(ctrl, 1)

	interactionRpc.EXPECT().LikeFeed(gomock.Any(), gomock.Any()).
		Return(nil, errorx.ToGRPCError(errorx.New(errorx.InteractionFeedNotFound)))

	_, err := NewLikeFeedLogic(ctx, svcCtx).LikeFeed(&types.LikeFeedReq{FeedID: 101})
	if ce, ok := errorx.TryParse(err); !ok || ce.Code != errorx.InteractionFeedNotFound {
		t.Fatalf("business code should passthrough, got %v", err)
	}
}

func TestMyLikes_SkipDeletedFeed(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx, svcCtx, userRpc, feedRpc, interactionRpc := newInteractionEnv(ctrl, 1)

	interactionRpc.EXPECT().GetUserLikedFeeds(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, req *interactionClient.GetUserLikedFeedsReq, _ ...interface{}) (*interactionClient.GetUserLikedFeedsResp, error) {
			if req.UserId != 1 || req.Cursor != "abc_cursor" {
				t.Errorf("unexpected req: %+v", req)
			}
			return &interactionClient.GetUserLikedFeedsResp{
				FeedIds:    []int64{101, 102},
				NextCursor: "next_cursor",
				Total:      2,
			}, nil
		})
	// 102 已删除，BatchGetFeeds 不返回
	feedRpc.EXPECT().BatchGetFeeds(gomock.Any(), gomock.Any()).Return(&feedClient.BatchGetFeedsResp{
		Feeds: map[int64]*feedClient.FeedInfo{
			101: {FeedId: 101, AuthorId: 11, Title: "t1", LikeCount: 5},
		},
	}, nil)
	userRpc.EXPECT().BatchGetUsers(gomock.Any(), gomock.Any()).Return(&userClient.BatchGetUsersResp{
		Users: []*userClient.UserBrief{{Id: 11, Nickname: "u11"}},
	}, nil)
	interactionRpc.EXPECT().BatchGetFeedStats(gomock.Any(), gomock.Any()).Return(&interactionClient.BatchGetFeedStatsResp{}, nil)
	interactionRpc.EXPECT().BatchGetUserInteractionStatus(gomock.Any(), gomock.Any()).Return(&interactionClient.BatchGetUserInteractionStatusResp{}, nil)

	resp, err := NewMyLikesLogic(ctx, svcCtx).MyLikes(&types.MyLikesReq{Cursor: "abc_cursor", PageSize: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.List) != 1 || resp.List[0].ID != 101 {
		t.Errorf("deleted feed should be skipped: %+v", resp.List)
	}
	if resp.NextCursor != "next_cursor" || !resp.HasMore {
		t.Errorf("cursor should passthrough: %+v", resp)
	}
}

func TestMyCollects_Empty(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx, svcCtx, _, _, interactionRpc := newInteractionEnv(ctrl, 1)

	interactionRpc.EXPECT().GetUserCollectedFeeds(gomock.Any(), gomock.Any()).
		Return(&interactionClient.GetUserCollectedFeedsResp{FeedIds: []int64{}, NextCursor: ""}, nil)

	resp, err := NewMyCollectsLogic(ctx, svcCtx).MyCollects(&types.MyCollectsReq{PageSize: 10})
	if err != nil || len(resp.List) != 0 || resp.HasMore {
		t.Fatalf("unexpected resp: %+v err %v", resp, err)
	}
}

func TestMyLikes_Unauthorized(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	_, svcCtx, _, _, _ := newInteractionEnv(ctrl, 1)

	_, err := NewMyLikesLogic(context.Background(), svcCtx).MyLikes(&types.MyLikesReq{})
	ce, ok := err.(*errorx.CodeError)
	if !ok || ce.Code != errorx.Unauthorized {
		t.Fatalf("want Unauthorized, got %v", err)
	}
}
