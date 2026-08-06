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
		NameServer   []string
		GroupName    string
		ConsumeGroup string
	}
	// Trace 请求级 Trace 配置（见 02-request-trace §6）。
	Trace struct {
		// SampleRate 采样率：<=0 全部跳过；<1 按比例采样；1 全量。开发默认 1，生产建议 0.1。
		SampleRate float64 `json:",default=1"`
		// TTL Trace Redis key 过期秒数。默认 24h，生产建议 1800。
		TTL int `json:",default=86400"`
		// InternalUserIDs 可越权查询任意 request_id Trace 的内部用户（如排障后台）。
		// 为空时仅允许查询属于自己的 request_id（归属校验）。
		InternalUserIDs []int64 `json:",optional"`
	} `json:",optional"`
}
