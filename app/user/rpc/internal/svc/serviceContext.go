// serviceContext.go
//
// 职责：依赖注入容器。整个 User RPC 服务里，所有 logic 需要用到的外部依赖
// （MySQL model、Redis 客户端、JWT 签发器）都在这里统一初始化一次，
// 然后通过 ServiceContext 挂载给每个 logic 使用，避免每个 logic 各自
// 重复建立数据库/Redis连接。
//
// 新增一个依赖（比如以后要接 RocketMQ 生产者）的步骤：
//  1. 在 Config 结构体里加对应配置字段
//  2. 在 NewServiceContext 里用配置初始化这个依赖
//  3. 挂到 ServiceContext 结构体的字段上
//  4. logic 里通过 l.svcCtx.XXX 访问
package svc

import (
	"github.com/sponge-dad/feed/app/user/model"
	"github.com/sponge-dad/feed/app/user/rpc/internal/config"
	"github.com/sponge-dad/feed/common/jwtx"

	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

// ServiceContext User RPC 服务的依赖容器。
type ServiceContext struct {
	Config config.Config

	// UserModel 封装 users 表的 CRUD（内置 Cache-Aside 缓存），
	// logic 层通过它读写用户数据，不直接写 SQL。
	UserModel model.UsersModel

	// Redis 用于 logic 层手动做业务级缓存（比如 GetUser 的整对象缓存），
	// 区别于 UserModel 内置的按主键/唯一索引缓存。
	Redis *redis.Redis

	// JwtManager 签发和解析登录 token，Register/Login 成功后调用 Generate。
	JwtManager *jwtx.Manager
}

// NewServiceContext 根据配置初始化所有依赖，服务启动时调用一次。
func NewServiceContext(c config.Config) *ServiceContext {
	// 初始化 MySQL 连接
	conn := sqlx.NewMysql(c.Mysql.DataSource)

	// 初始化 Redis 客户端：CacheRedis 配置的第一个节点用于业务级缓存读写。
	// UserModel 内部会复用 c.CacheRedis 自己再初始化一份连接（go-zero cache层要求），
	// 这里额外持有一份是为了给 logic 层做 UserModel 覆盖不到的自定义缓存操作。
	rds := redis.MustNewRedis(c.CacheRedis[0].RedisConf)

	return &ServiceContext{
		Config:     c,
		UserModel:  model.NewUsersModel(conn, c.CacheRedis),
		Redis:      rds,
		JwtManager: jwtx.NewManager(c.Auth.AccessSecret, c.Auth.AccessExpireHour),
	}
}
