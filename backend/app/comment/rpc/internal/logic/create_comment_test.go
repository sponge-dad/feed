// create_comment_test.go
//
// 职责：CreateComment 单元测试。覆盖楼中楼字段推导（root_id/parent_id/reply_user_id）、
// 参数与业务校验（空内容/超长/帖子不存在/父评论已删/跨帖回复）、
// 根评论 reply_count 联动与 comment_count 缓存增量（见 docs/design/comment/08-test-strategy.md）。
package logic

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sponge-dad/feed/app/comment/model"
	"github.com/sponge-dad/feed/app/comment/rpc/comment"
	"github.com/sponge-dad/feed/app/comment/rpc/internal/keys"
	"github.com/sponge-dad/feed/common/errorx"
)

// TestCreateComment_LouZhongLou 验证一级评论、回复一级、回复子回复的字段推导。
func TestCreateComment_LouZhongLou(t *testing.T) {
	m := newStubCommentsModel()
	svcCtx := newTestSvc(t, m, &stubUserRpc{}, &stubFeedRpc{existing: map[int64]bool{100: true}})
	l := NewCreateCommentLogic(context.Background(), svcCtx)

	// 一级评论
	root, err := l.CreateComment(&comment.CreateCommentReq{UserId: 1, FeedId: 100, Content: "一级评论"})
	require.NoError(t, err)
	require.EqualValues(t, 0, root.Comment.RootId)
	require.EqualValues(t, 0, root.Comment.ParentId)
	require.EqualValues(t, 0, root.Comment.ReplyUserId)

	// 回复一级评论：root = 一级评论，reply_user = 一级评论作者
	reply1, err := l.CreateComment(&comment.CreateCommentReq{
		UserId: 2, FeedId: 100, Content: "回复一级", ParentId: root.Comment.CommentId})
	require.NoError(t, err)
	require.Equal(t, root.Comment.CommentId, reply1.Comment.RootId)
	require.Equal(t, root.Comment.CommentId, reply1.Comment.ParentId)
	require.EqualValues(t, 1, reply1.Comment.ReplyUserId)

	// 回复子回复：root 不变，parent 指向直接父，reply_user = 直接父作者
	reply2, err := l.CreateComment(&comment.CreateCommentReq{
		UserId: 3, FeedId: 100, Content: "回复子回复", ParentId: reply1.Comment.CommentId})
	require.NoError(t, err)
	require.Equal(t, root.Comment.CommentId, reply2.Comment.RootId)
	require.Equal(t, reply1.Comment.CommentId, reply2.Comment.ParentId)
	require.EqualValues(t, 2, reply2.Comment.ReplyUserId)

	// 根评论 reply_count 已联动 +2
	stored, err := m.FindOne(context.Background(), uint64(root.Comment.CommentId))
	require.NoError(t, err)
	require.EqualValues(t, 2, stored.ReplyCount)
}

// TestCreateComment_Validation 验证参数与业务校验分支。
func TestCreateComment_Validation(t *testing.T) {
	m := newStubCommentsModel()
	feedRpc := &stubFeedRpc{existing: map[int64]bool{100: true}}
	svcCtx := newTestSvc(t, m, &stubUserRpc{}, feedRpc)
	l := NewCreateCommentLogic(context.Background(), svcCtx)

	// 空内容
	_, err := l.CreateComment(&comment.CreateCommentReq{UserId: 1, FeedId: 100, Content: ""})
	requireBizCode(t, err, errorx.CommentEmpty)

	// 超长（>1000 字）
	_, err = l.CreateComment(&comment.CreateCommentReq{
		UserId: 1, FeedId: 100, Content: strings.Repeat("字", 1001)})
	requireBizCode(t, err, errorx.CommentTooLong)

	// 恰好 1000 字合法
	_, err = l.CreateComment(&comment.CreateCommentReq{
		UserId: 1, FeedId: 100, Content: strings.Repeat("字", 1000)})
	require.NoError(t, err)

	// 帖子不存在
	_, err = l.CreateComment(&comment.CreateCommentReq{UserId: 1, FeedId: 999, Content: "hi"})
	requireBizCode(t, err, errorx.CommentFeedNotFound)

	// 父评论不存在
	_, err = l.CreateComment(&comment.CreateCommentReq{
		UserId: 1, FeedId: 100, Content: "hi", ParentId: 888888})
	requireBizCode(t, err, errorx.CommentParentNotFound)

	// 非法参数
	_, err = l.CreateComment(&comment.CreateCommentReq{UserId: 0, FeedId: 100, Content: "hi"})
	requireBizCode(t, err, errorx.ParamError)
}

// TestCreateComment_ParentDeletedOrCrossFeed 验证父评论已删与跨帖回复被拒。
func TestCreateComment_ParentDeletedOrCrossFeed(t *testing.T) {
	m := newStubCommentsModel()
	now := time.Now()

	// feed=100 下有一条已删父评论；feed=200 下有一条正常评论
	deleted := mkComment(11, 100, 1, 0, 0, now)
	deleted.Status = model.CommentStatusDeleted
	require.NoError(t, m.InsertComment(context.Background(), deleted))
	crossFeed := mkComment(22, 200, 1, 0, 0, now)
	require.NoError(t, m.InsertComment(context.Background(), crossFeed))

	svcCtx := newTestSvc(t, m, &stubUserRpc{}, &stubFeedRpc{existing: map[int64]bool{100: true, 200: true}})
	l := NewCreateCommentLogic(context.Background(), svcCtx)

	// 回复已删评论
	_, err := l.CreateComment(&comment.CreateCommentReq{
		UserId: 2, FeedId: 100, Content: "hi", ParentId: 11})
	requireBizCode(t, err, errorx.CommentParentNotFound)

	// 跨帖回复：parent 属于 feed=200，但请求 feed=100
	_, err = l.CreateComment(&comment.CreateCommentReq{
		UserId: 2, FeedId: 100, Content: "hi", ParentId: 22})
	requireBizCode(t, err, errorx.CommentParentNotFound)
}

// TestCreateComment_CountCacheDelta 验证写后 comment_count 缓存增量与「key 缺失不误置 1」。
func TestCreateComment_CountCacheDelta(t *testing.T) {
	m := newStubCommentsModel()
	svcCtx := newTestSvc(t, m, &stubUserRpc{}, &stubFeedRpc{existing: map[int64]bool{100: true}})
	l := NewCreateCommentLogic(context.Background(), svcCtx)

	// key 不存在时发表：不 INCR（避免错误地从 1 开始），留给读路径重建
	_, err := l.CreateComment(&comment.CreateCommentReq{UserId: 1, FeedId: 100, Content: "a"})
	require.NoError(t, err)
	val, err := svcCtx.Redis.Get(keys.CommentCount(100))
	require.NoError(t, err)
	require.Empty(t, val)

	// 预置缓存后发表：INCR +1
	require.NoError(t, svcCtx.Redis.Set(keys.CommentCount(100), "1"))
	_, err = l.CreateComment(&comment.CreateCommentReq{UserId: 1, FeedId: 100, Content: "b"})
	require.NoError(t, err)
	val, err = svcCtx.Redis.Get(keys.CommentCount(100))
	require.NoError(t, err)
	require.Equal(t, "2", val)
}
