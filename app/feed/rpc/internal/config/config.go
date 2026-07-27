package config

import (
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	Mysql struct {
		DataSource string
	}
	CacheRedis  cache.CacheConf
	RelationRpc zrpc.RpcClientConf
	RocketMQ    struct {
		NameServer []string
		GroupName  string
	}
}
