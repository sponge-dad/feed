// Package config 定义 Gateway 的配置结构。
package config

import (
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	rest.RestConf

	// Auth JWT 鉴权配置，与 user.rpc 服务签发 token 用的密钥保持一致。
	Auth struct {
		AccessSecret string
		AccessExpire int64
	}

	// UserRpc user 服务 gRPC 客户端配置。
	UserRpc zrpc.RpcClientConf

	// RelationRpc relation 服务 gRPC 客户端配置。
	RelationRpc zrpc.RpcClientConf

	// FeedRpc feed 服务 gRPC 客户端配置。
	FeedRpc zrpc.RpcClientConf

	// CommentRpc comment 服务 gRPC 客户端配置。
	CommentRpc zrpc.RpcClientConf

	// InteractionRpc interaction 服务 gRPC 客户端配置。
	InteractionRpc zrpc.RpcClientConf

	// ContentRpc content 服务 gRPC 客户端配置（内容画像查询/反馈）。
	ContentRpc zrpc.RpcClientConf

	// IPLocation 同城流 IP 定位的兜底配置。
	// 本地/内网开发环境无法做真实 GeoIP 解析时，使用该默认城市；
	// 三项均为空时视为未配置，解析失败将返回业务码 12006。
	IPLocation struct {
		DefaultCityCode string
		DefaultCityName string
		DefaultProvince string
	}

	// Cos 腾讯云对象存储（COS）配置，详见 docs/design/oss/00-overview.md。
	Cos CosConf

	// RocketMQ 行为埋点生产者配置。
	// Gateway 把行为事件 SendSync 到 feed-behavior-event，由 Interaction RPC 消费。
	// 见 docs/design/agent/03-behavior-event.md
	RocketMQ struct {
		NameServer []string
		GroupName  string
	}

	// BehaviorRedis 行为埋点限流用 Redis（与业务缓存同实例即可）。
	BehaviorRedis redis.RedisConf

	// Behavior 行为埋点上报配置。
	Behavior struct {
		// RateLimitPerUserPerMin 单用户每分钟可上报的事件条数上限（默认 300）。
		// 见 docs/design/agent/03-behavior-event.md §3。
		RateLimitPerUserPerMin int
	}
}

// CosConf 定义腾讯云 COS 相关配置。
// SecretId/SecretKey 生产环境必须来自环境变量（如 COS_SECRET_ID），
// 禁止在仓库 YAML 中写入明文（见 AGENTS.md §6.7）。
type CosConf struct {
	Bucket       string // 桶名，如 feed-1250000000
	Region       string // 地域，如 ap-guangzhou
	SecretId     string // 主/子账号 SecretId，生产来自环境变量
	SecretKey    string // 主/子账号 SecretKey，生产来自环境变量
	Env          string // 环境标识 dev/test/prod，用于 file_key 前缀
	StsDuration  int64  // STS 临时凭证有效期(秒)，默认 3600
	SignDuration int64  // 下载签名 URL 有效期(秒)，默认 600
	BaseURL      string // 对外访问域名（bucket 域名或 CDN 域名）
}
