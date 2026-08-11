// content.go
//
// 职责：Content RPC 服务入口（FeedMind Agent 阶段二/三）。
// 启动 gRPC 服务（端口 9007）：画像查询/批量/检索/重试/反馈。
// Prometheus metrics：9110（见 etc/content.yaml）。
package main

import (
	"flag"
	"fmt"

	"github.com/sponge-dad/feed/app/content/rpc/content"
	"github.com/sponge-dad/feed/app/content/rpc/internal/config"
	"github.com/sponge-dad/feed/app/content/rpc/internal/server"
	"github.com/sponge-dad/feed/app/content/rpc/internal/serverinterceptors"
	"github.com/sponge-dad/feed/app/content/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/interceptors"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/content.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)
	ctx, err := svc.NewServiceContext(c)
	if err != nil {
		panic(err)
	}

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		content.RegisterContentServer(grpcServer, server.NewContentServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	// 服务端拦截器：request_id 透传 + 业务错误码转 gRPC status。
	s.AddUnaryInterceptors(
		interceptors.UnaryServerRequestID,
		serverinterceptors.ErrorInterceptor,
	)
	defer s.Stop()

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)
	s.Start()
}
