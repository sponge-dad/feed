// Package config 定义 Content RPC 服务的配置结构。
package config

import (
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/zrpc"
)

// Config Content 服务配置。
type Config struct {
	zrpc.RpcServerConf
	// Mysql 业务库连接（feed_content）。
	Mysql struct {
		DataSource string
	}
	// CacheRedis go-zero 缓存配置；第一个节点同时用作业务级 Redis（画像缓存、分析锁）。
	CacheRedis cache.CacheConf
	// Elasticsearch 检索索引配置（feed_content_v1）。
	Elasticsearch struct {
		Addr string // 如 http://127.0.0.1:9200
	}
	// InternalUserIDs 内部用户白名单（可查看字幕/OCR 全文、执行重试分析）。
	InternalUserIDs []int64
	// CategoryWhitelist 类目白名单（SearchContent 校验：非法类目降级为不限类目）。
	CategoryWhitelist []string
	// FeedRpc 回查 Feed 存在性/状态（SearchContent 结果校验用）。
	FeedRpc zrpc.RpcClientConf
	// RocketMQ 生产者（RetryContentAnalysis 重新入队 feed-created 用）。
	RocketMQ struct {
		NameServer []string
		GroupName  string
	}
}

// IsInternal 是否内部用户。
func (c *Config) IsInternal(userID int64) bool {
	for _, id := range c.InternalUserIDs {
		if id == userID {
			return true
		}
	}
	return false
}

// IsValidCategory 类目是否在白名单内（SearchContent 校验用）。
func (c *Config) IsValidCategory(category string) bool {
	for _, cate := range c.CategoryWhitelist {
		if cate == category {
			return true
		}
	}
	return false
}
