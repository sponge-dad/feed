package svc

import (
	"github.com/sponge-dad/feed/app/gateway/internal/config"
	relationClient "github.com/sponge-dad/feed/app/relation/rpc/relationclient"
	userClient "github.com/sponge-dad/feed/app/user/rpc/userClient"

	"github.com/zeromicro/go-zero/zrpc"
)

// ServiceContext 是 Gateway 的依赖注入容器，持有所有下游 RPC 客户端。
type ServiceContext struct {
	Config      config.Config
	UserRpc     userClient.User
	RelationRpc relationClient.Relation
}

func NewServiceContext(c config.Config) *ServiceContext {
	return &ServiceContext{
		Config:      c,
		UserRpc:     userClient.NewUser(zrpc.MustNewClient(c.UserRpc)),
		RelationRpc: relationClient.NewRelation(zrpc.MustNewClient(c.RelationRpc)),
	}
}
