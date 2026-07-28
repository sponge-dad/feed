// list_comment_test.go
//
// 职责：ListComments / ListReplies 单元测试。覆盖一级评论分页与预览前 N 条（时间正序）、
// 已删不可见、空帖 total=0、用户信息批量填充无 N+1、User 不可用降级、
// 楼内 cursor 翻页无重复无遗漏（见 docs/design/comment/08-test-strategy.md）。
package logic

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sponge-dad/feed/app/comment/model"
	"github.com/sponge-dad/feed/app/comment/rpc/comment"
	"github.com/sponge-dad/feed/app/user/rpc/user"
	"github.com/sponge-dad/feed/common/errorx"
)

// seedFeed 预置 feed=100：3 楼（id=1,2,3 时间递增），第 1 楼带 5 条子回复（id=11..15）。
func seedFeed(t *testing.T, m *stubCommentsModel) {
	t.Helper()
	ctx := context.Background()
	base := time.Now().Truncate(time.Second).Add(-time.Hour)
	require.NoError(t, m.InsertComment(ctx, mkComment(1, 100, 1, 0, 0, base)))
	require.NoError(t, m.InsertComment(ctx, mkComment(2, 100, 2, 0, 0, base.Add(time.Minute))))
	require.NoError(t, m.InsertComment(ctx, mkComment(3, 100, 3, 0, 0, base.Add(2*time.Minute))))
	for i := uint64(0); i < 5; i++ {
		require.NoError(t, m.InsertComment(ctx,
			mkComment(11+i, 100, 4+i, 1, 1, base.Add(time.Duration(i+1)*time.Second))))
	}
}

// TestListComments_PageAndPreview 一级评论时间倒序分页，每楼预览前 N 条时间正序。
func TestListComments_PageAndPreview(t *testing.T) {
	m := newStubCommentsModel()
	seedFeed(t, m)
	userRpc := &stubUserRpc{users: map[int64]*user.UserBrief{
		1: {Id: 1, Nickname: "u1", Avatar: "a1"},
		4: {Id: 4, Nickname: "u4", Avatar: "a4"},
	}}
	svcCtx := newTestSvc(t, m, userRpc, &stubFeedRpc{})
	l := NewListCommentsLogic(context.Background(), svcCtx)

	resp, err := l.ListComments(&comment.ListCommentsReq{FeedId: 100, Page: 1, PageSize: 2, PreviewCount: 3})
	require.NoError(t, err)
	require.Len(t, resp.Comments, 2)
	// 时间倒序：id=3 最新在前
	require.EqualValues(t, 3, resp.Comments[0].Comment.CommentId)
	require.EqualValues(t, 2, resp.Comments[1].Comment.CommentId)
	require.True(t, resp.Page.HasMore)
	require.EqualValues(t, 8, resp.Page.Total) // 3 楼 + 5 子回复

	// 第 2 页拿到第 1 楼，预览仅前 3 条且时间正序，reply_total=5
	resp, err = l.ListComments(&comment.ListCommentsReq{FeedId: 100, Page: 2, PageSize: 2, PreviewCount: 3})
	require.NoError(t, err)
	require.Len(t, resp.Comments, 1)
	floor1 := resp.Comments[0]
	require.EqualValues(t, 1, floor1.Comment.CommentId)
	require.Len(t, floor1.PreviewReplies, 3)
	require.EqualValues(t, 11, floor1.PreviewReplies[0].CommentId)
	require.EqualValues(t, 12, floor1.PreviewReplies[1].CommentId)
	require.EqualValues(t, 13, floor1.PreviewReplies[2].CommentId)
	require.EqualValues(t, 5, floor1.ReplyTotal)

	// 用户信息批量填充：整个请求只调 1 次 BatchGetUsers（无 N+1）
	require.Equal(t, 2, userRpc.calls) // 两次 ListComments 各 1 次
	require.Equal(t, "u1", floor1.Comment.UserNickname)
	require.Equal(t, "u4", floor1.PreviewReplies[0].UserNickname)
}

// TestListComments_DeletedInvisibleAndEmpty 已删评论不可见；空帖返回空列表 total=0。
func TestListComments_DeletedInvisibleAndEmpty(t *testing.T) {
	m := newStubCommentsModel()
	seedFeed(t, m)
	// 软删第 2 楼
	c2, err := m.FindOne(context.Background(), 2)
	require.NoError(t, err)
	_, _, err = m.SoftDelete(context.Background(), c2)
	require.NoError(t, err)

	svcCtx := newTestSvc(t, m, &stubUserRpc{}, &stubFeedRpc{})
	l := NewListCommentsLogic(context.Background(), svcCtx)

	resp, err := l.ListComments(&comment.ListCommentsReq{FeedId: 100})
	require.NoError(t, err)
	require.Len(t, resp.Comments, 2)
	for _, c := range resp.Comments {
		require.NotEqualValues(t, 2, c.Comment.CommentId)
		require.EqualValues(t, model.CommentStatusNormal, c.Comment.Status)
	}

	// 空帖
	empty, err := l.ListComments(&comment.ListCommentsReq{FeedId: 999})
	require.NoError(t, err)
	require.Empty(t, empty.Comments)
	require.EqualValues(t, 0, empty.Page.Total)
	require.False(t, empty.Page.HasMore)
}

// TestListComments_UserRpcDegrade User 服务不可用时列表照常返回，昵称为空。
func TestListComments_UserRpcDegrade(t *testing.T) {
	m := newStubCommentsModel()
	seedFeed(t, m)
	userRpc := &stubUserRpc{returnErr: errors.New("user rpc unavailable")}
	svcCtx := newTestSvc(t, m, userRpc, &stubFeedRpc{})
	l := NewListCommentsLogic(context.Background(), svcCtx)

	resp, err := l.ListComments(&comment.ListCommentsReq{FeedId: 100})
	require.NoError(t, err)
	require.Len(t, resp.Comments, 3)
	require.Empty(t, resp.Comments[0].Comment.UserNickname)
}

// TestListReplies_CursorPaging cursor 翻页无重复无遗漏，最后一页 has_more=false。
func TestListReplies_CursorPaging(t *testing.T) {
	m := newStubCommentsModel()
	seedFeed(t, m)
	svcCtx := newTestSvc(t, m, &stubUserRpc{}, &stubFeedRpc{})
	l := NewListRepliesLogic(context.Background(), svcCtx)

	seen := make(map[int64]bool)
	cursor := ""
	pages := 0
	for {
		resp, err := l.ListReplies(&comment.ListRepliesReq{RootId: 1, Cursor: cursor, PageSize: 2})
		require.NoError(t, err)
		for _, r := range resp.Replies {
			require.False(t, seen[r.CommentId], "重复评论 %d", r.CommentId)
			seen[r.CommentId] = true
		}
		pages++
		if !resp.Page.HasMore {
			require.Empty(t, resp.Page.Cursor)
			break
		}
		require.NotEmpty(t, resp.Page.Cursor)
		cursor = resp.Page.Cursor
	}
	require.Len(t, seen, 5) // 5 条子回复全部遍历，无遗漏
	require.Equal(t, 3, pages)
}

// TestListReplies_RootInvalid 根评论不存在/已删/传子回复 ID 均返回 CommentNotFound；坏游标返回参数错误。
func TestListReplies_RootInvalid(t *testing.T) {
	m := newStubCommentsModel()
	seedFeed(t, m)
	svcCtx := newTestSvc(t, m, &stubUserRpc{}, &stubFeedRpc{})
	l := NewListRepliesLogic(context.Background(), svcCtx)

	_, err := l.ListReplies(&comment.ListRepliesReq{RootId: 777})
	requireBizCode(t, err, errorx.CommentNotFound)

	// root_id 传了子回复 ID
	_, err = l.ListReplies(&comment.ListRepliesReq{RootId: 11})
	requireBizCode(t, err, errorx.CommentNotFound)

	// 非法游标
	_, err = l.ListReplies(&comment.ListRepliesReq{RootId: 1, Cursor: "!!!not-base64!!!"})
	requireBizCode(t, err, errorx.ParamError)
}
