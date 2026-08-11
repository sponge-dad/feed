// Package svc 定义 Content RPC 服务的依赖装配。
package svc

import (
	"errors"

	feedClient "github.com/sponge-dad/feed/app/feed/rpc/feedclient"
	"github.com/sponge-dad/feed/app/content/rpc/internal/config"
	"github.com/sponge-dad/feed/app/content/model"
	"github.com/sponge-dad/feed/app/content/search"
	"github.com/sponge-dad/feed/common/interceptors"
	"github.com/sponge-dad/feed/common/mq"

	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"github.com/zeromicro/go-zero/zrpc"
)

// ES 索引读写别名（见 docs/design/agent/05-content-search.md §3）。
const (
	ESReadAlias  = "feed_content"
	ESWriteAlias = "feed_content_write"
)

// Publisher MQ 生产者的最小接口抽象（单元测试用 stub 捕获，避免依赖真实 RocketMQ）。
type Publisher interface {
	SendSync(topic string, body []byte) error
}

// ServiceContext Content 服务上下文。
type ServiceContext struct {
	Config               config.Config
	Redis                *redis.Redis
	ContentProfilesModel model.FeedContentProfilesModel
	Es                   *search.Client
	FeedRpc              feedClient.Feed
	Producer             Publisher
	// InternalUsers 内部用户白名单（查看完整画像 / 重试分析）。
	InternalUsers map[int64]bool
}

// NewServiceContext 装配生产环境依赖。
func NewServiceContext(c config.Config) (*ServiceContext, error) {
	if len(c.CacheRedis) == 0 {
		return nil, errors.New("CacheRedis config is empty")
	}
	conn := sqlx.NewMysql(c.Mysql.DataSource)
	rds := redis.MustNewRedis(c.CacheRedis[0].RedisConf)
	es, err := search.NewClient(c.Elasticsearch.Addr, ESReadAlias, ESWriteAlias)
	if err != nil {
		return nil, err
	}
	producer, err := mq.NewProducer(c.RocketMQ.NameServer, c.RocketMQ.GroupName)
	if err != nil {
		return nil, err
	}
	internal := make(map[int64]bool, len(c.InternalUserIDs))
	for _, id := range c.InternalUserIDs {
		internal[id] = true
	}
	return &ServiceContext{
		Config:               c,
		Redis:                rds,
		ContentProfilesModel: model.NewFeedContentProfilesModel(conn, c.CacheRedis),
		Es:                   es,
		FeedRpc: feedClient.NewFeed(zrpc.MustNewClient(c.FeedRpc,
			zrpc.WithUnaryClientInterceptor(interceptors.UnaryClientRequestID))),
		Producer:      producer,
		InternalUsers: internal,
	}, nil
}

// IsInternal 判断用户是否为内部白名单用户。
func (s *ServiceContext) IsInternal(userID int64) bool {
	return s.InternalUsers[userID]
}
