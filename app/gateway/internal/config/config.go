// Package config 定义 Gateway HTTP 服务的配置结构体，
// 字段与 app/gateway/etc/gateway.yaml 一一对应。
package config

import (
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/zrpc"
)

// Config Gateway 服务配置。
type Config struct {
	rest.RestConf

	// UserRpc User gRPC 服务客户端配置（通过 etcd 服务发现）。
	UserRpc zrpc.RpcClientConf

	// RelationRpc Relation gRPC 服务客户端配置（用于用户主页聚合）。
	RelationRpc zrpc.RpcClientConf

	// JwtAuth JWT 解析所需配置，必须与 User RPC 服务保持一致。
	//
	// 注意：这里用 JwtAuth 而不是 Auth，避免与 rest.RestConf 内嵌的 Auth 字段撞名。
	JwtAuth struct {
		AccessSecret     string
		AccessExpireHour int
	}
}
