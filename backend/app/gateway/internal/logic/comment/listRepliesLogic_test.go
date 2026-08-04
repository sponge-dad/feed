// listRepliesLogic_test.go
//
// 职责：ListRepliesLogic 单元测试，验证鉴权/参数校验与评论回复映射。
package comment

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	"google.golang.org/grpc"

	commentClient "github.com/sponge-dad/feed/app/comment/rpc/commentClient"
	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/mocks"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	"github.com/sponge-dad/feed/common/errorx"
)

// TestListReplies_Success_MapsReplies 验证回复列表映射与分页字段透传。
func TestListReplies_Success_MapsReplies(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx := mocks.NewTestServiceContext(ctrl)
	commentRpc := svcCtx.CommentRpc.(*mocks.MockComment)

	commentRpc.EXPECT().ListReplies(gomock.Any(), gomock.Any()).Return(&commentClient.ListRepliesResp{
		Replies: []*commentClient.CommentInfo{{
			CommentId: 1, Content: "c", UserId: 7, UserNickname: "n7", UserAvatar: "av7", LikeCount: 3,
		}},
		Page: &commentClient.PageInfo{HasMore: true, Cursor: "88", Page: 1, PageSize: 10},
	}, nil)

	resp, err := NewListRepliesLogic(middleware.WithUserID(context.Background(), 5), svcCtx).ListReplies(&types.ListRepliesReq{
		RootID: 100, Cursor: "", PageSize: 10,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.List) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(resp.List))
	}
	r := resp.List[0]
	if r.ID != 1 || r.Content != "c" || r.Author.ID != 7 || r.Author.Nickname != "n7" || r.LikeCount != 3 {
		t.Errorf("reply mapping mismatch: %+v", r)
	}
	if !resp.HasMore || resp.NextCursor != "88" {
		t.Errorf("pagination mismatch: hasMore=%v cursor=%s", resp.HasMore, resp.NextCursor)
	}
}

// TestListReplies_Unauthorized_ReturnsUnauthorized 验证未登录直接拒绝。
func TestListReplies_Unauthorized_ReturnsUnauthorized(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx := mocks.NewTestServiceContext(ctrl)

	_, err := NewListRepliesLogic(context.Background(), svcCtx).ListReplies(&types.ListRepliesReq{RootID: 100})
	ce, ok := errorx.TryParse(err)
	if !ok || ce.Code != errorx.Unauthorized {
		t.Fatalf("expected Unauthorized, got %v", err)
	}
}

// TestListReplies_InvalidRootID_ReturnsParamError 验证非法 RootID 被拒绝。
func TestListReplies_InvalidRootID_ReturnsParamError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx := mocks.NewTestServiceContext(ctrl)

	_, err := NewListRepliesLogic(middleware.WithUserID(context.Background(), 5), svcCtx).ListReplies(&types.ListRepliesReq{RootID: 0})
	ce, ok := errorx.TryParse(err)
	if !ok || ce.Code != errorx.ParamError {
		t.Fatalf("expected ParamError, got %v", err)
	}
}

// TestListReplies_ZeroPageSize_ClampedToDefault 验证 PageSize<=0 被钳制为默认 20 而非拒绝。
func TestListReplies_ZeroPageSize_ClampedToDefault(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx := mocks.NewTestServiceContext(ctrl)
	commentRpc := svcCtx.CommentRpc.(*mocks.MockComment)

	commentRpc.EXPECT().ListReplies(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, in *commentClient.ListRepliesReq, _ ...grpc.CallOption) (*commentClient.ListRepliesResp, error) {
			if in.PageSize != 20 {
				t.Errorf("expected default pageSize 20, got %d", in.PageSize)
			}
			return &commentClient.ListRepliesResp{Replies: []*commentClient.CommentInfo{}, Page: &commentClient.PageInfo{HasMore: false, Cursor: "", Page: 1, PageSize: 20}}, nil
		})

	resp, err := NewListRepliesLogic(middleware.WithUserID(context.Background(), 5), svcCtx).ListReplies(&types.ListRepliesReq{RootID: 100, PageSize: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil || resp.HasMore {
		t.Errorf("unexpected resp: %+v", resp)
	}
}
