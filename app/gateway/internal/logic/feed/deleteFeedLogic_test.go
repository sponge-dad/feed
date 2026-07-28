// deletefeedlogic_test.go
//
// 职责：DeleteFeed logic 单元测试：作者本人删除成功、非作者越权 12002、
// 帖子不存在错误透传、参数错误。
package feed

import (
	"context"
	"testing"

	feedClient "github.com/sponge-dad/feed/app/feed/rpc/feedclient"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/golang/mock/gomock"
)

func TestDeleteFeed_OwnerSuccess(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx, svcCtx, _, feedRpc, _ := newTestEnv(ctrl, 1)

	feedRpc.EXPECT().GetFeed(gomock.Any(), gomock.Any()).Return(&feedClient.GetFeedResp{
		Feed: &feedClient.FeedInfo{FeedId: 101, AuthorId: 1},
	}, nil)
	feedRpc.EXPECT().DeleteFeed(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, req *feedClient.DeleteFeedReq, _ ...interface{}) (*feedClient.DeleteFeedResp, error) {
			if req.FeedId != 101 || req.UserId != 1 {
				t.Errorf("unexpected req: %+v", req)
			}
			return &feedClient.DeleteFeedResp{Success: true}, nil
		})

	resp, err := NewDeleteFeedLogic(ctx, svcCtx).DeleteFeed(&types.DeleteFeedReq{FeedID: 101})
	if err != nil || !resp.Success {
		t.Fatalf("owner delete should succeed, got %v %v", resp, err)
	}
}

func TestDeleteFeed_NotOwnerForbidden(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx, svcCtx, _, feedRpc, _ := newTestEnv(ctrl, 2) // 登录用户 2，作者 1

	feedRpc.EXPECT().GetFeed(gomock.Any(), gomock.Any()).Return(&feedClient.GetFeedResp{
		Feed: &feedClient.FeedInfo{FeedId: 101, AuthorId: 1},
	}, nil)
	// DeleteFeed 不应被调用

	_, err := NewDeleteFeedLogic(ctx, svcCtx).DeleteFeed(&types.DeleteFeedReq{FeedID: 101})
	ce, ok := err.(*errorx.CodeError)
	if !ok || ce.Code != errorx.FeedNoPermission {
		t.Fatalf("want code %d, got %v", errorx.FeedNoPermission, err)
	}
}

func TestDeleteFeed_NotFoundPassthrough(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx, svcCtx, _, feedRpc, _ := newTestEnv(ctrl, 1)

	feedRpc.EXPECT().GetFeed(gomock.Any(), gomock.Any()).
		Return(nil, errorx.ToGRPCError(errorx.New(errorx.FeedNotFound)))

	_, err := NewDeleteFeedLogic(ctx, svcCtx).DeleteFeed(&types.DeleteFeedReq{FeedID: 101})
	if err == nil {
		t.Fatal("not found should propagate")
	}
	if ce, ok := errorx.TryParse(err); !ok || ce.Code != errorx.FeedNotFound {
		t.Fatalf("want business code %d passthrough, got %v", errorx.FeedNotFound, err)
	}
}

func TestDeleteFeed_BadParam(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx, svcCtx, _, _, _ := newTestEnv(ctrl, 1)

	_, err := NewDeleteFeedLogic(ctx, svcCtx).DeleteFeed(&types.DeleteFeedReq{FeedID: 0})
	ce, ok := err.(*errorx.CodeError)
	if !ok || ce.Code != errorx.ParamError {
		t.Fatalf("want ParamError, got %v", err)
	}
}
