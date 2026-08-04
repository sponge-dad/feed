package main

import (
	"flag"
	"fmt"

	"github.com/sponge-dad/feed/app/relation/rpc/internal/config"
	"github.com/sponge-dad/feed/app/relation/rpc/internal/server"
	"github.com/sponge-dad/feed/app/relation/rpc/internal/serverinterceptors"
	"github.com/sponge-dad/feed/app/relation/rpc/internal/svc"
	"github.com/sponge-dad/feed/app/relation/rpc/relation"
	"github.com/sponge-dad/feed/common/idgen"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/relation.yaml", "the config file")

func main() {
	flag.Parse()

	// 初始化 Snowflake 节点。单机开发环境固定用机器ID 1。
	// 生产环境（K8s 多 Pod）需要通过环境变量/ConfigMap 注入各 Pod 唯一 ID，
	// 否则会出现不同实例生成相同 ID 的冲突。
	if err := idgen.Init(1); err != nil {
		panic(err)
	}

	var c config.Config
	conf.MustLoad(*configFile, &c)
	ctx := svc.NewServiceContext(c)

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		relation.RegisterRelationServer(grpcServer, server.NewRelationServer(ctx))

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
