// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package main

import (
	"flag"
	"fmt"

	"github.com/sponge-dad/feed/app/gateway/internal/config"
	"github.com/sponge-dad/feed/app/gateway/internal/handler"
	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/rest"
)

var configFile = flag.String("f", "etc/gateway.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)

	server := rest.MustNewServer(c.RestConf)
	defer server.Stop()

	// 全局中间件：提取客户端 IP 注入 context，供同城流 / 发帖 IP 属地使用。
	server.Use(middleware.RequestIDMiddleware)
	server.Use(middleware.ClientIPMiddleware)

	ctx := svc.NewServiceContext(c)
	handler.RegisterHandlers(server, ctx)

	fmt.Printf("Starting server at %s:%d...\n", c.Host, c.Port)
	server.Start()
}
