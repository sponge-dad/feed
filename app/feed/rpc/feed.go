package main

import (
	"flag"
	"fmt"

	"github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/config"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/server"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/serverinterceptors"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/svc"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/worker"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/feed.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)
	ctx, err := svc.NewServiceContext(c)
	if err != nil {
		panic(err)
	}

	wk := worker.NewWorker(ctx)
	// 启动后台消费者（订阅 feed.created / feed.deleted），进程退出时关闭。
	if err := wk.Start(); err != nil {
		panic(err)
	}
	defer ctx.Consumer.Shutdown()

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		feed.RegisterFeedServer(grpcServer, server.NewFeedServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	s.AddUnaryInterceptors(serverinterceptors.ErrorInterceptor)
	defer s.Stop()

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)
	s.Start()
}
