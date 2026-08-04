// updateMeLogic_test.go
//
// 职责：UpdateMeLogic 单元测试，验证从 context 取当前用户并透传更新字段。
package logic

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	"google.golang.org/grpc"

	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/mocks"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	userClient "github.com/sponge-dad/feed/app/user/rpc/userClient"
)

// TestUpdateMe_WithUserID_ReturnsMappedUser 验证带登录态时更新并映射用户。
func TestUpdateMe_WithUserID_ReturnsMappedUser(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx := mocks.NewTestServiceContext(ctrl)
	userRpc := svcCtx.UserRpc.(*mocks.MockUser)

	userRpc.EXPECT().UpdateUser(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, in *userClient.UpdateUserReq, _ ...grpc.CallOption) (*userClient.UpdateUserResp, error) {
			if in.UserId != 5 || in.Nickname != "newname" || in.CityName != "深圳" {
				t.Errorf("updateUser req mapping mismatch: %+v", in)
			}
			return &userClient.UpdateUserResp{User: &userClient.UserInfo{Id: 5, Nickname: "newname", CityName: "深圳"}}, nil
		})

	resp, err := NewUpdateMeLogic(middleware.WithUserID(context.Background(), 5), svcCtx).UpdateMe(&types.UpdateUserReq{
		Nickname: "newname", CityName: "深圳",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.User.ID != 5 || resp.User.Nickname != "newname" || resp.User.CityName != "深圳" {
		t.Errorf("updateMe mapping mismatch: %+v", resp)
	}
}

// TestUpdateMe_NoUserID_ReturnsNil 验证无登录态时 UpdateMe 直接返回 (nil, nil)，且不调用下游。
func TestUpdateMe_NoUserID_ReturnsNil(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx := mocks.NewTestServiceContext(ctrl)

	resp, err := NewUpdateMeLogic(context.Background(), svcCtx).UpdateMe(&types.UpdateUserReq{Nickname: "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != nil {
		t.Errorf("expected nil, got %+v", resp)
	}
}
