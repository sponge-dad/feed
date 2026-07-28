// Package config 定义 Gateway 的配置结构。
package config

import (
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

	// IPLocation 同城流 IP 定位的兜底配置。
	// 本地/内网开发环境无法做真实 GeoIP 解析时，使用该默认城市；
	// 三项均为空时视为未配置，解析失败将返回业务码 12006。
	IPLocation struct {
		DefaultCityCode string `json:",optional"`
		DefaultCityName string `json:",optional"`
		DefaultProvince string `json:",optional"`
	} `json:",optional"`
}
