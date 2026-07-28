// unlikecommentlogic.go
//
// 职责：取消点赞评论。
// 说明：Comment RPC 当前未提供评论点赞方法，处理方式同 likecommentlogic.go。
package comment

import (
	"context"

	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnlikeCommentLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnlikeCommentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnlikeCommentLogic {
	return &UnlikeCommentLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// UnlikeComment 取消点赞评论（下游能力未就绪，暂返回业务错误）。
func (l *UnlikeCommentLogic) UnlikeComment(req *types.UnlikeCommentReq) (*types.UnlikeCommentResp, error) {
	me := middleware.MustGetUserID(l.ctx)
	if me == 0 {
		return nil, errorx.New(errorx.Unauthorized)
	}
	if req.CommentID <= 0 {
		return nil, errorx.NewWithMsg(errorx.ParamError, "commentId 非法")
	}

	l.Infof("comment: unlike comment %d by user %d rejected, downstream not ready", req.CommentID, me)
	return nil, errorx.NewWithMsg(errorx.ServerError, "评论点赞功能暂未开放")
}
