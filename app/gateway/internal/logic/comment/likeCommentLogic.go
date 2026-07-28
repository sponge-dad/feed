// likecommentlogic.go
//
// 职责：点赞评论。
// 说明：Comment RPC（api/proto/comment/comment.proto）当前未提供评论点赞方法，
// 路由按契约（docs/design/api-spec/comment.md）先行暴露，待下游补齐后接入；
// 当前统一返回明确的业务错误，避免误导客户端。
package comment

import (
	"context"

	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

type LikeCommentLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLikeCommentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LikeCommentLogic {
	return &LikeCommentLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// LikeComment 点赞评论（下游能力未就绪，暂返回业务错误）。
func (l *LikeCommentLogic) LikeComment(req *types.LikeCommentReq) (*types.LikeCommentResp, error) {
	me := middleware.MustGetUserID(l.ctx)
	if me == 0 {
		return nil, errorx.New(errorx.Unauthorized)
	}
	if req.CommentID <= 0 {
		return nil, errorx.NewWithMsg(errorx.ParamError, "commentId 非法")
	}

	l.Infof("comment: like comment %d by user %d rejected, downstream not ready", req.CommentID, me)
	return nil, errorx.NewWithMsg(errorx.ServerError, "评论点赞功能暂未开放")
}
