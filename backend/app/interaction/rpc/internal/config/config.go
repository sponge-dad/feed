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
	// FeedRpc 行为消费侧重判 feed 状态（status==NORMAL）用。
	FeedRpc zrpc.RpcClientConf
	// Behavior 行为埋点消费配置（见 docs/design/agent/03-behavior-event.md）。
	Behavior struct {
		ConsumeGroup                string  // 行为事件消费组：behavior-persistence-consumer
		MetricsFlushIntervalSec     int     // 小时指标落库刷新间隔（秒）
		ExposeSampleRate            float64 // EXPOSE 明细抽样率（0~1）
		RateLimitPerUserPerSec      int     // 50/s/uid 全量（worker 兜底）
		RateLimitPerActionFeedPerSec int    // 5/s/uid+action+feed
	}
}
