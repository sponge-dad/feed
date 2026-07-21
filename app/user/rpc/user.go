package main

import (
	"flag"
	"fmt"

	"github.com/sponge-dad/feed/app/user/rpc/internal/config"
	"github.com/sponge-dad/feed/app/user/rpc/internal/server"
	"github.com/sponge-dad/feed/app/user/rpc/internal/serverinterceptors"
	"github.com/sponge-dad/feed/app/user/rpc/internal/svc"
	"github.com/sponge-dad/feed/app/user/rpc/user"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/user.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)
	ctx := svc.NewServiceContext(c)

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		user.RegisterUserServer(grpcServer, server.NewUserServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})

	// 注册服务端拦截器：将业务错误码转换为 gRPC status error，供调用方还原。
	s.AddUnaryInterceptors(serverinterceptors.ErrorInterceptor)
	defer s.Stop()

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)
	s.Start()
}
