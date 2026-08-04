// listRepliesLogic.go
//
// 职责：「查看全部回复」——某根评论下全部可见子回复的游标分页（时间正序，
// created_at + id 组合游标，避免长列表翻页漏评/重复），昵称头像批量填充。
// 规范见 docs/design/comment/04-list.md 第 4 节。
package logic

import (
	"context"
	"errors"

	"github.com/sponge-dad/feed/app/comment/model"
	"github.com/sponge-dad/feed/app/comment/rpc/comment"
	"github.com/sponge-dad/feed/app/comment/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListRepliesLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListRepliesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListRepliesLogic {
	return &ListRepliesLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// ListReplies 楼内子回复游标分页；root_id 必须是可见的根评论，否则返回 CommentNotFound。
func (l *ListRepliesLogic) ListReplies(in *comment.ListRepliesReq) (*comment.ListRepliesResp, error) {
	if in.RootId <= 0 {
		return nil, errorx.New(errorx.ParamError)
	}
	pageSize := in.PageSize
	if pageSize <= 0 {
		pageSize = defaultPageSize
	} else if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	cursorCreatedAt, cursorID, ok := decodeReplyCursor(in.Cursor)
	if !ok {
		return nil, errorx.New(errorx.ParamError)
	}

	// 根评论必须存在且可见（status=1 的一级评论）
	root, err := l.svcCtx.CommentModel.FindOne(l.ctx, uint64(in.RootId))
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return nil, errorx.New(errorx.CommentNotFound)
		}
		return nil, err
	}
	if root.Status != model.CommentStatusNormal || root.RootId != 0 {
		return nil, errorx.New(errorx.CommentNotFound)
	}

	// 多取 1 条判断是否还有下一页
	rows, err := l.svcCtx.CommentModel.FindRepliesByCursor(l.ctx,
		uint64(in.RootId), cursorCreatedAt, cursorID, uint64(pageSize)+1)
	if err != nil {
		return nil, err
	}

	hasMore := int64(len(rows)) > pageSize
	if hasMore {
		rows = rows[:pageSize]
	}

	replies := make([]*comment.CommentInfo, 0, len(rows))
	for _, r := range rows {
		replies = append(replies, toCommentInfo(r))
	}
	fillUserInfos(l.ctx, l.svcCtx, replies)

	nextCursor := ""
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		nextCursor = encodeReplyCursor(last.CreatedAt, last.Id)
	}

	return &comment.ListRepliesResp{
		Replies: replies,
		Page: &comment.PageInfo{
			PageSize: pageSize,
			Total:    int64(root.ReplyCount),
			HasMore:  hasMore,
			Cursor:   nextCursor,
		},
	}, nil
}
