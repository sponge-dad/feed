// getCommentCountLogic.go
//
// 职责：单帖评论总数查询。Cache-Aside：优先 Redis comment_count:{feed_id}，
// 未命中回源 MySQL COUNT 并回写（TTL 1h）。规范见 docs/design/comment/05-stats.md。
package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/comment/rpc/comment"
	"github.com/sponge-dad/feed/app/comment/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetCommentCountLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetCommentCountLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetCommentCountLogic {
	return &GetCommentCountLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetCommentCount 返回帖子可见评论总数（一级 + 子回复）。
func (l *GetCommentCountLogic) GetCommentCount(in *comment.GetCommentCountReq) (*comment.GetCommentCountResp, error) {
	if in.FeedId <= 0 {
		return nil, errorx.New(errorx.ParamError)
	}
	count, err := getCommentCount(l.ctx, l.svcCtx, in.FeedId)
	if err != nil {
		return nil, err
	}
	return &comment.GetCommentCountResp{Count: count}, nil
}
