// Package config 定义 Interaction RPC 服务的配置结构。
package config

import (
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/zrpc"
)

// Config Interaction 服务配置。
type Config struct {
	zrpc.RpcServerConf
	// Mysql 业务库连接（feed_interaction）。
	Mysql struct {
		DataSource string
	}
	// CacheRedis go-zero 缓存配置（数组，单机写一个节点），
	// 第一个节点同时用作业务级 Redis（互动 Set/ZSet/Hash）。
	CacheRedis cache.CacheConf
	// RocketMQ 生产者/消费者配置。
	RocketMQ struct {
		NameServer   []string
		GroupName    string // 生产者组：interaction_producer_group
		ConsumeGroup string // 持久化消费组：interaction-persistence-consumer
	}
}
