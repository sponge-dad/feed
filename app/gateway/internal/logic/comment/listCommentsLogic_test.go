// listcommentslogic_test.go
//
// 职责：comment 模块 logic 单元测试：
// 第一页热评并行拉取（含降级）、页码 cursor 分页、发评论参数校验、删除评论透传。
package comment

import (
	"context"
	"testing"

	commentClient "github.com/sponge-dad/feed/app/comment/rpc/commentClient"
	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/mocks"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/golang/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func newCommentEnv(ctrl *gomock.Controller, uid int64) (context.Context, *svc.ServiceContext, *mocks.MockComment, *mocks.MockUser) {
	commentRpc := mocks.NewMockComment(ctrl)
	userRpc := mocks.NewMockUser(ctrl)
	svcCtx := &svc.ServiceContext{CommentRpc: commentRpc, UserRpc: userRpc}
	return middleware.WithUserID(context.Background(), uid), svcCtx, commentRpc, userRpc
}

func TestListComments_FirstPageWithHot(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx, svcCtx, commentRpc, _ := newCommentEnv(ctrl, 1)

	commentRpc.EXPECT().ListComments(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, req *commentClient.ListCommentsReq, _ ...interface{}) (*commentClient.ListCommentsResp, error) {
			if req.Page != 1 || req.PageSize != 20 || req.PreviewCount != 3 {
				t.Errorf("unexpected req: %+v", req)
			}
			return &commentClient.ListCommentsResp{
				Comments: []*commentClient.CommentWithReplies{
					{
						Comment:        &commentClient.CommentInfo{CommentId: 201, Content: "c1", UserId: 11, UserNickname: "u11"},
						ReplyTotal:     5,
						PreviewReplies: []*commentClient.CommentInfo{{CommentId: 202, Content: "r1", UserId: 12}},
					},
				},
				Page: &commentClient.PageInfo{HasMore: true},
			}, nil
		})
	commentRpc.EXPECT().GetHotComments(gomock.Any(), gomock.Any()).Return(&commentClient.GetHotCommentsResp{
		Comments: []*commentClient.CommentInfo{{CommentId: 299, Content: "hot", UserId: 13, ReplyCount: 2}},
	}, nil)

	resp, err := NewListCommentsLogic(ctx, svcCtx).ListComments(&types.ListCommentsReq{FeedID: 101, PageSize: 20})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.List) != 1 || resp.List[0].ReplyCount != 5 || len(resp.List[0].SubReplies) != 1 {
		t.Errorf("unexpected list: %+v", resp.List)
	}
	if len(resp.HotComments) != 1 || resp.HotComments[0].ID != 299 {
		t.Errorf("unexpected hot comments: %+v", resp.HotComments)
	}
	if resp.NextCursor != "2" || !resp.HasMore {
		t.Errorf("unexpected paging: %+v", resp)
	}
}

func TestListComments_HotDegrade(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx, svcCtx, commentRpc, _ := newCommentEnv(ctrl, 1)

	commentRpc.EXPECT().ListComments(gomock.Any(), gomock.Any()).Return(&commentClient.ListCommentsResp{
		Comments: []*commentClient.CommentWithReplies{},
		Page:     &commentClient.PageInfo{HasMore: false},
	}, nil)
	commentRpc.EXPECT().GetHotComments(gomock.Any(), gomock.Any()).Return(nil, status.Error(codes.Unavailable, "down"))

	resp, err := NewListCommentsLogic(ctx, svcCtx).ListComments(&types.ListCommentsReq{FeedID: 101})
	if err != nil {
		t.Fatalf("hot comments failure should degrade, got %v", err)
	}
	if len(resp.HotComments) != 0 || resp.NextCursor != "" {
		t.Errorf("unexpected resp: %+v", resp)
	}
}

func TestListComments_SecondPageNoHot(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx, svcCtx, commentRpc, _ := newCommentEnv(ctrl, 1)

	commentRpc.EXPECT().ListComments(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, req *commentClient.ListCommentsReq, _ ...interface{}) (*commentClient.ListCommentsResp, error) {
			if req.Page != 3 {
				t.Errorf("page should be 3, got %d", req.Page)
			}
			return &commentClient.ListCommentsResp{Page: &commentClient.PageInfo{HasMore: false}}, nil
		})
	// GetHotComments 不应被调用（非第一页）

	if _, err := NewListCommentsLogic(ctx, svcCtx).ListComments(&types.ListCommentsReq{FeedID: 101, Cursor: "3"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestListComments_BadCursor(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx, svcCtx, _, _ := newCommentEnv(ctrl, 1)

	_, err := NewListCommentsLogic(ctx, svcCtx).ListComments(&types.ListCommentsReq{FeedID: 101, Cursor: "x"})
	ce, ok := err.(*errorx.CodeError)
	if !ok || ce.Code != errorx.ParamError {
		t.Fatalf("want ParamError, got %v", err)
	}
}

func TestCreateComment_EmptyContent(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx, svcCtx, _, _ := newCommentEnv(ctrl, 1)

	_, err := NewCreateCommentLogic(ctx, svcCtx).CreateComment(&types.CreateCommentReq{FeedID: 101, Content: "   "})
	ce, ok := err.(*errorx.CodeError)
	if !ok || ce.Code != errorx.CommentEmpty {
		t.Fatalf("want code %d, got %v", errorx.CommentEmpty, err)
	}
}

func TestCreateComment_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx, svcCtx, commentRpc, _ := newCommentEnv(ctrl, 1)

	commentRpc.EXPECT().CreateComment(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, req *commentClient.CreateCommentReq, _ ...interface{}) (*commentClient.CreateCommentResp, error) {
			if req.UserId != 1 || req.FeedId != 101 || req.ParentId != 200 {
				t.Errorf("unexpected req: %+v", req)
			}
			return &commentClient.CreateCommentResp{
				Comment: &commentClient.CommentInfo{
					CommentId: 201, FeedId: 101, Content: "hi", RootId: 200, ParentId: 200,
					UserId: 1, UserNickname: "me", CreatedAt: 1000,
				},
			}, nil
		})

	resp, err := NewCreateCommentLogic(ctx, svcCtx).CreateComment(&types.CreateCommentReq{
		FeedID: 101, Content: "hi", RootID: 200,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Comment.ID != 201 || resp.Comment.Author.Nickname != "me" {
		t.Errorf("unexpected resp: %+v", resp.Comment)
	}
}

func TestDeleteComment_NoPermissionPassthrough(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx, svcCtx, commentRpc, _ := newCommentEnv(ctrl, 2)

	commentRpc.EXPECT().DeleteComment(gomock.Any(), gomock.Any()).
		Return(nil, errorx.ToGRPCError(errorx.New(errorx.CommentNoPermission)))

	_, err := NewDeleteCommentLogic(ctx, svcCtx).DeleteComment(&types.DeleteCommentReq{CommentID: 201})
	if ce, ok := errorx.TryParse(err); !ok || ce.Code != errorx.CommentNoPermission {
		t.Fatalf("want code %d passthrough, got %v", errorx.CommentNoPermission, err)
	}
}

func TestLikeComment_NotReady(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx, svcCtx, _, _ := newCommentEnv(ctrl, 1)

	_, err := NewLikeCommentLogic(ctx, svcCtx).LikeComment(&types.LikeCommentReq{CommentID: 201})
	if err == nil {
		t.Fatal("like comment should return business error while downstream not ready")
	}
}
