// feedcard_test.go
//
// 职责：BuildFeedCards 聚合逻辑与 cursor 分页工具的单元测试。
// 使用 gomock 模拟下游 RPC Client，不依赖真实服务。
package aggregate

import (
	"context"
	"testing"

	feedClient "github.com/sponge-dad/feed/app/feed/rpc/feedclient"
	"github.com/sponge-dad/feed/app/gateway/internal/mocks"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	interactionClient "github.com/sponge-dad/feed/app/interaction/rpc/interactionClient"
	userClient "github.com/sponge-dad/feed/app/user/rpc/userClient"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/golang/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// newTestSvcCtx 构造注入 mock 客户端的 ServiceContext。
func newTestSvcCtx(ctrl *gomock.Controller) (*svc.ServiceContext, *mocks.MockUser, *mocks.MockRelation, *mocks.MockFeed, *mocks.MockInteraction) {
	userRpc := mocks.NewMockUser(ctrl)
	relationRpc := mocks.NewMockRelation(ctrl)
	feedRpc := mocks.NewMockFeed(ctrl)
	interactionRpc := mocks.NewMockInteraction(ctrl)
	svcCtx := &svc.ServiceContext{
		UserRpc:        userRpc,
		RelationRpc:    relationRpc,
		FeedRpc:        feedRpc,
		InteractionRpc: interactionRpc,
	}
	return svcCtx, userRpc, relationRpc, feedRpc, interactionRpc
}

func briefs() []*feedClient.FeedBrief {
	return []*feedClient.FeedBrief{
		{FeedId: 101, AuthorId: 11, FeedType: 1, Title: "t1", CoverUrl: "c1", LikeCount: 5, CommentCount: 2, CreatedAt: 1000},
		{FeedId: 102, AuthorId: 12, FeedType: 2, Title: "t2", CoverUrl: "c2", LikeCount: 3, CommentCount: 1, CreatedAt: 2000},
	}
}

func TestBuildFeedCards_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx, userRpc, _, _, interactionRpc := newTestSvcCtx(ctrl)

	userRpc.EXPECT().BatchGetUsers(gomock.Any(), gomock.Any()).Return(&userClient.BatchGetUsersResp{
		Users: []*userClient.UserBrief{
			{Id: 11, Nickname: "u11", Avatar: "a11"},
			{Id: 12, Nickname: "u12", Avatar: "a12"},
		},
	}, nil)
	interactionRpc.EXPECT().BatchGetFeedStats(gomock.Any(), gomock.Any()).Return(&interactionClient.BatchGetFeedStatsResp{
		StatsList: []*interactionClient.FeedStats{
			{FeedId: 101, LikeCount: 50, CollectCount: 7},
		},
	}, nil)
	interactionRpc.EXPECT().BatchGetUserInteractionStatus(gomock.Any(), gomock.Any()).Return(&interactionClient.BatchGetUserInteractionStatusResp{
		StatusList: []*interactionClient.UserInteractionStatus{
			{FeedId: 101, IsLiked: true, IsCollected: false},
		},
	}, nil)

	cards, err := BuildFeedCards(context.Background(), svcCtx, 1, ItemsFromBriefs(briefs()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cards) != 2 {
		t.Fatalf("want 2 cards, got %d", len(cards))
	}
	// feed 101 使用 Interaction 实时计数与状态，评论计数取 Feed 镜像值
	if cards[0].Stats.LikeCount != 50 || cards[0].Stats.CollectCount != 7 || !cards[0].Interaction.IsLiked {
		t.Errorf("card 101 stats/interaction mismatch: %+v", cards[0])
	}
	if cards[0].Stats.CommentCount != 2 {
		t.Errorf("card 101 should use feed mirror comment count 2: %+v", cards[0].Stats)
	}
	// feed 102 无实时计数，降级为 Feed 镜像值
	if cards[1].Stats.LikeCount != 3 || cards[1].Interaction.IsLiked {
		t.Errorf("card 102 should use mirror counts: %+v", cards[1])
	}
	if cards[1].Stats.CommentCount != 1 {
		t.Errorf("card 102 should keep mirror comment count 1: %+v", cards[1].Stats)
	}
	if cards[0].Author.Nickname != "u11" || cards[1].Author.Nickname != "u12" {
		t.Errorf("author mismatch: %+v, %+v", cards[0].Author, cards[1].Author)
	}
}

func TestBuildFeedCards_InteractionDegrade(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx, userRpc, _, _, interactionRpc := newTestSvcCtx(ctrl)

	userRpc.EXPECT().BatchGetUsers(gomock.Any(), gomock.Any()).Return(&userClient.BatchGetUsersResp{
		Users: []*userClient.UserBrief{{Id: 11, Nickname: "u11"}, {Id: 12, Nickname: "u12"}},
	}, nil)
	rpcErr := status.Error(codes.DeadlineExceeded, "timeout")
	interactionRpc.EXPECT().BatchGetFeedStats(gomock.Any(), gomock.Any()).Return(nil, rpcErr)
	interactionRpc.EXPECT().BatchGetUserInteractionStatus(gomock.Any(), gomock.Any()).Return(nil, rpcErr)

	cards, err := BuildFeedCards(context.Background(), svcCtx, 1, ItemsFromBriefs(briefs()))
	if err != nil {
		t.Fatalf("interaction failure should degrade, got error: %v", err)
	}
	if len(cards) != 2 {
		t.Fatalf("want 2 cards, got %d", len(cards))
	}
	if cards[0].Stats.LikeCount != 5 || cards[0].Interaction.IsLiked {
		t.Errorf("should degrade to mirror counts and false state: %+v", cards[0])
	}
	if cards[0].Stats.CommentCount != 2 {
		t.Errorf("should keep mirror comment count 2: %+v", cards[0].Stats)
	}
}

func TestBuildFeedCards_UserRpcFail(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx, userRpc, _, _, interactionRpc := newTestSvcCtx(ctrl)

	userRpc.EXPECT().BatchGetUsers(gomock.Any(), gomock.Any()).Return(nil, status.Error(codes.Unavailable, "down"))
	interactionRpc.EXPECT().BatchGetFeedStats(gomock.Any(), gomock.Any()).Return(&interactionClient.BatchGetFeedStatsResp{}, nil).AnyTimes()
	interactionRpc.EXPECT().BatchGetUserInteractionStatus(gomock.Any(), gomock.Any()).Return(&interactionClient.BatchGetUserInteractionStatusResp{}, nil).AnyTimes()

	if _, err := BuildFeedCards(context.Background(), svcCtx, 1, ItemsFromBriefs(briefs())); err == nil {
		t.Fatal("user rpc failure should fail the aggregation")
	}
}

func TestBuildFeedCards_SkipMissingAuthor(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx, userRpc, _, _, interactionRpc := newTestSvcCtx(ctrl)

	// 只返回 11，author 12 已注销
	userRpc.EXPECT().BatchGetUsers(gomock.Any(), gomock.Any()).Return(&userClient.BatchGetUsersResp{
		Users: []*userClient.UserBrief{{Id: 11, Nickname: "u11"}},
	}, nil)
	interactionRpc.EXPECT().BatchGetFeedStats(gomock.Any(), gomock.Any()).Return(&interactionClient.BatchGetFeedStatsResp{}, nil)
	interactionRpc.EXPECT().BatchGetUserInteractionStatus(gomock.Any(), gomock.Any()).Return(&interactionClient.BatchGetUserInteractionStatusResp{}, nil)

	cards, err := BuildFeedCards(context.Background(), svcCtx, 1, ItemsFromBriefs(briefs()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cards) != 1 || cards[0].ID != 101 {
		t.Fatalf("should skip feed of missing author, got %+v", cards)
	}
}

func TestBuildFeedCards_Empty(t *testing.T) {
	cards, err := BuildFeedCards(context.Background(), &svc.ServiceContext{}, 1, nil)
	if err != nil || len(cards) != 0 {
		t.Fatalf("empty input should return empty list, got %v %v", cards, err)
	}
}

// TestBuildFeedCards_ZeroMirrorBaseline 验证 R-P1-5：互动服务无实时计数时计数回落为 Feed 镜像值；
// 当镜像值本身为 0 时，卡片计数必须显式为 0（不丢精度、不为脏值），互动状态默认 false。
func TestBuildFeedCards_ZeroMirrorBaseline(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx, userRpc, _, _, interactionRpc := newTestSvcCtx(ctrl)

	items := ItemsFromBriefs([]*feedClient.FeedBrief{
		{FeedId: 201, AuthorId: 21, FeedType: 1, Title: "zero", CoverUrl: "c", LikeCount: 0, CommentCount: 0, CreatedAt: 100},
	})
	userRpc.EXPECT().BatchGetUsers(gomock.Any(), gomock.Any()).Return(&userClient.BatchGetUsersResp{
		Users: []*userClient.UserBrief{{Id: 21, Nickname: "u21"}},
	}, nil)
	// 互动服务成功返回空统计（无 201 的实时数据） -> 走镜像降级
	interactionRpc.EXPECT().BatchGetFeedStats(gomock.Any(), gomock.Any()).Return(&interactionClient.BatchGetFeedStatsResp{}, nil)
	interactionRpc.EXPECT().BatchGetUserInteractionStatus(gomock.Any(), gomock.Any()).Return(&interactionClient.BatchGetUserInteractionStatusResp{}, nil)

	cards, err := BuildFeedCards(context.Background(), svcCtx, 1, items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("want 1 card, got %d", len(cards))
	}
	if cards[0].Stats.LikeCount != 0 || cards[0].Stats.CommentCount != 0 || cards[0].Stats.CollectCount != 0 {
		t.Errorf("R-P1-5: mirror 0 baseline mismatch: %+v", cards[0].Stats)
	}
	if cards[0].Interaction.IsLiked || cards[0].Interaction.IsCollected {
		t.Errorf("R-P1-5: interaction should default false: %+v", cards[0].Interaction)
	}
}

func TestPageFromCursor(t *testing.T) {
	cases := []struct {
		cursor  string
		want    int64
		wantErr bool
	}{
		{"", 1, false},
		{"3", 3, false},
		{"0", 0, true},
		{"-1", 0, true},
		{"abc", 0, true},
	}
	for _, c := range cases {
		got, err := PageFromCursor(c.cursor)
		if c.wantErr {
			if err == nil {
				t.Errorf("cursor %q should fail", c.cursor)
			} else if ce, ok := err.(*errorx.CodeError); !ok || ce.Code != errorx.ParamError {
				t.Errorf("cursor %q should return ParamError, got %v", c.cursor, err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("cursor %q: want %d, got %d err %v", c.cursor, c.want, got, err)
		}
	}
}

func TestNextPageCursorAndClamp(t *testing.T) {
	if NextPageCursor(1, true) != "2" || NextPageCursor(1, false) != "" {
		t.Error("NextPageCursor mismatch")
	}
	if ClampPageSize(0, 10, 50) != 10 || ClampPageSize(100, 10, 50) != 50 || ClampPageSize(20, 10, 50) != 20 {
		t.Error("ClampPageSize mismatch")
	}
}
