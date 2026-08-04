// comment_flow_test.go
//
// 职责：Comment 服务端到端流程与并发一致性集成测试（真实 MySQL）。
// 覆盖：发表/楼中楼推导、窗口函数预览、游标翻页、删除联动与幂等、
// comment_count 缓存重建与 MySQL COUNT 对账、并发发表计数一致。
package tests

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sponge-dad/feed/app/comment/rpc/comment"
)

// TestCommentLifecycle 端到端：发表 -> 列表(预览) -> 全部回复翻页 -> 计数 -> 删除 -> 幂等。
func TestCommentLifecycle(t *testing.T) {
	requireEnv(t)
	ctx := context.Background()
	feedID := nextID() // 运行期唯一，替代固定 ID + TRUNCATE

	// 1. 发一级评论
	root, err := testClient.CreateComment(ctx, &comment.CreateCommentReq{
		UserId: 1, FeedId: feedID, Content: "一级评论"})
	require.NoError(t, err)
	require.EqualValues(t, 0, root.Comment.RootId)

	// 2. 楼中楼：4 条子回复（两条回复根，两条回复子回复）
	reply1, err := testClient.CreateComment(ctx, &comment.CreateCommentReq{
		UserId: 2, FeedId: feedID, Content: "回复根", ParentId: root.Comment.CommentId})
	require.NoError(t, err)
	require.Equal(t, root.Comment.CommentId, reply1.Comment.RootId)

	reply2, err := testClient.CreateComment(ctx, &comment.CreateCommentReq{
		UserId: 3, FeedId: feedID, Content: "回复子回复", ParentId: reply1.Comment.CommentId})
	require.NoError(t, err)
	require.Equal(t, root.Comment.CommentId, reply2.Comment.RootId)
	require.Equal(t, reply1.Comment.CommentId, reply2.Comment.ParentId)
	require.EqualValues(t, 2, reply2.Comment.ReplyUserId)

	for i := 0; i < 2; i++ {
		_, err = testClient.CreateComment(ctx, &comment.CreateCommentReq{
			UserId: 4, FeedId: feedID, Content: "更多回复", ParentId: root.Comment.CommentId})
		require.NoError(t, err)
	}

	// 3. 列表：预览仅前 2 条（时间正序），reply_total=4，昵称已填充
	list, err := testClient.ListComments(ctx, &comment.ListCommentsReq{
		FeedId: feedID, Page: 1, PageSize: 10, PreviewCount: 2})
	require.NoError(t, err)
	require.Len(t, list.Comments, 1)
	floor := list.Comments[0]
	require.EqualValues(t, 4, floor.ReplyTotal)
	require.Len(t, floor.PreviewReplies, 2)
	require.Equal(t, reply1.Comment.CommentId, floor.PreviewReplies[0].CommentId)
	assert.Equal(t, "nick-1", floor.Comment.UserNickname)
	assert.EqualValues(t, 5, list.Page.Total)

	// 4. 查看全部回复：page_size=2 游标翻页，无重复无遗漏
	seen := make(map[int64]bool)
	cursor := ""
	for {
		resp, err := testClient.ListReplies(ctx, &comment.ListRepliesReq{
			RootId: root.Comment.CommentId, Cursor: cursor, PageSize: 2})
		require.NoError(t, err)
		for _, r := range resp.Replies {
			require.False(t, seen[r.CommentId])
			seen[r.CommentId] = true
		}
		if !resp.Page.HasMore {
			break
		}
		cursor = resp.Page.Cursor
	}
	require.Len(t, seen, 4)

	// 5. 计数与 MySQL COUNT 对账
	count, err := testClient.GetCommentCount(ctx, &comment.GetCommentCountReq{FeedId: feedID})
	require.NoError(t, err)
	require.Equal(t, dbCommentCount(t, feedID), count.Count)
	require.EqualValues(t, 5, count.Count)

	// 6. 删子回复：根 reply_count-1，计数-1；非作者删除被拒
	_, err = testClient.DeleteComment(ctx, &comment.DeleteCommentReq{
		CommentId: reply2.Comment.CommentId, UserId: 999})
	require.Error(t, err) // CommentNoPermission

	del, err := testClient.DeleteComment(ctx, &comment.DeleteCommentReq{
		CommentId: reply2.Comment.CommentId, UserId: 3})
	require.NoError(t, err)
	require.True(t, del.Success)

	count, err = testClient.GetCommentCount(ctx, &comment.GetCommentCountReq{FeedId: feedID})
	require.NoError(t, err)
	require.EqualValues(t, 4, count.Count)
	require.Equal(t, dbCommentCount(t, feedID), count.Count)

	// 7. 删根评论：整楼折叠，计数减 1+可见子回复；重复删除幂等
	del, err = testClient.DeleteComment(ctx, &comment.DeleteCommentReq{
		CommentId: root.Comment.CommentId, UserId: 1})
	require.NoError(t, err)
	require.True(t, del.Success)

	count, err = testClient.GetCommentCount(ctx, &comment.GetCommentCountReq{FeedId: feedID})
	require.NoError(t, err)
	require.EqualValues(t, 0, count.Count)
	require.Equal(t, dbCommentCount(t, feedID), count.Count)

	del, err = testClient.DeleteComment(ctx, &comment.DeleteCommentReq{
		CommentId: root.Comment.CommentId, UserId: 1})
	require.NoError(t, err)
	require.True(t, del.Success) // 幂等
	count, err = testClient.GetCommentCount(ctx, &comment.GetCommentCountReq{FeedId: feedID})
	require.NoError(t, err)
	require.EqualValues(t, 0, count.Count) // 不二次减

	// 8. 列表中已删楼不可见
	list, err = testClient.ListComments(ctx, &comment.ListCommentsReq{FeedId: feedID})
	require.NoError(t, err)
	require.Empty(t, list.Comments)
}

// TestConcurrentCreate_CountConsistency 并发发表 50 条（同楼回复），
// 最终 comment_count 缓存与 MySQL COUNT 一致，根 reply_count 正确。
func TestConcurrentCreate_CountConsistency(t *testing.T) {
	requireEnv(t)
	ctx := context.Background()
	feedID := nextID() // 运行期唯一，替代固定 ID + TRUNCATE
	const workers = 50

	root, err := testClient.CreateComment(ctx, &comment.CreateCommentReq{
		UserId: 1, FeedId: feedID, Content: "并发根评论"})
	require.NoError(t, err)

	// 预热计数缓存，使后续走 INCR 增量路径
	_, err = testClient.GetCommentCount(ctx, &comment.GetCommentCountReq{FeedId: feedID})
	require.NoError(t, err)

	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, err := testClient.CreateComment(ctx, &comment.CreateCommentReq{
				UserId: int64(100 + n), FeedId: feedID, Content: "并发回复",
				ParentId: root.Comment.CommentId})
			if err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent create failed: %v", err)
	}

	// 计数收敛一致：缓存值 == MySQL COUNT == 1 + workers
	count, err := testClient.GetCommentCount(ctx, &comment.GetCommentCountReq{FeedId: feedID})
	require.NoError(t, err)
	require.EqualValues(t, 1+workers, count.Count)
	require.Equal(t, dbCommentCount(t, feedID), count.Count)

	// 根 reply_count 事务联动正确
	var replyCount int64
	require.NoError(t, testDB.QueryRow(
		"SELECT reply_count FROM comments WHERE id = ?", root.Comment.CommentId).Scan(&replyCount))
	require.EqualValues(t, workers, replyCount)

	// 批量计数接口同样一致
	batch, err := testClient.BatchGetCommentCount(ctx, &comment.BatchGetCommentCountReq{
		FeedIds: []int64{feedID, 99999}})
	require.NoError(t, err)
	require.EqualValues(t, 1+workers, batch.Counts[feedID])
	require.EqualValues(t, 0, batch.Counts[99999])
}
