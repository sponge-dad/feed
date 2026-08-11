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
	// ContentRpc 兴趣画像取内容标签（BatchGetContentProfile）与同类对比（SearchContent）用。
	ContentRpc zrpc.RpcClientConf
	// InternalUserIDs 内部用户集合（创作者指标/兴趣画像越权例外，见 08-creator-metrics.md）。
	InternalUserIDs []int64
	// Behavior 行为埋点消费配置（见 docs/design/agent/03-behavior-event.md）。
	Behavior struct {
		ConsumeGroup            string  // 行为事件消费组：behavior-persistence-consumer
		MetricsFlushIntervalSec int     // 小时指标落库刷新间隔（秒，默认 60）
		MetricsFlushBatch       int     // 单轮 SPOP dirty 集合上限（默认 500）
		ExposeSampleRate        float64 // EXPOSE 明细抽样率（0~1，默认 0.1）
		DetailRetentionDays     int     // 明细保留天数（默认 30）
		// Rule 行为判定阈值。放服务端的意义：客户端可被篡改，且口径变更无需发版。
		Rule BehaviorRule
	}
}

// BehaviorRule 行为判定阈值（见 03-behavior-event.md §2.1）。
// 服务端对 EFFECTIVE_PLAY / FINISH / SKIP 重新判定，客户端结论不作准。
type BehaviorRule struct {
	EffectivePlayMs    int64   // 有效播放绝对阈值（毫秒，默认 3000）
	EffectivePlayRatio float64 // 有效播放比例阈值（默认 0.5）
	FinishRatio        float64 // 完播比例阈值（默认 0.95）
	SkipMs             int64   // 快划阈值（毫秒，默认 3000）
}

// Fill 为未配置的阈值填充默认值，避免零值导致判定全部命中。
func (r *BehaviorRule) Fill() {
	if r.EffectivePlayMs <= 0 {
		r.EffectivePlayMs = 3000
	}
	if r.EffectivePlayRatio <= 0 {
		r.EffectivePlayRatio = 0.5
	}
	if r.FinishRatio <= 0 {
		r.FinishRatio = 0.95
	}
	if r.SkipMs <= 0 {
		r.SkipMs = 3000
	}
}
