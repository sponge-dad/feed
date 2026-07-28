// deletecommentlogic.go
//
// 职责：删除评论。Comment RPC 未提供单条评论查询接口，
// 网关无法预取作者信息，越权校验由 Comment 服务执行（userId 随请求下传，
// 非作者删除返回业务码 13003，网关原样透传）。
package comment

import (
	"context"

	commentClient "github.com/sponge-dad/feed/app/comment/rpc/commentClient"
	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

type DeleteCommentLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDeleteCommentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteCommentLogic {
	return &DeleteCommentLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// DeleteComment 删除自己的评论。
func (l *DeleteCommentLogic) DeleteComment(req *types.DeleteCommentReq) (*types.DeleteCommentResp, error) {
	me := middleware.MustGetUserID(l.ctx)
	if me == 0 {
		return nil, errorx.New(errorx.Unauthorized)
	}
	if req.CommentID <= 0 {
		return nil, errorx.NewWithMsg(errorx.ParamError, "commentId 非法")
	}

	rpcResp, err := l.svcCtx.CommentRpc.DeleteComment(l.ctx, &commentClient.DeleteCommentReq{
		CommentId: req.CommentID,
		UserId:    me,
	})
	if err != nil {
		return nil, err
	}

	return &types.DeleteCommentResp{Success: rpcResp.Success}, nil
}
