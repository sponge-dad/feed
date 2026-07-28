// listcommentslogic.go
//
// 职责：一级评论列表（含子回复预览）。
// 下游 Comment.ListComments 为 page 分页，网关用「页码 cursor」对外统一成 cursor 分页；
// 第一页时并行拉取热评（GetHotComments，失败降级为空）。
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
	"golang.org/x/sync/errgroup"
)

// hotCommentLimit 第一页返回的热评数量。
const hotCommentLimit = 3

// previewReplyCount 每条一级评论携带的子回复预览条数。
const previewReplyCount = 3

type ListCommentsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListCommentsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListCommentsLogic {
	return &ListCommentsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// ListComments 一级评论列表。
func (l *ListCommentsLogic) ListComments(req *types.ListCommentsReq) (*types.ListCommentsResp, error) {
	me := middleware.MustGetUserID(l.ctx)
	if me == 0 {
		return nil, errorx.New(errorx.Unauthorized)
	}
	if req.FeedID <= 0 {
		return nil, errorx.NewWithMsg(errorx.ParamError, "feedId 非法")
	}
	pageSize := aggregate.ClampPageSize(req.PageSize, 20, 50)
	page, err := aggregate.PageFromCursor(req.Cursor)
	if err != nil {
		return nil, err
	}

	var (
		listResp *commentClient.ListCommentsResp
		hotResp  *commentClient.GetHotCommentsResp
	)

	g, gctx := errgroup.WithContext(l.ctx)
	g.Go(func() error {
		resp, lerr := l.svcCtx.CommentRpc.ListComments(gctx, &commentClient.ListCommentsReq{
			FeedId:       req.FeedID,
			UserId:       me,
			Page:         page,
			PageSize:     pageSize,
			PreviewCount: previewReplyCount,
		})
		if lerr != nil {
			return lerr
		}
		listResp = resp
		return nil
	})
	if page == 1 {
		// 热评仅第一页返回，失败降级为空。
		g.Go(func() error {
			resp, herr := l.svcCtx.CommentRpc.GetHotComments(gctx, &commentClient.GetHotCommentsReq{
				FeedId: req.FeedID,
				UserId: me,
				Limit:  hotCommentLimit,
			})
			if herr != nil {
				l.Errorf("comment: GetHotComments degrade to empty: %v", herr)
				return nil
			}
			hotResp = resp
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	resp := &types.ListCommentsResp{
		HotComments: []types.CommentEntry{},
		List:        make([]types.CommentEntry, 0, len(listResp.Comments)),
	}
	for _, cw := range listResp.Comments {
		if cw == nil || cw.Comment == nil {
			continue
		}
		resp.List = append(resp.List, toCommentEntry(cw.Comment, cw.ReplyTotal, cw.PreviewReplies))
	}
	if hotResp != nil {
		for _, hc := range hotResp.Comments {
			if hc == nil {
				continue
			}
			resp.HotComments = append(resp.HotComments, toCommentEntry(hc, hc.ReplyCount, nil))
		}
	}

	var hasMore bool
	if listResp.Page != nil {
		hasMore = listResp.Page.HasMore
	}
	resp.HasMore = hasMore
	resp.NextCursor = aggregate.NextPageCursor(page, hasMore)
	return resp, nil
}
