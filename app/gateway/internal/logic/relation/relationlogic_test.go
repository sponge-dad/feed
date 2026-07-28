// relationlogic_test.go
//
// 职责：relation 模块 logic 单元测试：
// 关注（成功/自关/参数错误）、关注列表聚合（IsFollow 降级）、粉丝列表分页边界。
package relation

import (
	"context"
	"testing"

	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/mocks"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	relationClient "github.com/sponge-dad/feed/app/relation/rpc/relationclient"
	userClient "github.com/sponge-dad/feed/app/user/rpc/userClient"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/golang/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func newRelationEnv(ctrl *gomock.Controller, uid int64) (context.Context, *svc.ServiceContext, *mocks.MockUser, *mocks.MockRelation) {
	userRpc := mocks.NewMockUser(ctrl)
	relationRpc := mocks.NewMockRelation(ctrl)
	svcCtx := &svc.ServiceContext{UserRpc: userRpc, RelationRpc: relationRpc}
	return middleware.WithUserID(context.Background(), uid), svcCtx, userRpc, relationRpc
}

func TestFollow_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx, svcCtx, _, relationRpc := newRelationEnv(ctrl, 1)

	relationRpc.EXPECT().Follow(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, req *relationClient.FollowReq, _ ...interface{}) (*relationClient.FollowResp, error) {
			if req.FollowerId != 1 || req.FolloweeId != 2 {
				t.Errorf("unexpected req: %+v", req)
			}
			return &relationClient.FollowResp{Success: true}, nil
		})
	relationRpc.EXPECT().GetFans(gomock.Any(), gomock.Any()).Return(&relationClient.GetFansResp{Total: 8}, nil)

	resp, err := NewFollowLogic(ctx, svcCtx).Follow(&types.FollowReq{FolloweeID: 2})
	if err != nil || !resp.Success || resp.FollowerCount != 8 {
		t.Fatalf("unexpected resp: %+v err: %v", resp, err)
	}
}

func TestFollow_Self(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx, svcCtx, _, _ := newRelationEnv(ctrl, 1)

	_, err := NewFollowLogic(ctx, svcCtx).Follow(&types.FollowReq{FolloweeID: 1})
	ce, ok := err.(*errorx.CodeError)
	if !ok || ce.Code != errorx.RelationSelf {
		t.Fatalf("want code %d, got %v", errorx.RelationSelf, err)
	}
}

func TestFollow_FansCountDegrade(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx, svcCtx, _, relationRpc := newRelationEnv(ctrl, 1)

	relationRpc.EXPECT().Follow(gomock.Any(), gomock.Any()).Return(&relationClient.FollowResp{Success: true}, nil)
	relationRpc.EXPECT().GetFans(gomock.Any(), gomock.Any()).Return(nil, status.Error(codes.Unavailable, "down"))

	resp, err := NewFollowLogic(ctx, svcCtx).Follow(&types.FollowReq{FolloweeID: 2})
	if err != nil || !resp.Success || resp.FollowerCount != 0 {
		t.Fatalf("fans count should degrade to 0, got %+v err %v", resp, err)
	}
}

func TestFollowingList_Aggregation(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx, svcCtx, userRpc, relationRpc := newRelationEnv(ctrl, 1)

	relationRpc.EXPECT().GetFollows(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, req *relationClient.GetFollowsReq, _ ...interface{}) (*relationClient.GetFollowsResp, error) {
			if req.UserId != 1 || req.Page != 1 || req.PageSize != 20 {
				t.Errorf("unexpected req: %+v", req)
			}
			return &relationClient.GetFollowsResp{FolloweeIds: []int64{2, 3}, Total: 25}, nil
		})
	userRpc.EXPECT().BatchGetUsers(gomock.Any(), gomock.Any()).Return(&userClient.BatchGetUsersResp{
		Users: []*userClient.UserBrief{{Id: 2, Nickname: "u2"}, {Id: 3, Nickname: "u3"}},
	}, nil)
	relationRpc.EXPECT().IsFollow(gomock.Any(), gomock.Any()).Return(&relationClient.IsFollowResp{
		Results: map[int64]bool{2: true, 3: true},
	}, nil)

	resp, err := NewFollowingListLogic(ctx, svcCtx).FollowingList(&types.FollowingListReq{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.List) != 2 || resp.Total != 25 || !resp.HasMore {
		t.Errorf("unexpected resp: %+v", resp)
	}
	if !resp.List[0].IsFollowing || resp.List[0].Nickname != "u2" {
		t.Errorf("unexpected item: %+v", resp.List[0])
	}
}

func TestFollowerList_IsFollowDegrade(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx, svcCtx, userRpc, relationRpc := newRelationEnv(ctrl, 1)

	relationRpc.EXPECT().GetFans(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, req *relationClient.GetFansReq, _ ...interface{}) (*relationClient.GetFansResp, error) {
			// page_size 超上限应被钳制为 50
			if req.PageSize != 50 {
				t.Errorf("page_size should clamp to 50, got %d", req.PageSize)
			}
			return &relationClient.GetFansResp{FollowerIds: []int64{5}, Total: 1}, nil
		})
	userRpc.EXPECT().BatchGetUsers(gomock.Any(), gomock.Any()).Return(&userClient.BatchGetUsersResp{
		Users: []*userClient.UserBrief{{Id: 5, Nickname: "u5"}},
	}, nil)
	relationRpc.EXPECT().IsFollow(gomock.Any(), gomock.Any()).Return(nil, status.Error(codes.Unavailable, "down"))

	resp, err := NewFollowerListLogic(ctx, svcCtx).FollowerList(&types.FollowerListReq{PageSize: 500})
	if err != nil {
		t.Fatalf("IsFollow failure should degrade, got %v", err)
	}
	if len(resp.List) != 1 || resp.List[0].IsFollowing {
		t.Errorf("is_following should degrade to false: %+v", resp.List)
	}
	if resp.HasMore {
		t.Errorf("has_more should be false: %+v", resp)
	}
}

func TestFollowingList_UserRpcFail(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx, svcCtx, userRpc, relationRpc := newRelationEnv(ctrl, 1)

	relationRpc.EXPECT().GetFollows(gomock.Any(), gomock.Any()).
		Return(&relationClient.GetFollowsResp{FolloweeIds: []int64{2}, Total: 1}, nil)
	userRpc.EXPECT().BatchGetUsers(gomock.Any(), gomock.Any()).Return(nil, status.Error(codes.Unavailable, "down"))
	relationRpc.EXPECT().IsFollow(gomock.Any(), gomock.Any()).Return(&relationClient.IsFollowResp{}, nil).AnyTimes()

	if _, err := NewFollowingListLogic(ctx, svcCtx).FollowingList(&types.FollowingListReq{}); err == nil {
		t.Fatal("user rpc failure should fail the request")
	}
}

func TestIsFollowing(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx, svcCtx, _, relationRpc := newRelationEnv(ctrl, 1)

	relationRpc.EXPECT().IsFollow(gomock.Any(), gomock.Any()).Return(&relationClient.IsFollowResp{
		Results: map[int64]bool{2: true},
	}, nil)

	resp, err := NewIsFollowingLogic(ctx, svcCtx).IsFollowing(&types.IsFollowingReq{TargetID: 2})
	if err != nil || !resp.IsFollowing {
		t.Fatalf("unexpected resp: %+v err %v", resp, err)
	}
}
