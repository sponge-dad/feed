// loginLogic_test.go
//
// 职责：LoginLogic 单元测试，验证请求透传与响应映射。
package logic

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	"google.golang.org/grpc"

	"github.com/sponge-dad/feed/app/gateway/internal/mocks"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	userClient "github.com/sponge-dad/feed/app/user/rpc/userClient"
	"github.com/sponge-dad/feed/common/errorx"
)

// TestLogin_Success_ReturnsMappedUserAndToken 验证登录成功时响应映射正确。
func TestLogin_Success_ReturnsMappedUserAndToken(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx := mocks.NewTestServiceContext(ctrl)
	userRpc := svcCtx.UserRpc.(*mocks.MockUser)

	userRpc.EXPECT().Login(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, in *userClient.LoginReq, _ ...grpc.CallOption) (*userClient.LoginResp, error) {
			if in.Username != "alice" || in.Password != "pwd" {
				t.Errorf("login req mapping mismatch: %+v", in)
			}
			return &userClient.LoginResp{
				User:  &userClient.UserInfo{Id: 1, Username: "alice", Nickname: "Alice"},
				Token: "login-tok",
			}, nil
		})

	resp, err := NewLoginLogic(context.Background(), svcCtx).Login(&types.LoginReq{
		Username: "alice", Password: "pwd",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.User.ID != 1 || resp.Token != "login-tok" {
		t.Errorf("login mapping mismatch: %+v", resp)
	}
}

// TestLogin_DownstreamError_Propagates 验证下游错误原样透传。
func TestLogin_DownstreamError_Propagates(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx := mocks.NewTestServiceContext(ctrl)
	userRpc := svcCtx.UserRpc.(*mocks.MockUser)

	wantErr := errorx.New(errorx.UserNotFound)
	userRpc.EXPECT().Login(gomock.Any(), gomock.Any()).Return(nil, wantErr)

	if _, err := NewLoginLogic(context.Background(), svcCtx).Login(&types.LoginReq{
		Username: "alice", Password: "pwd",
	}); err == nil {
		t.Fatal("expected downstream error to propagate")
	}
}
