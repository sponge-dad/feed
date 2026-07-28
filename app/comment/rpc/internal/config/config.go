// Package config 定义 Comment RPC 服务的配置结构，与 etc/comment.yaml 一一对应。
package config

import (
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/zrpc"
)

// Config Comment 服务配置。
type Config struct {
	zrpc.RpcServerConf
	// Mysql 评论库连接配置（feed_comment）。
	Mysql struct {
		DataSource string
	}
	// CacheRedis 业务级 Redis（comment_count / comment_hot），第一个节点用于业务操作。
	CacheRedis cache.CacheConf
	// UserRpc 用户服务客户端，列表查询时批量填充昵称头像。
	UserRpc zrpc.RpcClientConf
	// FeedRpc 帖子服务客户端，发表评论时校验帖子存在。
	FeedRpc zrpc.RpcClientConf
	// RocketMQ 生产者配置，发送 comment.event。
	RocketMQ struct {
		NameServer []string
		GroupName  string
	}
}
