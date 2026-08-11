package consumer

import (
	"context"
	"encoding/json"

	"github.com/sponge-dad/feed/app/content/keys"
	"github.com/sponge-dad/feed/app/content/worker/internal/svc"
	feedevent "github.com/sponge-dad/feed/common/event/feed"

	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/zeromicro/go-zero/core/logx"
)

// FeedDeletedConsumer 消费 feed-deleted，下线内容画像：
//
//	1. UPDATE feed_content_profiles SET analysis_status='DISABLED'
//	2. 删除 ES 索引文档（幂等，不存在也算成功）
//	3. 删除 Redis 画像缓存 content:profile:{feed_id}
//
// 采用「软禁用 + 删索引」而非物理删除（04-content-analysis.md §7）：
// 保留分析结果便于问题追溯，检索与 Agent 一律查不到。
type FeedDeletedConsumer struct {
	svcCtx *svc.ServiceContext
}

// NewFeedDeletedConsumer 创建消费者。
func NewFeedDeletedConsumer(svcCtx *svc.ServiceContext) *FeedDeletedConsumer {
	return &FeedDeletedConsumer{svcCtx: svcCtx}
}

// Handle 处理单条消息。
func (c *FeedDeletedConsumer) Handle(ctx context.Context, msg *primitive.MessageExt) error {
	var ev feedevent.EventFeedDeleted
	if err := json.Unmarshal(msg.Body, &ev); err != nil {
		// 消息体损坏不可恢复，直接 ACK。
		logx.Errorf("feed-deleted unmarshal failed msgId=%s", msg.MsgId)
		return nil
	}

	// 1. 软禁用画像。
	if err := c.svcCtx.ContentProfilesModel.UpdateStatus(ctx, ev.FeedID, "DISABLED", "", 0, 0); err != nil {
		logx.Errorf("feed-deleted disable profile failed feed_id=%d err=%v", ev.FeedID, err)
		return err
	}
	// 2. 删除 ES 索引文档（幂等）。
	if err := c.svcCtx.Es.DeleteProfile(ctx, ev.FeedID); err != nil {
		logx.Errorf("feed-deleted delete es doc failed feed_id=%d err=%v", ev.FeedID, err)
		return err
	}
	// 3. 删除画像缓存（失败触发 MQ 重投，避免残留旧画像被复用）。
	if _, err := c.svcCtx.Redis.Del(keys.ProfileCacheKey(ev.FeedID)); err != nil {
		logx.Errorf("feed-deleted del profile cache failed feed_id=%d err=%v", ev.FeedID, err)
		return err
	}

	logx.Infof("feed %d content profile disabled and removed from index", ev.FeedID)
	return nil
}
