// delete_comment_test.go
//
// 职责：DeleteComment 单元测试。覆盖软删、权限拒绝、幂等（不二次减计数）、
// 删子回复联动根 reply_count-1、删根评论整楼减量（1+可见子回复）与 hot 移除、
// 计数缓存非负保护（见 docs/design/comment/08-test-strategy.md）。
package logic

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sponge-dad/feed/app/comment/model"
	"github.com/sponge-dad/feed/app/comment/rpc/comment"
	"github.com/sponge-dad/feed/app/comment/rpc/internal/keys"
	"github.com/sponge-dad/feed/common/errorx"
)

// seedFloor 预置一楼评论：根(id=1) + 两条子回复(id=2,3)，feed=100，作者 user=1/2/3。
func seedFloor(t *testing.T, m *stubCommentsModel) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	require.NoError(t, m.InsertComment(ctx, mkComment(1, 100, 1, 0, 0, now)))
	require.NoError(t, m.InsertComment(ctx, mkComment(2, 100, 2, 1, 1, now.Add(time.Second))))
	require.NoError(t, m.InsertComment(ctx, mkComment(3, 100, 3, 1, 2, now.Add(2*time.Second))))
}

// TestDeleteComment_Reply 删子回复：根 reply_count-1，comment_count-1。
func TestDeleteComment_Reply(t *testing.T) {
	m := newStubCommentsModel()
	seedFloor(t, m)
	svcCtx := newTestSvc(t, m, &stubUserRpc{}, &stubFeedRpc{})
	require.NoError(t, svcCtx.Redis.Set(keys.CommentCount(100), "3"))

	l := NewDeleteCommentLogic(context.Background(), svcCtx)
	resp, err := l.DeleteComment(&comment.DeleteCommentReq{CommentId: 2, UserId: 2})
	require.NoError(t, err)
	require.True(t, resp.Success)

	// 该条不可见，兄弟可见
	deleted, err := m.FindOne(context.Background(), 2)
	require.NoError(t, err)
	require.Equal(t, model.CommentStatusDeleted, deleted.Status)
	sibling, err := m.FindOne(context.Background(), 3)
	require.NoError(t, err)
	require.Equal(t, model.CommentStatusNormal, sibling.Status)

	// 根 reply_count 2 -> 1
	root, err := m.FindOne(context.Background(), 1)
	require.NoError(t, err)
	require.EqualValues(t, 1, root.ReplyCount)

	// comment_count 3 -> 2
	val, err := svcCtx.Redis.Get(keys.CommentCount(100))
	require.NoError(t, err)
	require.Equal(t, "2", val)
}

// TestDeleteComment_Root 删根评论：减量 = 1 + 可见子回复数，并从 comment_hot 移除。
func TestDeleteComment_Root(t *testing.T) {
	m := newStubCommentsModel()
	seedFloor(t, m)
	svcCtx := newTestSvc(t, m, &stubUserRpc{}, &stubFeedRpc{})
	require.NoError(t, svcCtx.Redis.Set(keys.CommentCount(100), "3"))
	_, err := svcCtx.Redis.Zadd(keys.CommentHot(100), 10, "1")
	require.NoError(t, err)

	l := NewDeleteCommentLogic(context.Background(), svcCtx)
	resp, err := l.DeleteComment(&comment.DeleteCommentReq{CommentId: 1, UserId: 1})
	require.NoError(t, err)
	require.True(t, resp.Success)

	// comment_count 3 -> 0（1 根 + 2 可见子回复）
	val, err := svcCtx.Redis.Get(keys.CommentCount(100))
	require.NoError(t, err)
	require.Equal(t, "0", val)

	// hot ZSet 中已移除
	score, err := svcCtx.Redis.Zscore(keys.CommentHot(100), "1")
	require.Error(t, err) // 成员不存在
	_ = score
}

// TestDeleteComment_IdempotentAndPermission 重复删除幂等不二次减计数；非作者被拒；不存在报错。
func TestDeleteComment_IdempotentAndPermission(t *testing.T) {
	m := newStubCommentsModel()
	seedFloor(t, m)
	svcCtx := newTestSvc(t, m, &stubUserRpc{}, &stubFeedRpc{})
	require.NoError(t, svcCtx.Redis.Set(keys.CommentCount(100), "3"))
	l := NewDeleteCommentLogic(context.Background(), svcCtx)

	// 非作者删除
	_, err := l.DeleteComment(&comment.DeleteCommentReq{CommentId: 3, UserId: 999})
	requireBizCode(t, err, errorx.CommentNoPermission)

	// 评论不存在
	_, err = l.DeleteComment(&comment.DeleteCommentReq{CommentId: 777, UserId: 1})
	requireBizCode(t, err, errorx.CommentNotFound)

	// 首次删除成功
	resp, err := l.DeleteComment(&comment.DeleteCommentReq{CommentId: 3, UserId: 3})
	require.NoError(t, err)
	require.True(t, resp.Success)
	val, err := svcCtx.Redis.Get(keys.CommentCount(100))
	require.NoError(t, err)
	require.Equal(t, "2", val)

	// 重复删除：幂等成功，计数不再减少
	resp, err = l.DeleteComment(&comment.DeleteCommentReq{CommentId: 3, UserId: 3})
	require.NoError(t, err)
	require.True(t, resp.Success)
	val, err = svcCtx.Redis.Get(keys.CommentCount(100))
	require.NoError(t, err)
	require.Equal(t, "2", val)
}

// TestDeleteComment_NegativeProtection 缓存值偏小时删整楼，DECR 后触发非负保护归 0。
func TestDeleteComment_NegativeProtection(t *testing.T) {
	m := newStubCommentsModel()
	seedFloor(t, m)
	svcCtx := newTestSvc(t, m, &stubUserRpc{}, &stubFeedRpc{})
	// 缓存值被低估为 1，删根评论实际减量 3
	require.NoError(t, svcCtx.Redis.Set(keys.CommentCount(100), "1"))

	l := NewDeleteCommentLogic(context.Background(), svcCtx)
	_, err := l.DeleteComment(&comment.DeleteCommentReq{CommentId: 1, UserId: 1})
	require.NoError(t, err)

	val, err := svcCtx.Redis.Get(keys.CommentCount(100))
	require.NoError(t, err)
	require.Equal(t, "0", val)
	n, err := strconv.ParseInt(val, 10, 64)
	require.NoError(t, err)
	require.GreaterOrEqual(t, n, int64(0))
}
