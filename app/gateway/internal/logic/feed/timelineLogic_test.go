// timelinelogic_test.go
//
// 职责：Timeline（三流合一）logic 单元测试：
// recommend 页码 cursor、follow 原生 cursor 透传、city IP 定位失败 12006、
// 参数错误、下游 RPC 失败/超时透传。
package feed

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
	"github.com/sponge-dad/feed/common/ipx"

	"github.com/golang/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// newTestEnv 构造 mock 环境与已登录 ctx。
func newTestEnv(ctrl *gomock.Controller, uid int64) (context.Context, *svc.ServiceContext, *mocks.MockUser, *mocks.MockFeed, *mocks.MockInteraction) {
	userRpc := mocks.NewMockUser(ctrl)
	feedRpc := mocks.NewMockFeed(ctrl)
	interactionRpc := mocks.NewMockInteraction(ctrl)
	relationRpc := mocks.NewMockRelation(ctrl)
	svcCtx := &svc.ServiceContext{
		UserRpc:        userRpc,
		FeedRpc:        feedRpc,
		InteractionRpc: interactionRpc,
		RelationRpc:    relationRpc,
		IPResolver:     ipx.NewStaticResolver(nil), // 默认不可定位
	}
	ctx := middleware.WithUserID(context.Background(), uid)
	return ctx, svcCtx, userRpc, feedRpc, interactionRpc
}

// expectAggregation 设置一次成功的 FeedCard 聚合期望。
func expectAggregation(userRpc *mocks.MockUser, interactionRpc *mocks.MockInteraction) {
	userRpc.EXPECT().BatchGetUsers(gomock.Any(), gomock.Any()).Return(&userClient.BatchGetUsersResp{
		Users: []*userClient.UserBrief{{Id: 11, Nickname: "u11", Avatar: "a"}},
	}, nil)
	interactionRpc.EXPECT().BatchGetFeedStats(gomock.Any(), gomock.Any()).Return(&interactionClient.BatchGetFeedStatsResp{}, nil)
	interactionRpc.EXPECT().BatchGetUserInteractionStatus(gomock.Any(), gomock.Any()).Return(&interactionClient.BatchGetUserInteractionStatusResp{}, nil)
}

func TestTimeline_RecommendPageCursor(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx, svcCtx, userRpc, feedRpc, interactionRpc := newTestEnv(ctrl, 1)

	feedRpc.EXPECT().GetRecommendTimeline(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, req *feedClient.GetRecommendTimelineReq, _ ...interface{}) (*feedClient.GetRecommendTimelineResp, error) {
			if req.Page != 2 || req.PageSize != 10 || req.UserId != 1 {
				t.Errorf("unexpected req: %+v", req)
			}
			return &feedClient.GetRecommendTimelineResp{
				Feeds: []*feedClient.FeedBrief{{FeedId: 101, AuthorId: 11, Title: "t"}},
				Page:  &feedClient.PageInfo{HasMore: true},
			}, nil
		})
	expectAggregation(userRpc, interactionRpc)

	resp, err := NewTimelineLogic(ctx, svcCtx).Timeline(&types.TimelineReq{Type: "recommend", Cursor: "2", PageSize: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.NextCursor != "3" || !resp.HasMore || len(resp.List) != 1 {
		t.Errorf("unexpected resp: %+v", resp)
	}
}

func TestTimeline_FollowCursorPassthrough(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx, svcCtx, userRpc, feedRpc, interactionRpc := newTestEnv(ctrl, 1)

	feedRpc.EXPECT().GetFollowTimeline(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, req *feedClient.GetFollowTimelineReq, _ ...interface{}) (*feedClient.GetFollowTimelineResp, error) {
			if req.Cursor != "1690000000000_123" {
				t.Errorf("cursor should passthrough, got %q", req.Cursor)
			}
			return &feedClient.GetFollowTimelineResp{
				Feeds: []*feedClient.FeedBrief{{FeedId: 101, AuthorId: 11}},
				Page:  &feedClient.PageInfo{Cursor: "1680000000000_456", HasMore: true},
			}, nil
		})
	expectAggregation(userRpc, interactionRpc)

	resp, err := NewTimelineLogic(ctx, svcCtx).Timeline(&types.TimelineReq{Type: "follow", Cursor: "1690000000000_123", PageSize: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.NextCursor != "1680000000000_456" || !resp.HasMore {
		t.Errorf("unexpected resp: %+v", resp)
	}
}

func TestTimeline_CityIPLocateFail(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx, svcCtx, _, _, _ := newTestEnv(ctrl, 1) // IPResolver 无默认城市，必然定位失败

	_, err := NewTimelineLogic(ctx, svcCtx).Timeline(&types.TimelineReq{Type: "city", PageSize: 10})
	ce, ok := err.(*errorx.CodeError)
	if !ok || ce.Code != errorx.FeedIPLocateFail {
		t.Fatalf("want code %d, got %v", errorx.FeedIPLocateFail, err)
	}
}

func TestTimeline_CityWithDefaultCity(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx, svcCtx, userRpc, feedRpc, interactionRpc := newTestEnv(ctrl, 1)
	svcCtx.IPResolver = ipx.NewStaticResolver(&ipx.Location{CityCode: "440300", CityName: "深圳"})
	ctx = ipx.WithClientIP(ctx, "203.0.113.9")

	feedRpc.EXPECT().GetCityTimeline(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, req *feedClient.GetCityTimelineReq, _ ...interface{}) (*feedClient.GetCityTimelineResp, error) {
			if req.CityCode != "440300" || req.Page != 1 {
				t.Errorf("unexpected req: %+v", req)
			}
			return &feedClient.GetCityTimelineResp{
				Feeds: []*feedClient.FeedBrief{{FeedId: 101, AuthorId: 11}},
				Page:  &feedClient.PageInfo{HasMore: false},
			}, nil
		})
	expectAggregation(userRpc, interactionRpc)

	resp, err := NewTimelineLogic(ctx, svcCtx).Timeline(&types.TimelineReq{Type: "city", PageSize: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.HasMore || resp.NextCursor != "" {
		t.Errorf("unexpected resp: %+v", resp)
	}
}

func TestTimeline_BadCursor(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx, svcCtx, _, _, _ := newTestEnv(ctrl, 1)

	_, err := NewTimelineLogic(ctx, svcCtx).Timeline(&types.TimelineReq{Type: "recommend", Cursor: "abc"})
	ce, ok := err.(*errorx.CodeError)
	if !ok || ce.Code != errorx.ParamError {
		t.Fatalf("want ParamError, got %v", err)
	}
}

func TestTimeline_Unauthorized(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	_, svcCtx, _, _, _ := newTestEnv(ctrl, 1)

	_, err := NewTimelineLogic(context.Background(), svcCtx).Timeline(&types.TimelineReq{Type: "recommend"})
	ce, ok := err.(*errorx.CodeError)
	if !ok || ce.Code != errorx.Unauthorized {
		t.Fatalf("want Unauthorized, got %v", err)
	}
}

func TestTimeline_RpcTimeout(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx, svcCtx, _, feedRpc, _ := newTestEnv(ctrl, 1)

	feedRpc.EXPECT().GetRecommendTimeline(gomock.Any(), gomock.Any()).
		Return(nil, status.Error(codes.DeadlineExceeded, "context deadline exceeded"))

	_, err := NewTimelineLogic(ctx, svcCtx).Timeline(&types.TimelineReq{Type: "recommend"})
	if err == nil {
		t.Fatal("rpc timeout should propagate")
	}
}
