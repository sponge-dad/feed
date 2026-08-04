// config.go
//
// 职责：Relation RPC 服务的配置结构体，与 etc/relation.yaml 中的字段一一对应。
// goctl 只生成了 zrpc.RpcServerConf（端口、etcd 等框架自带配置），
// 数据库/缓存/VIP 阈值等业务相关配置需要手动补充。
package config

import (
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/zrpc"
)

// Config Relation RPC 服务配置。
type Config struct {
	zrpc.RpcServerConf

	// Mysql 数据库连接配置
	Mysql struct {
		// DataSource 标准 MySQL DSN
		DataSource string
	}

	// CacheRedis go-zero model 层 Cache-Aside 缓存配置
	CacheRedis cache.CacheConf

	// Vip 大V判定相关配置
	Vip struct {
		// FansThreshold 判定为大V的粉丝数阈值（>= 该值就算大V）
		FansThreshold int64
	}
}
