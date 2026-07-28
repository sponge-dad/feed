// Package svc 定义 Gateway 的依赖注入容器。
package svc

import (
	commentClient "github.com/sponge-dad/feed/app/comment/rpc/commentClient"
	feedClient "github.com/sponge-dad/feed/app/feed/rpc/feedclient"
	"github.com/sponge-dad/feed/app/gateway/internal/config"
	interactionClient "github.com/sponge-dad/feed/app/interaction/rpc/interactionClient"
	relationClient "github.com/sponge-dad/feed/app/relation/rpc/relationclient"
	userClient "github.com/sponge-dad/feed/app/user/rpc/userClient"
	"github.com/sponge-dad/feed/common/ipx"

	"github.com/zeromicro/go-zero/zrpc"
)

// ServiceContext 是 Gateway 的依赖注入容器，持有所有下游 RPC 客户端。
type ServiceContext struct {
	Config         config.Config
	UserRpc        userClient.User
	RelationRpc    relationClient.Relation
	FeedRpc        feedClient.Feed
	CommentRpc     commentClient.Comment
	InteractionRpc interactionClient.Interaction

	// IPResolver 请求 IP -> 城市 的解析器（同城流、发帖 IP 属地用）。
	IPResolver ipx.Resolver
}

func NewServiceContext(c config.Config) *ServiceContext {
	var defaultCity *ipx.Location
	if c.IPLocation.DefaultCityCode != "" {
		defaultCity = &ipx.Location{
			CityCode: c.IPLocation.DefaultCityCode,
			CityName: c.IPLocation.DefaultCityName,
			Province: c.IPLocation.DefaultProvince,
		}
	}

	return &ServiceContext{
		Config:         c,
		UserRpc:        userClient.NewUser(zrpc.MustNewClient(c.UserRpc)),
		RelationRpc:    relationClient.NewRelation(zrpc.MustNewClient(c.RelationRpc)),
		FeedRpc:        feedClient.NewFeed(zrpc.MustNewClient(c.FeedRpc)),
		CommentRpc:     commentClient.NewComment(zrpc.MustNewClient(c.CommentRpc)),
		InteractionRpc: interactionClient.NewInteraction(zrpc.MustNewClient(c.InteractionRpc)),
		IPResolver:     ipx.NewStaticResolver(defaultCity),
	}
}
