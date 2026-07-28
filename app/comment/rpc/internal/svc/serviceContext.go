// Package svc 组装 Comment 服务的依赖上下文（DB / Redis / 下游 RPC / MQ / ID 生成器），
// 供 logic 层通过 ServiceContext 统一访问，便于单元测试用桩替换。
package svc

import (
	"github.com/sponge-dad/feed/app/comment/model"
	"github.com/sponge-dad/feed/app/comment/rpc/internal/config"
	"github.com/sponge-dad/feed/app/feed/rpc/feedclient"
	"github.com/sponge-dad/feed/app/user/rpc/userClient"
	"github.com/sponge-dad/feed/common/idgen"
	"github.com/sponge-dad/feed/common/mq"

	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"github.com/zeromicro/go-zero/zrpc"
)

// ServiceContext Comment 服务依赖集合。
type ServiceContext struct {
	Config       config.Config
	CommentModel model.CommentsModel
	Redis        *redis.Redis
	IdGen        func() int64
	UserRpc      userClient.User
	FeedRpc      feedclient.Feed
	Producer     *mq.Producer
}

// NewServiceContext 按配置构造依赖上下文。
func NewServiceContext(c config.Config) (*ServiceContext, error) {
	conn := sqlx.NewMysql(c.Mysql.DataSource)
	// CacheRedis 第一个节点用于业务级 Redis 操作（comment_count / comment_hot）
	rds := redis.MustNewRedis(c.CacheRedis[0].RedisConf)
	producer, err := mq.NewProducer(c.RocketMQ.NameServer, c.RocketMQ.GroupName)
	if err != nil {
		return nil, err
	}
	return &ServiceContext{
		Config: c,
		// 评论内容不缓存（见 docs/design/comment/06-cache.md），model 关闭主键缓存
		CommentModel: model.NewCommentsModel(conn),
		Redis:        rds,
		IdGen:        idgen.Next,
		UserRpc:      userClient.NewUser(zrpc.MustNewClient(c.UserRpc)),
		FeedRpc:      feedclient.NewFeed(zrpc.MustNewClient(c.FeedRpc)),
		Producer:     producer,
	}, nil
}
