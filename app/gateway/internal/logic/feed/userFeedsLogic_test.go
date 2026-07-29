// userFeedsLogic_test.go
//
// 职责：UserFeedsLogic 单元测试，验证鉴权/参数校验与 BuildFeedCards 聚合。
package feed

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"

	feedClient "github.com/sponge-dad/feed/app/feed/rpc/feedclient"
	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/mocks"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	interactionClient "github.com/sponge-dad/feed/app/interaction/rpc/interactionClient"
	userClient "github.com/sponge-dad/feed/app/user/rpc/userClient"
	"github.com/sponge-dad/feed/common/errorx"
)

// TestUserFeeds_Success_BuildsCards 验证列表聚合：Feed 列表经 BuildFeedCards 补全作者/计数/互动。
func TestUserFeeds_Success_BuildsCards(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx := mocks.NewTestServiceContext(ctrl)
	feedRpc := svcCtx.FeedRpc.(*mocks.MockFeed)
	userRpc := svcCtx.UserRpc.(*mocks.MockUser)
	interactionRpc := svcCtx.InteractionRpc.(*mocks.MockInteraction)

	feedRpc.EXPECT().GetUserFeeds(gomock.Any(), gomock.Any()).Return(&feedClient.GetUserFeedsResp{
		Feeds: []*feedClient.FeedBrief{{FeedId: 101, AuthorId: 11, FeedType: 1, Title: "t", CoverUrl: "c", LikeCount: 3}},
		Page:  &feedClient.PageInfo{HasMore: true, Cursor: "2", Page: 1, PageSize: 20},
	}, nil)
	userRpc.EXPECT().BatchGetUsers(gomock.Any(), gomock.Any()).Return(&userClient.BatchGetUsersResp{
		Users: []*userClient.UserBrief{{Id: 11, Nickname: "a11", Avatar: "av11"}},
	}, nil)
	interactionRpc.EXPECT().BatchGetFeedStats(gomock.Any(), gomock.Any()).Return(&interactionClient.BatchGetFeedStatsResp{
		StatsList: []*interactionClient.FeedStats{{FeedId: 101, LikeCount: 3, CollectCount: 1}},
	}, nil)
	interactionRpc.EXPECT().BatchGetUserInteractionStatus(gomock.Any(), gomock.Any()).Return(&interactionClient.BatchGetUserInteractionStatusResp{
		StatusList: []*interactionClient.UserInteractionStatus{{FeedId: 101, IsLiked: true, IsCollected: false}},
	}, nil)

	resp, err := NewUserFeedsLogic(middleware.WithUserID(context.Background(), 5), svcCtx).UserFeeds(&types.UserFeedsReq{
		UserID: 11, Cursor: "", PageSize: 20,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.List) != 1 {
		t.Fatalf("expected 1 card, got %d", len(resp.List))
	}
	card := resp.List[0]
	if card.ID != 101 || card.Author.ID != 11 || card.Author.Nickname != "a11" {
		t.Errorf("card mapping mismatch: %+v", card)
	}
	if !card.Interaction.IsLiked || card.Stats.LikeCount != 3 {
		t.Errorf("card interaction/stats mismatch: %+v %+v", card.Interaction, card.Stats)
	}
	// 空 cursor 表示首页(page=1)，下游返回 HasMore=true 且 cursor="2"，网关应透传。
	if resp.NextCursor != "2" || !resp.HasMore {
		t.Errorf("pagination mismatch: cursor=%s hasMore=%v", resp.NextCursor, resp.HasMore)
	}
}

// TestUserFeeds_Unauthorized_ReturnsUnauthorized 验证未登录直接拒绝。
func TestUserFeeds_Unauthorized_ReturnsUnauthorized(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx := mocks.NewTestServiceContext(ctrl)

	_, err := NewUserFeedsLogic(context.Background(), svcCtx).UserFeeds(&types.UserFeedsReq{UserID: 11})
	ce, ok := errorx.TryParse(err)
	if !ok || ce.Code != errorx.Unauthorized {
		t.Fatalf("expected Unauthorized, got %v", err)
	}
}

// TestUserFeeds_InvalidUserID_ReturnsParamError 验证非法 UserID 被拒绝。
func TestUserFeeds_InvalidUserID_ReturnsParamError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx := mocks.NewTestServiceContext(ctrl)

	_, err := NewUserFeedsLogic(middleware.WithUserID(context.Background(), 5), svcCtx).UserFeeds(&types.UserFeedsReq{UserID: 0})
	ce, ok := errorx.TryParse(err)
	if !ok || ce.Code != errorx.ParamError {
		t.Fatalf("expected ParamError, got %v", err)
	}
}
