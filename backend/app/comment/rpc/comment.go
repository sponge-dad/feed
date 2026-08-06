// comment.go
//
// 职责：Comment RPC 服务入口。加载配置、构造依赖上下文、注册 gRPC 服务与
// 统一错误拦截器（业务错误码 -> gRPC status error），Dev/Test 模式开启反射便于调试。
package main

import (
	"flag"
	"fmt"

	"github.com/sponge-dad/feed/app/comment/rpc/comment"
	"github.com/sponge-dad/feed/app/comment/rpc/internal/config"
	"github.com/sponge-dad/feed/app/comment/rpc/internal/server"
	"github.com/sponge-dad/feed/app/comment/rpc/internal/serverinterceptors"
	"github.com/sponge-dad/feed/app/comment/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/interceptors"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/comment.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)
	ctx, err := svc.NewServiceContext(c)
	if err != nil {
		panic(err)
	}
	defer ctx.Producer.Close()

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		comment.RegisterCommentServer(grpcServer, server.NewCommentServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	defer s.Stop()

	// 统一将 logic 层业务错误转换为可跨服务透传的 gRPC status error
	s.AddUnaryInterceptors(
		interceptors.UnaryServerRequestID,
		serverinterceptors.ErrorInterceptor,
	)

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)
	s.Start()
}
