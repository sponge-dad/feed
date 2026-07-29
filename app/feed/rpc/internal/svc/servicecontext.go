package svc

import (
	"github.com/sponge-dad/feed/app/feed/model"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/config"
	"github.com/sponge-dad/feed/app/relation/rpc/relationclient"
	"github.com/sponge-dad/feed/common/idgen"
	"github.com/sponge-dad/feed/common/mq"

	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"github.com/zeromicro/go-zero/zrpc"
)

// Publisher 事件发布接口。生产实现为 *mq.Producer；
// 单元测试可注入可记录调用的桩实现（与 interaction 服务的 Publisher 抽象保持一致）。
type Publisher interface {
	SendSync(topic string, body []byte) error
}

// ServiceContext Feed 服务依赖集合。
// 注意：Feed 服务不依赖 Comment RPC（单向依赖：Comment → Feed），避免循环依赖导致启动死锁。
type ServiceContext struct {
	Config      config.Config
	FeedModel   model.FeedsModel
	Redis       *redis.Redis
	IdGen       func() int64
	RelationRpc relationclient.Relation
	Producer    Publisher
	Consumer    *mq.Consumer
}

func NewServiceContext(c config.Config) (*ServiceContext, error) {
	conn := sqlx.NewMysql(c.Mysql.DataSource)
	// CacheRedis 第一个节点用于业务级 Redis 操作
	rds := redis.MustNewRedis(c.CacheRedis[0].RedisConf)
	producer, err := mq.NewProducer(c.RocketMQ.NameServer, c.RocketMQ.GroupName)
	if err != nil {
		return nil, err
	}
	consumer, err := mq.NewConsumer(c.RocketMQ.NameServer, c.RocketMQ.ConsumeGroup)
	if err != nil {
		return nil, err
	}
	return &ServiceContext{
		Config: c,
		// FeedModel 关闭 go-zero 自带主键缓存：详情缓存统一用业务级 feed:{feed_id} Hash（见 06-cache-strategy.md）
		FeedModel:   model.NewFeedsModel(conn, rds),
		Redis:       rds,
		IdGen:       idgen.Next,
		RelationRpc: relationclient.NewRelation(zrpc.MustNewClient(c.RelationRpc)),
		Producer:    producer,
		Consumer:    consumer,
	}, nil
}
