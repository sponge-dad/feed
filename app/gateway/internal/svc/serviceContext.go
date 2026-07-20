// Package svc 是 Gateway 的依赖注入容器。
//
// 负责初始化 User/Relation RPC 客户端、JWT 管理器，并挂载到 ServiceContext
// 供所有 handler 复用，避免每个 handler 自己建连接。
package svc

import (
	"github.com/sponge-dad/feed/app/gateway/internal/config"
	relationClient "github.com/sponge-dad/feed/app/relation/rpc/relationclient"
	userClient "github.com/sponge-dad/feed/app/user/rpc/userClient"
	"github.com/sponge-dad/feed/common/jwtx"

	"github.com/zeromicro/go-zero/zrpc"
)

// ServiceContext Gateway 依赖容器。
type ServiceContext struct {
	Config         config.Config
	UserClient     userClient.User
	RelationClient relationClient.Relation
	JwtManager     *jwtx.Manager
}

// NewServiceContext 根据配置初始化所有 RPC 客户端和工具。
func NewServiceContext(c config.Config) *ServiceContext {
	return &ServiceContext{
		Config:         c,
		UserClient:     userClient.NewUser(zrpc.MustNewClient(c.UserRpc)),
		RelationClient: relationClient.NewRelation(zrpc.MustNewClient(c.RelationRpc)),
		JwtManager:     jwtx.NewManager(c.JwtAuth.AccessSecret, c.JwtAuth.AccessExpireHour),
	}
}
