// deleteCommentLogic.go
//
// 职责：删除评论（软删除，方案 A：删根评论保留子回复）。
// 流程：权限校验（仅作者可删）-> 事务软删（受影响行数做幂等，子回复联动
// 根评论 reply_count-1，删根评论统计整楼减量）-> comment_count DECR（非负保护）
// -> comment_hot ZREM -> 发送 comment.event(DELETE)。
// 规范见 docs/design/comment/03-delete.md。
package logic

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/sponge-dad/feed/app/comment/model"
	"github.com/sponge-dad/feed/app/comment/rpc/comment"
	"github.com/sponge-dad/feed/app/comment/rpc/internal/keys"
	"github.com/sponge-dad/feed/app/comment/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"
	commentEvent "github.com/sponge-dad/feed/common/event/comment"
	"github.com/sponge-dad/feed/common/requestid"

	"github.com/zeromicro/go-zero/core/logx"
)

type DeleteCommentLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDeleteCommentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteCommentLogic {
	return &DeleteCommentLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// DeleteComment 软删除评论；重复删除幂等返回成功，不重复减计数。
func (l *DeleteCommentLogic) DeleteComment(in *comment.DeleteCommentReq) (*comment.DeleteCommentResp, error) {
	if in.CommentId <= 0 || in.UserId <= 0 {
		return nil, errorx.New(errorx.ParamError)
	}

	record, err := l.svcCtx.CommentModel.FindOne(l.ctx, uint64(in.CommentId))
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return nil, errorx.New(errorx.CommentNotFound)
		}
		return nil, err
	}

	// 权限校验：仅作者本人可删（user_id 来自 metadata，禁止请求体透传）
	if int64(record.UserId) != in.UserId {
		return nil, errorx.New(errorx.CommentNoPermission)
	}

	// 已删除：幂等成功，不重复减计数
	if record.Status == model.CommentStatusDeleted {
		return &comment.DeleteCommentResp{Success: true}, nil
	}

	// 事务软删；deleted=false 说明并发中已被删（幂等）
	deleted, decrement, err := l.svcCtx.CommentModel.SoftDelete(l.ctx, record)
	if err != nil {
		return nil, err
	}
	if !deleted {
		return &comment.DeleteCommentResp{Success: true}, nil
	}

	// 先 DB 后缓存：comment_count 减对应量（含整楼可见子回复），失败不阻塞
	applyCommentCountDelta(l.ctx, l.svcCtx, int64(record.FeedId), -decrement)
	// 从热门 ZSet 移除（仅一级评论会进 hot，子回复 ZREM 为无害幂等操作）
	if _, err := l.svcCtx.Redis.ZremCtx(l.ctx, keys.CommentHot(int64(record.FeedId)),
		strconv.FormatInt(in.CommentId, 10)); err != nil {
		l.Errorf("redis zrem comment_hot failed feedId=%d commentId=%d err=%v", record.FeedId, in.CommentId, err)
	}

	// 发送 DELETE 事件（失败不阻塞）
	sendCommentEvent(l.ctx, l.svcCtx, commentEvent.NewEventCommentDeleted(
		in.CommentId, int64(record.FeedId), in.UserId, int64(record.ParentId), int64(record.RootId),
		time.Now().UnixMilli(), requestid.FromContext(l.ctx)))

	return &comment.DeleteCommentResp{Success: true}, nil
}
