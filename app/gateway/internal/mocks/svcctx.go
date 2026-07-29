// Package mocks 提供 gomock 生成的下游 RPC Client mock。
// 本文件为手写辅助代码（非 mockgen 生成），提供 Logic 单元测试共用的 ServiceContext 构造器。
package mocks

import (
	"github.com/golang/mock/gomock"

	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/common/ipx"
)

// NewTestServiceContext 构造注入全部 mock RPC 客户端的 ServiceContext，供 Gateway logic 单元测试使用。
// IPResolver 使用静态解析器（默认城市），CreateFeed/IP 属地解析可正常返回城市，便于断言。
func NewTestServiceContext(ctrl *gomock.Controller) *svc.ServiceContext {
	return &svc.ServiceContext{
		UserRpc:        NewMockUser(ctrl),
		RelationRpc:    NewMockRelation(ctrl),
		FeedRpc:        NewMockFeed(ctrl),
		CommentRpc:     NewMockComment(ctrl),
		InteractionRpc: NewMockInteraction(ctrl),
		IPResolver:     ipx.NewStaticResolver(&ipx.Location{CityCode: "440300", CityName: "深圳市", Province: "广东省"}),
	}
}
