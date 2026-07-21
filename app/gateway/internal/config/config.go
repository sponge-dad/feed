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
}
