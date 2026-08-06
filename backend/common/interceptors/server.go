package interceptors

import (
	"context"
	"regexp"
	"sync/atomic"

	"github.com/sponge-dad/feed/common/requestid"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// 缺失/非法 request_id 计数，便于发现漏改的调用点（§2）。
var (
	missingRequestIDCnt int64
	invalidRequestIDCnt int64
)

// requestIDRe 是 request-id 的白名单格式（见 docs/design/agent/02-request-trace.md）：
// 仅允许字母、数字、下划线与连字符，长度 1~64，用于防止日志注入与伪造。
var requestIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func UnaryServerRequestID(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get(MDKeyRequestID); len(vals) > 0 {
			id := vals[0]
			// 外部输入（任意调用方都可伪造 metadata），必须白名单校验；
			// 不合法直接丢弃并告警，避免进入日志与响应体造成注入/字段膨胀。
			if requestIDRe.MatchString(id) {
				ctx = requestid.WithRequestID(ctx, id)
				ctx = logx.WithFields(ctx, logx.Field("request_id", id))
				return handler(ctx, req)
			}
			atomic.AddInt64(&invalidRequestIDCnt, 1)
			logx.WithContext(ctx).WithFields(logx.Field("request_id", "invalid")).
				Errorf("[request_id] format invalid, dropped: %q (total=%d)", id, atomic.LoadInt64(&invalidRequestIDCnt))
		} else {
			// Metadata 缺失 request_id：记录 missing 并告警计数（§2）。
			atomic.AddInt64(&missingRequestIDCnt, 1)
			logx.WithContext(ctx).WithFields(logx.Field("request_id", "missing")).
				Errorf("[request_id] missing in incoming metadata (total=%d)", atomic.LoadInt64(&missingRequestIDCnt))
		}
	} else {
		atomic.AddInt64(&missingRequestIDCnt, 1)
		logx.WithContext(ctx).WithFields(logx.Field("request_id", "missing")).
			Errorf("[request_id] missing incoming metadata (total=%d)", atomic.LoadInt64(&missingRequestIDCnt))
	}
	return handler(ctx, req)
}
