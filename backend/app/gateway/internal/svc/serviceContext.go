// Package svc 定义 Gateway 的依赖注入容器。
package svc

import (
	commentClient "github.com/sponge-dad/feed/app/comment/rpc/commentClient"
	contentClient "github.com/sponge-dad/feed/app/content/rpc/contentClient"
	feedClient "github.com/sponge-dad/feed/app/feed/rpc/feedclient"
	"github.com/sponge-dad/feed/app/gateway/internal/config"
	"github.com/sponge-dad/feed/app/gateway/internal/pkg/cos"
	interactionClient "github.com/sponge-dad/feed/app/interaction/rpc/interactionClient"
	relationClient "github.com/sponge-dad/feed/app/relation/rpc/relationclient"
	userClient "github.com/sponge-dad/feed/app/user/rpc/userClient"
	"github.com/sponge-dad/feed/common/interceptors"
	"github.com/sponge-dad/feed/common/ipx"
	"github.com/sponge-dad/feed/common/mq"

	"github.com/zeromicro/go-zero/core/stores/redis"
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
	ContentRpc     contentClient.Content

	// IPResolver 请求 IP -> 城市 的解析器（同城流、发帖 IP 属地用）。
	IPResolver ipx.Resolver

	// Cos 腾讯云 COS 客户端（STS 临时凭证签发 / 下载签名 URL）。
	Cos *cos.Client

	// Producer 行为埋点事件生产者（SendSync 到 feed-behavior-event）。
	Producer *mq.Producer

	// BehaviorRedis 行为埋点限流用 Redis。
	BehaviorRedis *redis.Redis

	// BehaviorRateLimit 单用户每分钟可上报的事件条数上限。
	BehaviorRateLimit int
}

// BehaviorRateWindowSec 行为埋点限流窗口（秒）。
const BehaviorRateWindowSec = 60

// defaultBehaviorRatePerMin 未配置时的单用户每分钟上报上限。
const defaultBehaviorRatePerMin = 300

func NewServiceContext(c config.Config) *ServiceContext {
	var defaultCity *ipx.Location
	if c.IPLocation.DefaultCityCode != "" {
		defaultCity = &ipx.Location{
			CityCode: c.IPLocation.DefaultCityCode,
			CityName: c.IPLocation.DefaultCityName,
			Province: c.IPLocation.DefaultProvince,
		}
	}

	rateLimit := c.Behavior.RateLimitPerUserPerMin
	if rateLimit <= 0 {
		rateLimit = defaultBehaviorRatePerMin
	}
	behaviorRds := redis.MustNewRedis(c.BehaviorRedis)

	return &ServiceContext{
		Config:            c,
		UserRpc:           userClient.NewUser(zrpc.MustNewClient(c.UserRpc, zrpc.WithUnaryClientInterceptor(interceptors.UnaryClientRequestID))),
		RelationRpc:       relationClient.NewRelation(zrpc.MustNewClient(c.RelationRpc, zrpc.WithUnaryClientInterceptor(interceptors.UnaryClientRequestID))),
		FeedRpc:           feedClient.NewFeed(zrpc.MustNewClient(c.FeedRpc, zrpc.WithUnaryClientInterceptor(interceptors.UnaryClientRequestID))),
		CommentRpc:        commentClient.NewComment(zrpc.MustNewClient(c.CommentRpc, zrpc.WithUnaryClientInterceptor(interceptors.UnaryClientRequestID))),
		InteractionRpc:    interactionClient.NewInteraction(zrpc.MustNewClient(c.InteractionRpc, zrpc.WithUnaryClientInterceptor(interceptors.UnaryClientRequestID))),
		ContentRpc:        contentClient.NewContent(zrpc.MustNewClient(c.ContentRpc, zrpc.WithUnaryClientInterceptor(interceptors.UnaryClientRequestID))),
		IPResolver:        ipx.NewStaticResolver(defaultCity),
		Cos:               cos.MustNew(c.Cos),
		Producer:          mustNewProducer(c.RocketMQ.NameServer, c.RocketMQ.GroupName),
		BehaviorRedis:     behaviorRds,
		BehaviorRateLimit: rateLimit,
	}
}

// mustNewProducer 创建并启动 RocketMQ 生产者；失败直接 panic（启动期致命错误）。
func mustNewProducer(nameServer []string, groupName string) *mq.Producer {
	producer, err := mq.NewProducer(nameServer, groupName)
	if err != nil {
		panic(err)
	}
	return producer
}
