package interceptors

import (
	"context"
	"regexp"

	"github.com/sponge-dad/feed/common/requestid"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// requestIDRe 是 request-id 的白名单格式（见 docs/design/agent/02-request-trace.md）：
// 仅允许字母、数字、下划线与连字符，长度 1~64，用于防止日志注入与伪造。
var requestIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func UnaryServerRequestID(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get(MDKeyRequestID); len(vals) > 0 {
			id := vals[0]
			// 外部输入（任意调用方都可伪造 metadata），必须白名单校验；
			// 不合法直接丢弃，避免进入日志与响应体造成注入/字段膨胀。
			if requestIDRe.MatchString(id) {
				ctx = requestid.WithRequestID(ctx, id)
				ctx = logx.WithFields(ctx, logx.Field("request_id", id))
			}
		}
	}
	return handler(ctx, req)
}
