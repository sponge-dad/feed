// Package mocks 提供 gomock 生成的下游 RPC Client mock。
// 本文件为手写辅助代码（非 mockgen 生成），提供 Logic 单元测试共用的 ServiceContext 构造器。
package mocks

import (
	"github.com/golang/mock/gomock"

	"github.com/sponge-dad/feed/app/gateway/internal/config"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/common/ipx"
)

// NewTestServiceContext 构造注入全部 mock RPC 客户端的 ServiceContext，供 Gateway logic 单元测试使用。
// IPResolver 使用静态解析器（默认城市），CreateFeed/IP 属地解析可正常返回城市，便于断言。
// Config.Cos 预置 env=dev 与基础 BaseURL，使 COS 资源归属校验（ownsFileKey）在测试 key 形如
// "dev/{biz}/{uid}/..." 时可正常放行；Cos 仍为 nil，跳过「是否存在」的远端校验。
func NewTestServiceContext(ctrl *gomock.Controller) *svc.ServiceContext {
	return &svc.ServiceContext{
		UserRpc:        NewMockUser(ctrl),
		RelationRpc:    NewMockRelation(ctrl),
		FeedRpc:        NewMockFeed(ctrl),
		CommentRpc:     NewMockComment(ctrl),
		InteractionRpc: NewMockInteraction(ctrl),
		IPResolver:     ipx.NewStaticResolver(&ipx.Location{CityCode: "440300", CityName: "深圳市", Province: "广东省"}),
		Config: config.Config{
			Cos: config.CosConf{
				Env:          "dev",
				BaseURL:      "https://feed-test-bucket.cos.ap-guangzhou.myqcloud.com",
				SignDuration: 600,
			},
		},
	}
}
