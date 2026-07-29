// Package errorx gRPC 业务错误转换单元测试。
//
// 基线编号说明：docs/api-test-baseline.md 未为 common/errorx 单独编号，
// 本文件按模块前缀补充 E-GX-01 ~ E-GX-09，并包含 Feed 服务未注册
// ErrorInterceptor 的现状基线测试（Risk baseline: R-P0 相关，详见报告）。
package errorx

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Baseline: E-GX-01（CodeError → gRPC error，前缀与 JSON 内容正确）
func TestToGRPCError_CodeError_ProducesBizErrorPrefixAndJSON(t *testing.T) {
	err := ToGRPCError(New(UserExists))

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unknown, st.Code())
	require.True(t, strings.HasPrefix(st.Message(), "[bizerror]"), "消息应以 [bizerror] 开头: %s", st.Message())
	assert.JSONEq(t, `{"code":10001,"msg":"用户名已存在"}`, strings.TrimPrefix(st.Message(), "[bizerror]"))
}

// Baseline: E-GX-02（gRPC error → CodeError 还原码与消息）
func TestFromGRPCError_BizError_RecoversCodeAndMessage(t *testing.T) {
	grpcErr := ToGRPCError(NewWithMsg(FeedNoPermission, "无权限操作该帖子"))

	codeErr, ok := FromGRPCError(grpcErr)
	require.True(t, ok)
	assert.Equal(t, FeedNoPermission, codeErr.Code)
	assert.Equal(t, "无权限操作该帖子", codeErr.Message)
}

// Baseline: E-GX-03（普通 gRPC Internal 错误不能被误解析为业务错误）
func TestFromGRPCError_PlainInternalError_NotParsedAsBizError(t *testing.T) {
	codeErr, ok := FromGRPCError(status.Error(codes.Internal, "db connection refused"))
	assert.False(t, ok)
	assert.Nil(t, codeErr)
}

// Baseline: E-GX-04（格式错误的 [bizerror] 内容返回 false）
func TestFromGRPCError_MalformedBizErrorPayload_ReturnsFalse(t *testing.T) {
	cases := []struct {
		name string
		msg  string
	}{
		{"非JSON内容", "[bizerror]not-a-json"},
		{"空payload", "[bizerror]"},
		{"截断的JSON", `[bizerror]{"code":10001,`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			codeErr, ok := FromGRPCError(status.Error(codes.Unknown, tc.msg))
			assert.False(t, ok)
			assert.Nil(t, codeErr)
		})
	}
}

// Baseline: E-GX-05（nil error 不能被解析为业务错误）
func TestFromGRPCError_NilError_ReturnsFalse(t *testing.T) {
	codeErr, ok := TryParse(nil)
	assert.False(t, ok)
	assert.Nil(t, codeErr)
}

// Baseline: E-GX-06（未注册的业务码使用默认“未知错误”消息且可往返）
func TestToGRPCError_UnknownCode_RoundTripsWithDefaultMessage(t *testing.T) {
	src := New(99999)
	assert.Equal(t, "未知错误", src.Message)

	got, ok := FromGRPCError(ToGRPCError(src))
	require.True(t, ok)
	assert.Equal(t, 99999, got.Code)
	assert.Equal(t, "未知错误", got.Message)
}

// Baseline: E-GX-07（特殊字符消息往返一致）
func TestToGRPCError_SpecialCharacters_RoundTripConsistent(t *testing.T) {
	cases := []struct {
		name string
		msg  string
	}{
		{"双引号与反斜杠", `he said "hi" \ path`},
		{"中文与emoji", "用户不存在🚫"},
		{"换行与制表符", "line1\nline2\tend"},
		{"JSON破坏字符", `{"injected":true}`},
		{"bizerror前缀本身", "[bizerror]嵌套前缀"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := FromGRPCError(ToGRPCError(NewWithMsg(UserNotFound, tc.msg)))
			require.True(t, ok)
			assert.Equal(t, UserNotFound, got.Code)
			assert.Equal(t, tc.msg, got.Message)
		})
	}
}

// Baseline: E-GX-08（ToGRPCError/FromGRPCError 全量往返一致性）
func TestGRPCError_RoundTrip_AllRegisteredCodes(t *testing.T) {
	codesToCheck := []int{
		ServerError, ParamError, Unauthorized, Forbidden, TooManyReq,
		UserExists, UserPasswordWrong, UserNotFound, UserDisabled,
		RelationSelf, RelationAlreadyFollow,
		FeedNotFound, FeedNoPermission, FeedEmptyContent, FeedEmptyMedia,
		CommentNotFound, InteractionFeedNotFound,
	}
	for _, code := range codesToCheck {
		src := New(code)
		got, ok := FromGRPCError(ToGRPCError(src))
		require.True(t, ok, "code=%d 应可往返", code)
		assert.Equal(t, src.Code, got.Code)
		assert.Equal(t, src.Message, got.Message)
	}
}

// Baseline: E-GX-09（TryParse 直接识别本包 CodeError）
func TestTryParse_DirectCodeError_Recognized(t *testing.T) {
	got, ok := TryParse(New(FeedNotFound))
	require.True(t, ok)
	assert.Equal(t, FeedNotFound, got.Code)

	_, ok = TryParse(errors.New("plain error"))
	assert.False(t, ok)
}

// Risk baseline: Feed 服务当前未注册 serverinterceptors.ErrorInterceptor
// （app/feed/rpc/feed.go 无 AddUnaryInterceptors 调用，而 user/relation/comment/interaction 均已注册）。
// 因此 Feed logic 返回的 *CodeError 会被 gRPC 框架按普通 error 包装为
// codes.Unknown + CodeError.Error() 文本（无 [bizerror] 前缀），
// Gateway 侧 TryParse 无法还原业务码，最终会退化为 ServerError(1)。
// 本测试仅固化当前行为，不修改 Feed 启动代码。
func TestFeedServiceError_WithoutErrorInterceptor_CurrentlyDegradesToServerError(t *testing.T) {
	bizErr := New(FeedNoPermission)

	// gRPC server 对非 status error 的默认包装行为：codes.Unknown + err.Error()
	wireErr := status.Error(codes.Unknown, bizErr.Error())

	// Gateway 侧尝试还原：应失败（无 [bizerror] 前缀）
	codeErr, ok := TryParse(wireErr)
	assert.False(t, ok, "Feed 未注册 ErrorInterceptor 时业务码不应能被还原（当前行为基线）")
	assert.Nil(t, codeErr)

	// 对照组：若经过 ErrorInterceptor（ToGRPCError），业务码可完整还原
	intercepted := ToGRPCError(bizErr)
	recovered, ok := TryParse(intercepted)
	require.True(t, ok)
	assert.Equal(t, FeedNoPermission, recovered.Code)
}
