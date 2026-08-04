// getFeedDetailLogic_test.go
//
// 职责：GetFeedDetailLogic 单元测试，验证聚合映射、计数升级(Stats)、互动降级语义。
package feed

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"

	feedClient "github.com/sponge-dad/feed/app/feed/rpc/feedclient"
	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/mocks"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	interactionClient "github.com/sponge-dad/feed/app/interaction/rpc/interactionClient"
	relationClient "github.com/sponge-dad/feed/app/relation/rpc/relationclient"
	userClient "github.com/sponge-dad/feed/app/user/rpc/userClient"
	"github.com/sponge-dad/feed/common/errorx"
)

// errDownstream 用于模拟互动服务降级（非业务错误码）。
var errDownstream = errors.New("downstream degraded")

// TestGetFeedDetail_Success_AggregatesAndUpgradeCounts 验证作者/关注关系/Stats 升级/互动状态映射正确。
func TestGetFeedDetail_Success_AggregatesAndUpgradeCounts(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx := mocks.NewTestServiceContext(ctrl)
	feedRpc := svcCtx.FeedRpc.(*mocks.MockFeed)
	userRpc := svcCtx.UserRpc.(*mocks.MockUser)
	relationRpc := svcCtx.RelationRpc.(*mocks.MockRelation)
	interactionRpc := svcCtx.InteractionRpc.(*mocks.MockInteraction)

	feedRpc.EXPECT().GetFeed(gomock.Any(), gomock.Any()).Return(&feedClient.GetFeedResp{Feed: &feedClient.FeedInfo{
		FeedId: 99, AuthorId: 7, FeedType: 1, Title: "t", LikeCount: 1, CommentCount: 2,
	}}, nil)
	userRpc.EXPECT().GetUser(gomock.Any(), gomock.Any()).Return(&userClient.GetUserResp{User: &userClient.UserInfo{Id: 7, Nickname: "a7", Avatar: "av7"}}, nil)
	relationRpc.EXPECT().IsFollow(gomock.Any(), gomock.Any()).Return(&relationClient.IsFollowResp{Results: map[int64]bool{7: true}}, nil)
	interactionRpc.EXPECT().GetFeedStats(gomock.Any(), gomock.Any()).Return(&interactionClient.GetFeedStatsResp{Stats: &interactionClient.FeedStats{FeedId: 99, LikeCount: 50, CollectCount: 9}}, nil)
	interactionRpc.EXPECT().GetUserInteractionStatus(gomock.Any(), gomock.Any()).Return(&interactionClient.GetUserInteractionStatusResp{Status: &interactionClient.UserInteractionStatus{IsLiked: true, IsCollected: false}}, nil)

	resp, err := NewGetFeedDetailLogic(middleware.WithUserID(context.Background(), 5), svcCtx).GetFeedDetail(&types.GetFeedDetailReq{FeedID: 99})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != 99 {
		t.Errorf("id mismatch: %d", resp.ID)
	}
	if resp.Author.ID != 7 || resp.Author.Nickname != "a7" || !resp.Author.IsFollowing {
		t.Errorf("author mapping mismatch: %+v", resp.Author)
	}
	// Stats.LikeCount 必须来自互动服务(50)，而非镜像(1)
	if resp.Stats.LikeCount != 50 || resp.Stats.CommentCount != 2 || resp.Stats.CollectCount != 9 {
		t.Errorf("stats mismatch: %+v", resp.Stats)
	}
	if !resp.Interaction.IsLiked || resp.Interaction.IsCollected {
		t.Errorf("interaction mismatch: %+v", resp.Interaction)
	}
}

// TestGetFeedDetail_Unauthorized_ReturnsUnauthorized 验证未登录直接拒绝。
func TestGetFeedDetail_Unauthorized_ReturnsUnauthorized(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx := mocks.NewTestServiceContext(ctrl)

	_, err := NewGetFeedDetailLogic(context.Background(), svcCtx).GetFeedDetail(&types.GetFeedDetailReq{FeedID: 99})
	ce, ok := errorx.TryParse(err)
	if !ok || ce.Code != errorx.Unauthorized {
		t.Fatalf("expected Unauthorized, got %v", err)
	}
}

// TestGetFeedDetail_InvalidFeedID_ReturnsParamError 验证非法 FeedID 被拒绝。
func TestGetFeedDetail_InvalidFeedID_ReturnsParamError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx := mocks.NewTestServiceContext(ctrl)

	_, err := NewGetFeedDetailLogic(middleware.WithUserID(context.Background(), 5), svcCtx).GetFeedDetail(&types.GetFeedDetailReq{FeedID: 0})
	ce, ok := errorx.TryParse(err)
	if !ok || ce.Code != errorx.ParamError {
		t.Fatalf("expected ParamError, got %v", err)
	}
}

// TestGetFeedDetail_NilFeed_ReturnsFeedNotFound 验证下游返回空 Feed 映射为 FeedNotFound。
func TestGetFeedDetail_NilFeed_ReturnsFeedNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx := mocks.NewTestServiceContext(ctrl)
	feedRpc := svcCtx.FeedRpc.(*mocks.MockFeed)

	feedRpc.EXPECT().GetFeed(gomock.Any(), gomock.Any()).Return(&feedClient.GetFeedResp{Feed: nil}, nil)

	_, err := NewGetFeedDetailLogic(middleware.WithUserID(context.Background(), 5), svcCtx).GetFeedDetail(&types.GetFeedDetailReq{FeedID: 99})
	ce, ok := errorx.TryParse(err)
	if !ok || ce.Code != errorx.FeedNotFound {
		t.Fatalf("expected FeedNotFound, got %v", err)
	}
}

// TestGetFeedDetail_InteractionDegrade 验证互动服务失败时降级到镜像计数，互动状态回落默认值。
func TestGetFeedDetail_InteractionDegrade(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx := mocks.NewTestServiceContext(ctrl)
	feedRpc := svcCtx.FeedRpc.(*mocks.MockFeed)
	userRpc := svcCtx.UserRpc.(*mocks.MockUser)
	relationRpc := svcCtx.RelationRpc.(*mocks.MockRelation)
	interactionRpc := svcCtx.InteractionRpc.(*mocks.MockInteraction)

	feedRpc.EXPECT().GetFeed(gomock.Any(), gomock.Any()).Return(&feedClient.GetFeedResp{Feed: &feedClient.FeedInfo{
		FeedId: 99, AuthorId: 7, FeedType: 1, Title: "t", LikeCount: 1, CommentCount: 2,
	}}, nil)
	userRpc.EXPECT().GetUser(gomock.Any(), gomock.Any()).Return(&userClient.GetUserResp{User: &userClient.UserInfo{Id: 7, Nickname: "a7"}}, nil)
	relationRpc.EXPECT().IsFollow(gomock.Any(), gomock.Any()).Return(&relationClient.IsFollowResp{Results: map[int64]bool{7: false}}, nil)
	interactionRpc.EXPECT().GetFeedStats(gomock.Any(), gomock.Any()).Return(nil, errDownstream)
	interactionRpc.EXPECT().GetUserInteractionStatus(gomock.Any(), gomock.Any()).Return(nil, errDownstream)

	resp, err := NewGetFeedDetailLogic(middleware.WithUserID(context.Background(), 5), svcCtx).GetFeedDetail(&types.GetFeedDetailReq{FeedID: 99})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 降级：LikeCount 回落为镜像值 1，互动状态为默认 false
	if resp.Stats.LikeCount != 1 || resp.Stats.CommentCount != 2 {
		t.Errorf("degraded stats mismatch: %+v", resp.Stats)
	}
	if resp.Interaction.IsLiked || resp.Interaction.IsCollected {
		t.Errorf("degraded interaction should be false: %+v", resp.Interaction)
	}
}
