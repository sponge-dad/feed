// Package main 是 Gateway HTTP 服务的入口。
//
// Gateway 负责把对外 REST 请求路由到内部 gRPC 服务（User / Relation），
// 并处理 JWT 鉴权、统一响应格式等横切关注点。
package main

import (
	"flag"
	"fmt"
	"net/http"

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
	ctx := svc.NewServiceContext(c)

	server := rest.MustNewServer(c.RestConf)
	defer server.Stop()

	// JWT 鉴权中间件：除注册、登录外，其余接口都需要登录态。
	jwtMiddleware := middleware.NewJwtAuthMiddleware(ctx)

	// 免登录接口：注册、登录。
	server.AddRoutes(
		[]rest.Route{
			{Method: http.MethodPost, Path: "/users/register", Handler: handler.RegisterHandler(ctx)},
			{Method: http.MethodPost, Path: "/users/login", Handler: handler.LoginHandler(ctx)},
		},
		rest.WithPrefix("/api/v1"),
	)

	// 需登录接口：获取用户、当前用户、更新用户、上传凭证。
	protectedRoutes := rest.WithMiddleware(
		jwtMiddleware,
		rest.Route{Method: http.MethodGet, Path: "/users/:userId", Handler: handler.GetUserHandler(ctx)},
		rest.Route{Method: http.MethodGet, Path: "/users/me", Handler: handler.GetMeHandler(ctx)},
		rest.Route{Method: http.MethodPatch, Path: "/users/me", Handler: handler.UpdateMeHandler(ctx)},
		rest.Route{Method: http.MethodPost, Path: "/upload/token", Handler: handler.UploadTokenHandler(ctx)},
	)
	server.AddRoutes(protectedRoutes, rest.WithPrefix("/api/v1"))

	fmt.Printf("Starting gateway server at %s:%d...\n", c.Host, c.Port)
	server.Start()
}
