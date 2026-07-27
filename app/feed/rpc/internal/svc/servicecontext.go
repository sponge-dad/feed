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

type ServiceContext struct {
	Config      config.Config
	FeedModel   model.FeedsModel
	Redis       *redis.Redis
	IdGen       func() int64
	RelationRpc relationclient.Relation
	Producer    *mq.Producer
}

func NewServiceContext(c config.Config) (*ServiceContext, error) {
	conn := sqlx.NewMysql(c.Mysql.DataSource)
	// CacheRedis 第一个节点用于业务级 Redis 操作
	rds := redis.MustNewRedis(c.CacheRedis[0].RedisConf)
	producer, err := mq.NewProducer(c.RocketMQ.NameServer, c.RocketMQ.GroupName)
	if err != nil {
		return nil, err
	}
	return &ServiceContext{
		Config:      c,
		FeedModel:   model.NewFeedsModel(conn, c.CacheRedis),
		Redis:       rds,
		IdGen:       idgen.Next,
		RelationRpc: relationclient.NewRelation(zrpc.MustNewClient(c.RelationRpc)),
		Producer:    producer,
	}, nil
}
