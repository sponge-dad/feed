// createcommentlogic.go
//
// 职责：发表评论/回复。转发 Comment.CreateComment；
// 若下游未填充作者昵称，则回查 User 服务补齐（失败降级为空）。
package comment

import (
	"context"
	"strings"

	commentClient "github.com/sponge-dad/feed/app/comment/rpc/commentClient"
	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	userClient "github.com/sponge-dad/feed/app/user/rpc/userClient"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

type CreateCommentLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreateCommentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateCommentLogic {
	return &CreateCommentLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// CreateComment 发表评论或回复。
func (l *CreateCommentLogic) CreateComment(req *types.CreateCommentReq) (*types.CreateCommentResp, error) {
	me := middleware.MustGetUserID(l.ctx)
	if me == 0 {
		return nil, errorx.New(errorx.Unauthorized)
	}
	if req.FeedID <= 0 {
		return nil, errorx.NewWithMsg(errorx.ParamError, "feedId 非法")
	}
	if strings.TrimSpace(req.Content) == "" {
		return nil, errorx.New(errorx.CommentEmpty)
	}

	// 回复场景：优先使用 parent_id；只传 root_id 时视为回复一级评论本身。
	parentID := req.ParentID
	if parentID == 0 {
		parentID = req.RootID
	}

	rpcResp, err := l.svcCtx.CommentRpc.CreateComment(l.ctx, &commentClient.CreateCommentReq{
		UserId:   me,
		FeedId:   req.FeedID,
		Content:  req.Content,
		ParentId: parentID,
	})
	if err != nil {
		return nil, err
	}

	c := rpcResp.Comment
	detail := types.CommentDetail{
		ID:       c.CommentId,
		FeedID:   c.FeedId,
		Content:  c.Content,
		RootID:   c.RootId,
		ParentID: c.ParentId,
		Author: types.CommentAuthor{
			ID:       c.UserId,
			Nickname: c.UserNickname,
			Avatar:   c.UserAvatar,
		},
		ReplyUser: types.CommentReplyUser{
			ID:       c.ReplyUserId,
			Nickname: c.ReplyUserNickname,
		},
		LikeCount: c.LikeCount,
		CreatedAt: c.CreatedAt,
	}

	// 下游未填充作者信息时回查补齐，失败降级为空。
	if detail.Author.Nickname == "" {
		if uresp, uerr := l.svcCtx.UserRpc.GetUser(l.ctx, &userClient.GetUserReq{UserId: me}); uerr != nil {
			l.Infof("comment: fill author info degrade to empty: %v", uerr)
		} else if uresp.User != nil {
			detail.Author.Nickname = uresp.User.Nickname
			detail.Author.Avatar = uresp.User.Avatar
		}
	}

	return &types.CreateCommentResp{Comment: detail}, nil
}
