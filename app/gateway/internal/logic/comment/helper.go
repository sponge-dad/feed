// helper.go
//
// 职责：comment 模块 logic 的公共转换函数。
// 评论作者昵称/头像由 Comment 服务批量填充（CommentInfo.UserNickname 等），网关直接映射。
// 说明：评论点赞状态（is_liked）下游暂未提供查询能力，统一返回 false。
package comment

import (
	commentClient "github.com/sponge-dad/feed/app/comment/rpc/commentClient"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
)

// toCommentReply 将 CommentInfo 转换为子回复项。
func toCommentReply(c *commentClient.CommentInfo) types.CommentReply {
	return types.CommentReply{
		ID:      c.CommentId,
		Content: c.Content,
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
		IsLiked:   false,
		CreatedAt: c.CreatedAt,
	}
}

// toCommentEntry 将 CommentInfo（一级评论）转换为列表项。
func toCommentEntry(c *commentClient.CommentInfo, replyTotal int64, previews []*commentClient.CommentInfo) types.CommentEntry {
	entry := types.CommentEntry{
		ID:      c.CommentId,
		Content: c.Content,
		Author: types.CommentAuthor{
			ID:       c.UserId,
			Nickname: c.UserNickname,
			Avatar:   c.UserAvatar,
		},
		LikeCount:  c.LikeCount,
		IsLiked:    false,
		ReplyCount: replyTotal,
		CreatedAt:  c.CreatedAt,
		SubReplies: make([]types.CommentReply, 0, len(previews)),
	}
	for _, p := range previews {
		if p != nil {
			entry.SubReplies = append(entry.SubReplies, toCommentReply(p))
		}
	}
	return entry
}
