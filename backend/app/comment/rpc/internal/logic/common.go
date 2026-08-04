// common.go
//
// 职责：logic 层共享的常量与辅助函数，包括：
//   - 请求参数上限（内容长度 / 分页 / 预览 / 批量）
//   - model.Comments -> pb CommentInfo 转换
//   - 用户昵称头像批量填充（一次 BatchGetUsers，禁止逐条 N+1，失败降级不阻塞）
//   - comment_count 的 Cache-Aside 读取与写后增减（非负保护，缓存失败不阻塞主流程）
//   - 子回复游标编解码（created_at + id 组合游标）
//   - comment.event 事件发送（Producer 为空时安全跳过，便于单测）
package logic

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sponge-dad/feed/app/comment/model"
	"github.com/sponge-dad/feed/app/comment/rpc/comment"
	"github.com/sponge-dad/feed/app/comment/rpc/internal/keys"
	"github.com/sponge-dad/feed/app/comment/rpc/internal/svc"
	"github.com/sponge-dad/feed/app/user/rpc/userClient"
	commentEvent "github.com/sponge-dad/feed/common/event/comment"

	"github.com/zeromicro/go-zero/core/logx"
)

// 业务上限常量（见 docs/design/comment/02-publish.md / 04-list.md / 05-stats.md）。
const (
	maxContentRunes  = 1000 // 评论内容最大字数
	defaultPageSize  = 10   // 默认每页条数
	maxPageSize      = 100  // 最大每页条数，防恶意大页
	defaultPreview   = 3    // 默认每楼子回复预览条数
	maxPreview       = 10   // 最大预览条数
	defaultHotLimit  = 3    // 热门评论默认 Top-K
	maxHotLimit      = 10   // 热门评论最大 Top-K
	maxBatchFeedIds  = 100  // 批量计数单次上限
	commentHotZsetTL = int(5 * time.Minute / time.Second)
)

// toCommentInfo 将 model.Comments 转换为 pb CommentInfo（不含用户昵称，昵称统一批量填充）。
func toCommentInfo(c *model.Comments) *comment.CommentInfo {
	return &comment.CommentInfo{
		CommentId:   int64(c.Id),
		FeedId:      int64(c.FeedId),
		UserId:      int64(c.UserId),
		Content:     c.Content,
		RootId:      int64(c.RootId),
		ParentId:    int64(c.ParentId),
		ReplyUserId: int64(c.ReplyUserId),
		LikeCount:   int64(c.LikeCount),
		ReplyCount:  int64(c.ReplyCount),
		Status:      int32(c.Status),
		CreatedAt:   c.CreatedAt.UnixMilli(),
	}
}

// fillUserInfos 收集 infos 中所有 user_id / reply_user_id，一次批量 RPC 获取昵称头像后内存拼装。
// User 服务不可用时降级：仅记日志，评论列表照常返回（昵称头像为空）。
func fillUserInfos(ctx context.Context, svcCtx *svc.ServiceContext, infos []*comment.CommentInfo) {
	if len(infos) == 0 || svcCtx.UserRpc == nil {
		return
	}

	idSet := make(map[int64]struct{}, len(infos)*2)
	for _, info := range infos {
		if info.UserId > 0 {
			idSet[info.UserId] = struct{}{}
		}
		if info.ReplyUserId > 0 {
			idSet[info.ReplyUserId] = struct{}{}
		}
	}
	if len(idSet) == 0 {
		return
	}

	userIds := make([]int64, 0, len(idSet))
	for id := range idSet {
		userIds = append(userIds, id)
	}

	resp, err := svcCtx.UserRpc.BatchGetUsers(ctx, &userClient.BatchGetUsersReq{UserIds: userIds})
	if err != nil {
		logx.WithContext(ctx).Errorf("rpc call failed: service=user.rpc method=BatchGetUsers err=%v", err)
		return
	}

	briefs := make(map[int64]*userClient.UserBrief, len(resp.Users))
	for _, u := range resp.Users {
		briefs[u.Id] = u
	}
	for _, info := range infos {
		if u, ok := briefs[info.UserId]; ok {
			info.UserNickname = u.Nickname
			info.UserAvatar = u.Avatar
		}
		if u, ok := briefs[info.ReplyUserId]; ok {
			info.ReplyUserNickname = u.Nickname
		}
	}
}

// getCommentCount 按 Cache-Aside 读取帖子评论总数：
// 优先 Redis comment_count:{feed_id}；未命中或 Redis 异常时回源 MySQL COUNT 并尽力回写（TTL 1h）。
func getCommentCount(ctx context.Context, svcCtx *svc.ServiceContext, feedID int64) (int64, error) {
	key := keys.CommentCount(feedID)
	val, err := svcCtx.Redis.GetCtx(ctx, key)
	if err != nil {
		// Redis 不可用：性能降级直接读 MySQL，不出错（见 06-cache.md 降级）
		logx.WithContext(ctx).Errorf("redis get comment_count failed feedId=%d err=%v", feedID, err)
	} else if val != "" {
		if count, parseErr := strconv.ParseInt(val, 10, 64); parseErr == nil {
			return count, nil
		}
	}

	count, err := svcCtx.CommentModel.CountByFeedId(ctx, uint64(feedID))
	if err != nil {
		return 0, err
	}
	// 回写缓存失败不阻塞读
	if setErr := svcCtx.Redis.SetexCtx(ctx, key, strconv.FormatInt(count, 10), int(keys.CommentCountTTL/time.Second)); setErr != nil {
		logx.WithContext(ctx).Errorf("redis rebuild comment_count failed feedId=%d err=%v", feedID, setErr)
	}
	return count, nil
}

// applyCommentCountDelta 写后增减 comment_count 缓存。
// 仅在 key 已存在时增减（避免 INCR 把缺失 key 变成错误的 1），key 不存在交由读路径重建；
// DECR 后做非负保护；任何缓存失败只记日志，不阻塞写主流程。
func applyCommentCountDelta(ctx context.Context, svcCtx *svc.ServiceContext, feedID, delta int64) {
	logger := logx.WithContext(ctx)
	key := keys.CommentCount(feedID)
	exists, err := svcCtx.Redis.ExistsCtx(ctx, key)
	if err != nil {
		logger.Errorf("redis exists comment_count failed feedId=%d err=%v", feedID, err)
		return
	}
	if !exists {
		return
	}

	val, err := svcCtx.Redis.IncrbyCtx(ctx, key, delta)
	if err != nil {
		logger.Errorf("redis update comment_count failed feedId=%d delta=%d err=%v", feedID, delta, err)
		return
	}
	// 非负保护（见 06-cache.md 第 5 节）
	if val < 0 {
		if setErr := svcCtx.Redis.SetexCtx(ctx, key, "0", int(keys.CommentCountTTL/time.Second)); setErr != nil {
			logger.Errorf("redis reset negative comment_count failed feedId=%d err=%v", feedID, setErr)
		}
	}
}

// encodeReplyCursor 将 (created_at, id) 组合游标编码为 base64 字符串。
func encodeReplyCursor(createdAt time.Time, id uint64) string {
	raw := fmt.Sprintf("%d:%d", createdAt.Unix(), id)
	return base64.URLEncoding.EncodeToString([]byte(raw))
}

// decodeReplyCursor 解析游标；空串表示第一页（零值游标）。格式非法返回 ok=false。
func decodeReplyCursor(cursor string) (createdAt time.Time, id uint64, ok bool) {
	if cursor == "" {
		return time.Unix(0, 0), 0, true
	}
	raw, err := base64.URLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, 0, false
	}
	parts := strings.SplitN(string(raw), ":", 2)
	if len(parts) != 2 {
		return time.Time{}, 0, false
	}
	sec, err1 := strconv.ParseInt(parts[0], 10, 64)
	cid, err2 := strconv.ParseUint(parts[1], 10, 64)
	if err1 != nil || err2 != nil {
		return time.Time{}, 0, false
	}
	return time.Unix(sec, 0), cid, true
}

// sendCommentEvent 序列化并发送 comment.event；Producer 未配置（单测）或发送失败均不阻塞主流程。
func sendCommentEvent(ctx context.Context, svcCtx *svc.ServiceContext, ev *commentEvent.Event) {
	if svcCtx.Producer == nil {
		return
	}
	body, err := json.Marshal(ev)
	if err != nil {
		logx.WithContext(ctx).Errorf("marshal comment.event failed commentId=%d err=%v", ev.CommentID, err)
		return
	}
	if err := svcCtx.Producer.SendSync(commentEvent.TopicCommentEvent, body); err != nil {
		// MQ 失败不阻塞主流程：DB 已落库，通知可延迟（见 02-publish.md 第 8 节）
		logx.WithContext(ctx).Errorf("send comment.event to MQ failed commentId=%d action=%s err=%v",
			ev.CommentID, ev.ActionType, err)
	}
}
