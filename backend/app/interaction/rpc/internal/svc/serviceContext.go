// serviceContext.go
//
// 职责：Interaction RPC 服务的依赖装配（MySQL model、业务级 Redis、MQ 生产者/消费者、ID 生成器）。
// 单元测试中可用 miniredis + model stub + Publisher stub 替换真实依赖。
package svc

import (
	contentClient "github.com/sponge-dad/feed/app/content/rpc/contentClient"
	feedClient "github.com/sponge-dad/feed/app/feed/rpc/feedclient"
	"github.com/sponge-dad/feed/app/interaction/model"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/config"
	"github.com/sponge-dad/feed/common/idgen"
	"github.com/sponge-dad/feed/common/interceptors"
	"github.com/sponge-dad/feed/common/mq"

	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"github.com/zeromicro/go-zero/zrpc"
)

// Publisher MQ 生产者的最小接口抽象，*mq.Producer 天然满足；
// 单元测试用 stub 实现捕获事件，避免依赖真实 RocketMQ。
type Publisher interface {
	SendSync(topic string, body []byte) error
}

// ServiceContext Interaction 服务上下文。
type ServiceContext struct {
	Config                  config.Config
	Redis                   *redis.Redis
	LikesModel              model.LikesModel
	CollectionsModel        model.CollectionsModel
	Producer                Publisher
	Consumer                *mq.Consumer
	FeedRpc                 feedClient.Feed
	ContentRpc              contentClient.Content
	FeedBehaviorEventsModel model.FeedBehaviorEventsModel
	FeedMetricsHourlyModel  model.FeedMetricsHourlyModel
	UserInterestModel       model.UserInterestProfilesModel
	BehaviorConsumer        *mq.Consumer
	IdGen                   func() int64
	// InternalUsers 内部用户集合（O(1) 查询用 map）。
	InternalUsers map[int64]bool
}

// NewServiceContext 装配生产环境依赖。
func NewServiceContext(c config.Config) (*ServiceContext, error) {
	conn := sqlx.NewMysql(c.Mysql.DataSource)
	// CacheRedis 第一个节点用于业务级 Redis 操作（互动 Set/ZSet/Hash）
	rds := redis.MustNewRedis(c.CacheRedis[0].RedisConf)
	producer, err := mq.NewProducer(c.RocketMQ.NameServer, c.RocketMQ.GroupName)
	if err != nil {
		return nil, err
	}
	consumer, err := mq.NewConsumer(c.RocketMQ.NameServer, c.RocketMQ.ConsumeGroup)
	if err != nil {
		return nil, err
	}
	behaviorConsumer, err := mq.NewConsumer(c.RocketMQ.NameServer, c.Behavior.ConsumeGroup)
	if err != nil {
		return nil, err
	}
	return &ServiceContext{
		Config:           c,
		Redis:            rds,
		LikesModel:       model.NewLikesModel(conn, c.CacheRedis),
		CollectionsModel: model.NewCollectionsModel(conn, c.CacheRedis),
		Producer:         producer,
		Consumer:         consumer,
		// 必须挂 UnaryClientRequestID：否则 Interaction → Feed 这一跳会丢失 request_id，
		// 造成全链路追踪断链（其余服务的下游 client 均已注册）。
		FeedRpc: feedClient.NewFeed(zrpc.MustNewClient(c.FeedRpc,
			zrpc.WithUnaryClientInterceptor(interceptors.UnaryClientRequestID))),
		ContentRpc: contentClient.NewContent(zrpc.MustNewClient(c.ContentRpc,
			zrpc.WithUnaryClientInterceptor(interceptors.UnaryClientRequestID))),
		FeedBehaviorEventsModel: model.NewFeedBehaviorEventsModel(conn, c.CacheRedis),
		FeedMetricsHourlyModel:  model.NewFeedMetricsHourlyModel(conn, c.CacheRedis),
		UserInterestModel:       model.NewUserInterestProfilesModel(conn, c.CacheRedis),
		BehaviorConsumer:        behaviorConsumer,
		IdGen:                   idgen.Next,
		InternalUsers:           toUserSet(c.InternalUserIDs),
	}, nil
}

// toUserSet 内部用户 ID 列表 → map（O(1) 查询）。
func toUserSet(ids []int64) map[int64]bool {
	m := make(map[int64]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}

// IsInternal 是否内部用户（创作者指标/兴趣画像越权例外）。
func (s *ServiceContext) IsInternal(userID int64) bool {
	return s.InternalUsers[userID]
}
