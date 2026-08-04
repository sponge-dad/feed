// getMeLogic_test.go
//
// 职责：GetMeLogic 单元测试，验证从 context 取当前用户并映射详情。
package logic

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/mocks"
	userClient "github.com/sponge-dad/feed/app/user/rpc/userClient"
)

// TestGetMe_WithUserID_ReturnsMappedUser 验证带登录态时返回当前用户详情。
func TestGetMe_WithUserID_ReturnsMappedUser(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx := mocks.NewTestServiceContext(ctrl)
	userRpc := svcCtx.UserRpc.(*mocks.MockUser)

	userRpc.EXPECT().GetUser(gomock.Any(), gomock.Any()).Return(
		&userClient.GetUserResp{User: &userClient.UserInfo{Id: 5, Nickname: "me5"}}, nil)

	resp, err := NewGetMeLogic(middleware.WithUserID(context.Background(), 5), svcCtx).GetMe()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil || resp.ID != 5 || resp.Nickname != "me5" {
		t.Errorf("getMe mapping mismatch: %+v", resp)
	}
}

// TestGetMe_NoUserID_ReturnsNil 验证无登录态时 GetMe 直接返回 (nil, nil)，且不调用下游。
func TestGetMe_NoUserID_ReturnsNil(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx := mocks.NewTestServiceContext(ctrl)

	resp, err := NewGetMeLogic(context.Background(), svcCtx).GetMe()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != nil {
		t.Errorf("expected nil, got %+v", resp)
	}
}
