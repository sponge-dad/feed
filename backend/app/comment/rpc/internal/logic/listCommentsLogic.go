// listCommentsLogic.go
//
// 职责：帖子详情页首屏的一级评论分页（Offset 分页，时间倒序），
// 每楼附前 N 条子回复预览（一条窗口函数 SQL 批量取，禁止逐楼 N+1），
// 响应附带 comment_count 总数，用户昵称头像一次批量 RPC 填充。
// 规范见 docs/design/comment/04-list.md。
package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/comment/rpc/comment"
	"github.com/sponge-dad/feed/app/comment/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListCommentsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListCommentsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListCommentsLogic {
	return &ListCommentsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// ListComments 一级评论分页，每楼含前 N 条子回复预览。空帖返回空列表与 total=0，不报错。
func (l *ListCommentsLogic) ListComments(in *comment.ListCommentsReq) (*comment.ListCommentsResp, error) {
	if in.FeedId <= 0 {
		return nil, errorx.New(errorx.ParamError)
	}
	page := in.Page
	if page <= 0 {
		page = 1
	}
	pageSize := in.PageSize
	if pageSize <= 0 {
		pageSize = defaultPageSize
	} else if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	previewCount := in.PreviewCount
	if previewCount <= 0 {
		previewCount = defaultPreview
	} else if previewCount > maxPreview {
		previewCount = maxPreview
	}

	// 一级评论分页（feed_id + root_id=0 + status=1，时间倒序）
	roots, err := l.svcCtx.CommentModel.FindRootsByFeedId(l.ctx,
		uint64(in.FeedId), uint64(pageSize), uint64((page-1)*pageSize))
	if err != nil {
		return nil, err
	}

	// 帖子评论总数（Cache-Aside）
	total, err := getCommentCount(l.ctx, l.svcCtx, in.FeedId)
	if err != nil {
		return nil, err
	}

	if len(roots) == 0 {
		return &comment.ListCommentsResp{
			Comments: []*comment.CommentWithReplies{},
			Page:     &comment.PageInfo{Page: page, PageSize: pageSize, Total: total, HasMore: false},
		}, nil
	}

	// 批量取每楼前 N 条子回复预览（一条 SQL，内存按 root_id 分组，防 N+1）
	rootIds := make([]uint64, 0, len(roots))
	for _, r := range roots {
		rootIds = append(rootIds, r.Id)
	}
	previews, err := l.svcCtx.CommentModel.FindPreviewsByRootIds(l.ctx, rootIds, uint64(previewCount))
	if err != nil {
		return nil, err
	}
	previewByRoot := make(map[uint64][]*comment.CommentInfo, len(roots))
	allInfos := make([]*comment.CommentInfo, 0, len(roots)+len(previews))
	for _, p := range previews {
		info := toCommentInfo(p)
		previewByRoot[p.RootId] = append(previewByRoot[p.RootId], info)
		allInfos = append(allInfos, info)
	}

	items := make([]*comment.CommentWithReplies, 0, len(roots))
	for _, r := range roots {
		info := toCommentInfo(r)
		allInfos = append(allInfos, info)
		items = append(items, &comment.CommentWithReplies{
			Comment:        info,
			PreviewReplies: previewByRoot[r.Id],
			ReplyTotal:     int64(r.ReplyCount),
		})
	}

	// 一次批量 RPC 填充昵称头像（失败降级，不阻塞列表）
	fillUserInfos(l.ctx, l.svcCtx, allInfos)

	return &comment.ListCommentsResp{
		Comments: items,
		Page: &comment.PageInfo{
			Page:     page,
			PageSize: pageSize,
			Total:    total,
			// 本页取满视为可能有下一页；一级评论量级可控，无需额外 COUNT
			HasMore: int64(len(roots)) == pageSize,
		},
	}, nil
}
