// createCommentLogic.go
//
// 职责：发表评论 / 回复评论。
// 流程：参数与内容校验 -> 帖子存在性校验（Feed RPC）-> 父评论校验与
// root_id/parent_id/reply_user_id 推导 -> 单事务写 MySQL（子回复联动根评论
// reply_count+1）-> 更新 comment_count 缓存 -> 发送 comment.event(CREATE)。
// 规范见 docs/design/comment/02-publish.md。
package logic

import (
	"context"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/sponge-dad/feed/app/comment/model"
	"github.com/sponge-dad/feed/app/comment/rpc/comment"
	"github.com/sponge-dad/feed/app/comment/rpc/internal/svc"
	"github.com/sponge-dad/feed/app/feed/rpc/feedclient"
	"github.com/sponge-dad/feed/common/errorx"
	commentEvent "github.com/sponge-dad/feed/common/event/comment"
	"github.com/sponge-dad/feed/common/requestid"

	"github.com/zeromicro/go-zero/core/logx"
)

type CreateCommentLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateCommentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateCommentLogic {
	return &CreateCommentLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// CreateComment 发表一级评论或回复评论（楼中楼两层平铺存储）。
func (l *CreateCommentLogic) CreateComment(in *comment.CreateCommentReq) (*comment.CreateCommentResp, error) {
	// 参数校验：user_id 由上游从 metadata/JWT 提取，禁止信任客户端
	if in.UserId <= 0 || in.FeedId <= 0 || in.ParentId < 0 {
		return nil, errorx.New(errorx.ParamError)
	}
	if in.Content == "" {
		return nil, errorx.New(errorx.CommentEmpty)
	}
	if utf8.RuneCountInString(in.Content) > maxContentRunes {
		return nil, errorx.New(errorx.CommentTooLong)
	}

	// 业务校验：帖子必须存在
	if err := l.checkFeedExists(in.FeedId, in.UserId); err != nil {
		return nil, err
	}

	// 推导 root_id / reply_user_id（见 02-publish.md 第 2 节字段填充表）
	var rootID, replyUserID int64
	if in.ParentId > 0 {
		parent, err := l.svcCtx.CommentModel.FindOne(l.ctx, uint64(in.ParentId))
		if err != nil {
			if errors.Is(err, model.ErrNotFound) {
				return nil, errorx.New(errorx.CommentParentNotFound)
			}
			return nil, err
		}
		// 父评论必须可见，且归属同一帖子（防跨帖回复）
		if parent.Status != model.CommentStatusNormal || int64(parent.FeedId) != in.FeedId {
			return nil, errorx.New(errorx.CommentParentNotFound)
		}
		if parent.RootId == 0 {
			rootID = int64(parent.Id) // 回复一级评论：root = 该一级评论
		} else {
			rootID = int64(parent.RootId) // 回复子回复：root 取被回复者的 root
		}
		replyUserID = int64(parent.UserId)
	}

	// 单事务写库：INSERT + （子回复时）根评论 reply_count 原子 +1
	now := time.Now()
	commentID := l.svcCtx.IdGen()
	data := &model.Comments{
		Id:          uint64(commentID),
		FeedId:      uint64(in.FeedId),
		UserId:      uint64(in.UserId),
		Content:     in.Content,
		RootId:      uint64(rootID),
		ParentId:    uint64(in.ParentId),
		ReplyUserId: uint64(replyUserID),
		LikeCount:   0,
		ReplyCount:  0,
		Status:      model.CommentStatusNormal,
		CreatedAt:   now,
	}
	if err := l.svcCtx.CommentModel.InsertComment(l.ctx, data); err != nil {
		if errors.Is(err, model.ErrRootUnavailable) {
			// 根评论在并发中被删
			return nil, errorx.New(errorx.CommentParentNotFound)
		}
		return nil, err
	}

	// 先 DB 后缓存：comment_count +1（缓存失败不阻塞）
	applyCommentCountDelta(l.ctx, l.svcCtx, in.FeedId, 1)
	// 新评论 like_count=0，默认不进 comment_hot，待有点赞再进（见 02-publish.md 第 5 节）

	// 发送 CREATE 事件（失败不阻塞）
	sendCommentEvent(l.ctx, l.svcCtx, commentEvent.NewEventCommentCreated(
		commentID, in.FeedId, in.UserId, replyUserID, in.ParentId, rootID,
		int32(utf8.RuneCountInString(in.Content)), now.UnixMilli(), requestid.FromContext(l.ctx)))

	return &comment.CreateCommentResp{Comment: toCommentInfo(data)}, nil
}

// checkFeedExists 调用 Feed 服务校验帖子存在；FeedNotFound 转换为 Comment 段错误码。
func (l *CreateCommentLogic) checkFeedExists(feedID, userID int64) error {
	if l.svcCtx.FeedRpc == nil {
		// 单测/降级场景未注入 FeedRpc：跳过校验（集成环境必配）
		return nil
	}
	_, err := l.svcCtx.FeedRpc.GetFeed(l.ctx, &feedclient.GetFeedReq{FeedId: feedID, UserId: userID})
	if err == nil {
		return nil
	}
	if codeErr, ok := errorx.TryParse(err); ok && codeErr.Code == errorx.FeedNotFound {
		return errorx.New(errorx.CommentFeedNotFound)
	}
	l.Errorf("rpc call failed: service=feed.rpc method=GetFeed feedId=%d err=%v", feedID, err)
	return err
}
