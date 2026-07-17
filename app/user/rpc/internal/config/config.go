// config.go
//
// 职责：User RPC 服务的配置结构体，与 etc/user.yaml 中的字段一一对应。
// goctl 只生成了 zrpc.RpcServerConf（端口、etcd 等 RPC 框架自带配置），
// 数据库/缓存/JWT 这些业务相关配置需要我们手动在这里补充字段，
// 并同步在 etc/user.yaml 里加上对应的配置项，否则启动时会用不到这些值。
package config

import (
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/zrpc"
)

// Config User RPC 服务配置。
type Config struct {
	zrpc.RpcServerConf

	// Mysql 数据库连接配置
	Mysql struct {
		// DataSource 标准 MySQL DSN，格式：
		// user:password@tcp(host:port)/dbname?charset=utf8mb4&parseTime=true&loc=Local
		DataSource string
	}

	// CacheRedis go-zero model 层 Cache-Aside 模式所需的 Redis 集群配置，
	// 类型是 cache.CacheConf（本质是 []NodeConf），支持配置多个 Redis 节点做分片，
	// 单机场景配置一个节点即可。
	CacheRedis cache.CacheConf

	// Auth JWT 签发与校验所需的密钥配置
	Auth struct {
		// AccessSecret 签名密钥，务必在生产环境用随机字符串替换，不要和其他服务共用
		AccessSecret string
		// AccessExpireHour token 过期时间（小时）
		AccessExpireHour int
	}
}
