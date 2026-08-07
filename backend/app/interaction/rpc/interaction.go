// interaction.go
//
// 职责：Interaction RPC 服务入口。
// 启动 gRPC 服务（端口 9005）与后台 MQ 消费者（interaction.event 异步落库）。
package main

import (
	"flag"
	"fmt"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/config"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/server"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/serverinterceptors"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/worker"
	"github.com/sponge-dad/feed/common/idgen"
	"github.com/sponge-dad/feed/common/interceptors"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/interaction.yaml", "the config file")

func main() {
	flag.Parse()

	// 初始化 Snowflake 节点。单机开发环境固定用机器ID 1。
	// 生产环境（K8s 多 Pod）需要通过环境变量/ConfigMap 注入各 Pod 唯一 ID。
	if err := idgen.Init(1); err != nil {
		panic(err)
	}

	var c config.Config
	conf.MustLoad(*configFile, &c)
	ctx, err := svc.NewServiceContext(c)
	if err != nil {
		panic(err)
	}

	// 启动后台消费者（订阅 interaction.event，异步落库 likes/collections），进程退出时关闭。
	wk := worker.NewWorker(ctx)
	if err := wk.Start(); err != nil {
		panic(err)
	}
	defer ctx.Consumer.Shutdown()

	// 启动行为埋点消费者（订阅 feed-behavior-event，指标累加 + 抽样落库），进程退出时关闭。
	bw := worker.NewBehaviorWorker(ctx)
	if err := bw.Start(); err != nil {
		panic(err)
	}
	defer bw.Shutdown()

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		interaction.RegisterInteractionServer(grpcServer, server.NewInteractionServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	// 注册服务端拦截器：将业务错误码转换为 gRPC status error，供调用方还原。
	s.AddUnaryInterceptors(
		interceptors.UnaryServerRequestID,
		serverinterceptors.ErrorInterceptor,
	)
	defer s.Stop()

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)
	s.Start()
}
