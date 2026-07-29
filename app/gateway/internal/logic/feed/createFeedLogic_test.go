// createFeedLogic_test.go
//
// 职责：CreateFeedLogic 单元测试，验证鉴权/参数校验/请求映射与响应透传。
package feed

import (
	"context"
	"slices"
	"testing"

	"github.com/golang/mock/gomock"
	"google.golang.org/grpc"

	feedClient "github.com/sponge-dad/feed/app/feed/rpc/feedclient"
	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/mocks"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	"github.com/sponge-dad/feed/common/errorx"
	"github.com/sponge-dad/feed/common/ipx"
)

// TestCreateFeed_Success_ReturnsMappedFeed 验证鉴权通过时 AuthorId/媒体/IP属地映射正确并透传结果。
func TestCreateFeed_Success_ReturnsMappedFeed(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx := mocks.NewTestServiceContext(ctrl)
	feedRpc := svcCtx.FeedRpc.(*mocks.MockFeed)

	feedRpc.EXPECT().CreateFeed(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, in *feedClient.CreateFeedReq, _ ...grpc.CallOption) (*feedClient.CreateFeedResp, error) {
			if in.AuthorId != 5 {
				t.Errorf("AuthorId mapping mismatch: %d", in.AuthorId)
			}
			if !slices.Contains(in.MediaUrls, "u1.jpg") {
				t.Errorf("MediaUrls mapping mismatch: %v", in.MediaUrls)
			}
			if in.IpLocation == "" {
				t.Error("IpLocation should be resolved from client ip")
			}
			return &feedClient.CreateFeedResp{Feed: &feedClient.FeedInfo{FeedId: 99, AuthorId: 5}}, nil
		})

	resp, err := NewCreateFeedLogic(
		ipx.WithClientIP(middleware.WithUserID(context.Background(), 5), "1.2.3.4"), svcCtx,
	).CreateFeed(&types.CreateFeedReq{
		FeedType: 1, Title: "t", Description: "d", MediaUrls: []string{"u1.jpg"}, CoverURL: "c",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Feed.ID != 99 {
		t.Errorf("feed id mapping mismatch: %d", resp.Feed.ID)
	}
}

// TestCreateFeed_Unauthorized_ReturnsUnauthorized 验证未登录直接拒绝。
func TestCreateFeed_Unauthorized_ReturnsUnauthorized(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx := mocks.NewTestServiceContext(ctrl)

	_, err := NewCreateFeedLogic(context.Background(), svcCtx).CreateFeed(&types.CreateFeedReq{FeedType: 1})
	ce, ok := errorx.TryParse(err)
	if !ok || ce.Code != errorx.Unauthorized {
		t.Fatalf("expected Unauthorized, got %v", err)
	}
}

// TestCreateFeed_BadType_ReturnsFeedBadType 验证非法 FeedType 被拒绝。
func TestCreateFeed_BadType_ReturnsFeedBadType(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx := mocks.NewTestServiceContext(ctrl)

	_, err := NewCreateFeedLogic(middleware.WithUserID(context.Background(), 5), svcCtx).CreateFeed(&types.CreateFeedReq{FeedType: 9})
	ce, ok := errorx.TryParse(err)
	if !ok || ce.Code != errorx.FeedBadType {
		t.Fatalf("expected FeedBadType, got %v", err)
	}
}

// TestCreateFeed_EmptyMedia_ReturnsFeedEmptyMedia 验证图文类型缺少媒体被拒绝。
func TestCreateFeed_EmptyMedia_ReturnsFeedEmptyMedia(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx := mocks.NewTestServiceContext(ctrl)

	_, err := NewCreateFeedLogic(middleware.WithUserID(context.Background(), 5), svcCtx).CreateFeed(&types.CreateFeedReq{FeedType: 1})
	ce, ok := errorx.TryParse(err)
	if !ok || ce.Code != errorx.FeedEmptyMedia {
		t.Fatalf("expected FeedEmptyMedia, got %v", err)
	}
}
