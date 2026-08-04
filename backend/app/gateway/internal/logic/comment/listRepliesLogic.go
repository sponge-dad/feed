// listreplieslogic.go
//
// 职责：某条一级评论的子回复列表。下游 Comment.ListReplies 原生 cursor 分页，直接透传。
package comment

import (
	"context"

	commentClient "github.com/sponge-dad/feed/app/comment/rpc/commentClient"
	"github.com/sponge-dad/feed/app/gateway/internal/logic/aggregate"
	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListRepliesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListRepliesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListRepliesLogic {
	return &ListRepliesLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// ListReplies 子回复列表。
func (l *ListRepliesLogic) ListReplies(req *types.ListRepliesReq) (*types.ListRepliesResp, error) {
	me := middleware.MustGetUserID(l.ctx)
	if me == 0 {
		return nil, errorx.New(errorx.Unauthorized)
	}
	if req.RootID <= 0 {
		return nil, errorx.NewWithMsg(errorx.ParamError, "rootId 非法")
	}
	pageSize := aggregate.ClampPageSize(req.PageSize, 20, 50)

	rpcResp, err := l.svcCtx.CommentRpc.ListReplies(l.ctx, &commentClient.ListRepliesReq{
		RootId:   req.RootID,
		UserId:   me,
		Cursor:   req.Cursor,
		PageSize: pageSize,
	})
	if err != nil {
		return nil, err
	}

	resp := &types.ListRepliesResp{
		List: make([]types.CommentReply, 0, len(rpcResp.Replies)),
	}
	for _, r := range rpcResp.Replies {
		if r != nil {
			resp.List = append(resp.List, toCommentReply(r))
		}
	}
	if rpcResp.Page != nil {
		resp.NextCursor = rpcResp.Page.Cursor
		resp.HasMore = rpcResp.Page.HasMore
	}
	return resp, nil
}
