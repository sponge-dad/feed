package interceptors

import (
	"context"

	"github.com/sponge-dad/feed/common/requestid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func UnaryClientRequestID(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	if rid := requestid.FromContext(ctx); rid != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, MDKeyRequestID, rid)
	}
	return invoker(ctx, method, req, reply, cc, opts...)
}
