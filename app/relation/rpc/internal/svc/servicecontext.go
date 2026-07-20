// servicecontext.go
//
// 职责：Relation RPC 服务的依赖注入容器。集中初始化 MySQL model、Redis、
// Snowflake ID 生成器，供所有 logic 方法复用。
package svc

import (
	"github.com/sponge-dad/feed/app/relation/model"
	"github.com/sponge-dad/feed/app/relation/rpc/internal/config"
	"github.com/sponge-dad/feed/common/idgen"

	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

// ServiceContext Relation RPC 服务的依赖容器。
type ServiceContext struct {
	Config config.Config

	// RelationModel 封装 relations 表的 CRUD（内置 Cache-Aside 缓存）
	RelationModel model.RelationsModel

	// Redis 用于业务级缓存：关注/粉丝列表、粉丝数、大V集合
	Redis *redis.Redis

	// IdGen 用于生成关系记录的 Snowflake ID。
	// 这里调用的是全局函数，服务启动时需要在 main 里调用 idgen.Init 一次。
	// 注意：同一集群内各 Pod 实例传入的 machineID 必须不同，K8s 场景下
	// 通常由环境变量/StatefulSet 序号注入。
	IdGen func() int64
}

// NewServiceContext 根据配置初始化所有依赖，服务启动时调用一次。
func NewServiceContext(c config.Config) *ServiceContext {
	conn := sqlx.NewMysql(c.Mysql.DataSource)

	// CacheRedis 第一个节点用于业务级 Redis 操作
	rds := redis.MustNewRedis(c.CacheRedis[0].RedisConf)

	return &ServiceContext{
		Config:        c,
		RelationModel: model.NewRelationsModel(conn, c.CacheRedis),
		Redis:         rds,
		IdGen:         idgen.Next,
	}
}
