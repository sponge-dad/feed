// Package logic 实现 Feed 服务的所有 gRPC 业务逻辑。
// getuserfeedslogic.go 提供指定用户个人主页帖子的分页查询（按发布时间倒序）。
package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"
	"github.com/zeromicro/go-zero/core/logx"
)

// GetUserFeedsLogic 封装 GetUserFeeds 请求所需的上下文与依赖。
type GetUserFeedsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

// NewGetUserFeedsLogic 构造 GetUserFeedsLogic。
func NewGetUserFeedsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserFeedsLogic {
	return &GetUserFeedsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetUserFeeds 分页查询用户个人主页帖子。
// 采用 offset 分页：offset = (page-1)*pageSize，与 model.FindByUserId 对齐。
func (l *GetUserFeedsLogic) GetUserFeeds(in *feed.GetUserFeedsReq) (*feed.GetUserFeedsResp, error) {
	if in.UserId <= 0 {
		return nil, errorx.New(errorx.ParamError)
	}
	page := in.Page
	if page <= 0 {
		page = 1
	}
	pageSize := int64(in.PageSize)
	if pageSize <= 0 {
		pageSize = defaultUserFeedPageSize
	}
	if pageSize > maxUserFeedPageSize {
		pageSize = maxUserFeedPageSize
	}
	offset := (page - 1) * pageSize

	feeds, err := l.svcCtx.FeedModel.FindByUserId(l.ctx, uint64(in.UserId), uint64(pageSize), uint64(offset))
	if err != nil {
		return nil, err
	}
	briefs := make([]*feed.FeedBrief, 0, len(feeds))
	for _, f := range feeds {
		briefs = append(briefs, toFeedBrief(f))
	}
	// offset 分页无游标，以本页是否取满作为是否还有更多的粗略判断。
	hasMore := int64(len(briefs)) >= pageSize
	return &feed.GetUserFeedsResp{
		Feeds: briefs,
		Page:  &feed.PageInfo{Cursor: "", HasMore: hasMore},
	}, nil
}
