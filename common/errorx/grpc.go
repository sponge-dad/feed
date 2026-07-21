package errorx

import (
	"encoding/json"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const grpcErrorPrefix = "[bizerror]"

// ToGRPCError 将业务 CodeError 转换为 gRPC status error。
// 转换后的错误可在 Gateway 侧通过 FromGRPCError 还原为原始 CodeError，
// 从而保持业务错误码跨服务透传。
func ToGRPCError(err *CodeError) error {
	payload, _ := json.Marshal(map[string]any{
		"code": err.Code,
		"msg":  err.Message,
	})
	return status.Error(codes.Unknown, grpcErrorPrefix+string(payload))
}

// FromGRPCError 尝试从 gRPC error 中还原业务 CodeError。
// 若不是由 ToGRPCError 生成的业务错误，则返回 nil, false。
func FromGRPCError(err error) (*CodeError, bool) {
	st, ok := status.FromError(err)
	if !ok {
		return nil, false
	}

	msg := st.Message()
	if !strings.HasPrefix(msg, grpcErrorPrefix) {
		return nil, false
	}

	var payload struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if jsonErr := json.Unmarshal([]byte(msg[len(grpcErrorPrefix):]), &payload); jsonErr != nil {
		return nil, false
	}

	return NewWithMsg(payload.Code, payload.Msg), true
}

// TryParse 从任意 error 中尝试解析出 CodeError。
// 优先判断是否是本包的 CodeError，其次尝试从 gRPC error 还原。
func TryParse(err error) (*CodeError, bool) {
	if err == nil {
		return nil, false
	}
	if codeErr, ok := err.(*CodeError); ok {
		return codeErr, true
	}
	return FromGRPCError(err)
}
