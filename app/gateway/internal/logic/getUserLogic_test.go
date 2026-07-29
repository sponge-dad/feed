// getUserLogic_test.go
//
// 职责：GetUserLogic 单元测试，验证并发聚合(用户/关注数/粉丝数/关注关系)与降级语义。
package logic

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/mocks"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	relationClient "github.com/sponge-dad/feed/app/relation/rpc/relationclient"
	userClient "github.com/sponge-dad/feed/app/user/rpc/userClient"
	"github.com/sponge-dad/feed/common/errorx"
)

// errDownstream 用于模拟下游服务降级（非业务错误码）。
var errDownstream = errors.New("downstream degraded")

// TestGetUser_Success_AggregatesCountsAndIsFollow 验证并行聚合下计数与关注关系映射正确。
func TestGetUser_Success_AggregatesCountsAndIsFollow(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx := mocks.NewTestServiceContext(ctrl)
	userRpc := svcCtx.UserRpc.(*mocks.MockUser)
	relationRpc := svcCtx.RelationRpc.(*mocks.MockRelation)

	userRpc.EXPECT().GetUser(gomock.Any(), gomock.Any()).Return(
		&userClient.GetUserResp{User: &userClient.UserInfo{Id: 7, Nickname: "u7"}}, nil)
	relationRpc.EXPECT().GetFollows(gomock.Any(), gomock.Any()).Return(&relationClient.GetFollowsResp{Total: 3}, nil)
	relationRpc.EXPECT().GetFans(gomock.Any(), gomock.Any()).Return(&relationClient.GetFansResp{Total: 5}, nil)
	relationRpc.EXPECT().IsFollow(gomock.Any(), gomock.Any()).Return(&relationClient.IsFollowResp{Results: map[int64]bool{7: true}}, nil)

	resp, err := NewGetUserLogic(middleware.WithUserID(context.Background(), 99), svcCtx).GetUser(&types.GetUserReq{UserID: 7})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil detail")
	}
	if resp.ID != 7 || resp.Nickname != "u7" {
		t.Errorf("user mapping mismatch: %+v", resp)
	}
	if resp.FollowingCount != 3 || resp.FollowerCount != 5 {
		t.Errorf("counts mismatch: following=%d follower=%d", resp.FollowingCount, resp.FollowerCount)
	}
	if !resp.IsFollowing {
		t.Error("expected IsFollowing=true")
	}
}

// TestGetUser_NilUser_ReturnsNil 验证下游返回空用户时逻辑返回 (nil, nil)。
func TestGetUser_NilUser_ReturnsNil(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx := mocks.NewTestServiceContext(ctrl)
	userRpc := svcCtx.UserRpc.(*mocks.MockUser)

	userRpc.EXPECT().GetUser(gomock.Any(), gomock.Any()).Return(&userClient.GetUserResp{User: nil}, nil)

	resp, err := NewGetUserLogic(context.Background(), svcCtx).GetUser(&types.GetUserReq{UserID: 7})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != nil {
		t.Errorf("expected nil detail, got %+v", resp)
	}
}

// TestGetUser_RelationFail_StillReturns 验证关系服务降级时仍可返回用户，计数回落为 0。
func TestGetUser_RelationFail_StillReturns(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx := mocks.NewTestServiceContext(ctrl)
	userRpc := svcCtx.UserRpc.(*mocks.MockUser)
	relationRpc := svcCtx.RelationRpc.(*mocks.MockRelation)

	userRpc.EXPECT().GetUser(gomock.Any(), gomock.Any()).Return(
		&userClient.GetUserResp{User: &userClient.UserInfo{Id: 7, Nickname: "u7"}}, nil)
	relationRpc.EXPECT().GetFollows(gomock.Any(), gomock.Any()).Return(nil, errDownstream)
	relationRpc.EXPECT().GetFans(gomock.Any(), gomock.Any()).Return(nil, errDownstream)
	relationRpc.EXPECT().IsFollow(gomock.Any(), gomock.Any()).Return(&relationClient.IsFollowResp{Results: map[int64]bool{7: false}}, nil)

	resp, err := NewGetUserLogic(middleware.WithUserID(context.Background(), 99), svcCtx).GetUser(&types.GetUserReq{UserID: 7})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil || resp.ID != 7 {
		t.Fatalf("expected degraded detail, got %+v", resp)
	}
	if resp.FollowingCount != 0 || resp.FollowerCount != 0 {
		t.Errorf("counts should degrade to 0, got following=%d follower=%d", resp.FollowingCount, resp.FollowerCount)
	}
	if resp.IsFollowing {
		t.Error("expected IsFollowing=false on degraded relation")
	}
}

// TestGetUser_DownstreamError_Propagates 验证 GetUser 下游错误原样透传。
func TestGetUser_DownstreamError_Propagates(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx := mocks.NewTestServiceContext(ctrl)
	userRpc := svcCtx.UserRpc.(*mocks.MockUser)

	userRpc.EXPECT().GetUser(gomock.Any(), gomock.Any()).Return(nil, errorx.New(errorx.UserNotFound))

	if _, err := NewGetUserLogic(context.Background(), svcCtx).GetUser(&types.GetUserReq{UserID: 7}); err == nil {
		t.Fatal("expected downstream error to propagate")
	}
}
