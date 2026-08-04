// registerLogic_test.go
//
// 职责：RegisterLogic 单元测试，验证请求透传与响应映射。
// 使用 gomock 模拟 User RPC，不依赖真实服务。
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

// TestRegister_Success_ReturnsMappedUserAndToken 验证注册成功时下游 UserInfo 映射到 types.User 且 Token 透传。
func TestRegister_Success_ReturnsMappedUserAndToken(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx := mocks.NewTestServiceContext(ctrl)
	userRpc := svcCtx.UserRpc.(*mocks.MockUser)

	userRpc.EXPECT().Register(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, in *userClient.RegisterReq, _ ...grpc.CallOption) (*userClient.RegisterResp, error) {
			if in.Username != "alice" || in.Password != "pwd" || in.Nickname != "Alice" {
				t.Errorf("register req mapping mismatch: %+v", in)
			}
			return &userClient.RegisterResp{
				User:  &userClient.UserInfo{Id: 1, Username: "alice", Nickname: "Alice", Avatar: "av"},
				Token: "tok-123",
			}, nil
		})

	resp, err := NewRegisterLogic(context.Background(), svcCtx).Register(&types.RegisterReq{
		Username: "alice", Password: "pwd", Nickname: "Alice",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.User.ID != 1 || resp.User.Username != "alice" || resp.User.Nickname != "Alice" {
		t.Errorf("user mapping mismatch: %+v", resp.User)
	}
	if resp.Token != "tok-123" {
		t.Errorf("token mapping mismatch: %q", resp.Token)
	}
}

// TestRegister_DownstreamError_Propagates 验证下游错误原样透传。
func TestRegister_DownstreamError_Propagates(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svcCtx := mocks.NewTestServiceContext(ctrl)
	userRpc := svcCtx.UserRpc.(*mocks.MockUser)

	wantErr := errorx.New(errorx.UserExists)
	userRpc.EXPECT().Register(gomock.Any(), gomock.Any()).Return(nil, wantErr)

	_, err := NewRegisterLogic(context.Background(), svcCtx).Register(&types.RegisterReq{
		Username: "alice", Password: "pwd", Nickname: "Alice",
	})
	if err == nil {
		t.Fatal("expected downstream error to propagate")
	}
}
