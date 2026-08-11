// Package consumer Content Worker 的 RocketMQ 消费者。
package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sponge-dad/feed/app/content/keys"
	"github.com/sponge-dad/feed/app/content/model"
	"github.com/sponge-dad/feed/app/content/worker/internal/pipeline"
	"github.com/sponge-dad/feed/app/content/worker/internal/svc"
	feedevent "github.com/sponge-dad/feed/common/event/feed"
	feedpb "github.com/sponge-dad/feed/app/feed/rpc/feed"

	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/zeromicro/go-zero/core/logx"
)

// FeedCreatedConsumer 消费 feed-created，启动视频内容分析。
//
// 幂等三层（04-content-analysis.md §2）：
//  1. Redis 互斥锁 content:analysis:lock:{feed_id}（SETNX，TTL=FFmpegTimeoutSec×3）
//  2. uk_feed_id 唯一键（UpsertByFeedID）
//  3. media_hash+model_version 判重跑（同一版本已 COMPLETED → 跳过）
//
// 非视频（feed_type != 2）→ 置 DISABLED 直接 ACK。
type FeedCreatedConsumer struct {
	svcCtx *svc.ServiceContext
	// sem 并发分析任务信号量（MaxConcurrency，FFmpeg 进程数上限）。
	sem chan struct{}
}

// NewFeedCreatedConsumer 创建消费者。
func NewFeedCreatedConsumer(svcCtx *svc.ServiceContext) *FeedCreatedConsumer {
	return &FeedCreatedConsumer{
		svcCtx: svcCtx,
		sem:    make(chan struct{}, svcCtx.Config.Media.MaxConcurrency),
	}
}

// Handle 处理单条消息。返回 error → MQ 重投（任务级重试 ≤MaxRetry 由重投驱动）。
func (c *FeedCreatedConsumer) Handle(ctx context.Context, msg *primitive.MessageExt) error {
	var ev feedevent.EventFeedCreate
	if err := json.Unmarshal(msg.Body, &ev); err != nil {
		// 消息体损坏不可恢复，直接 ACK，避免死信堆积。
		logx.Errorf("feed-created unmarshal failed msgId=%s", msg.MsgId)
		return nil
	}

	// 幂等 ①：分析互斥锁。SETNX 已存在说明其他实例正在分析/已分析 → 跳过。
	ok, err := c.svcCtx.Redis.SetnxEx(keys.AnalysisLockKey(ev.FeedID), "1", keys.TTLAnalysisLock)
	if err != nil {
		return err
	}
	if !ok {
		logx.Infof("feed %d analysis already locked, skip", ev.FeedID)
		return nil
	}
	// 无论成功失败，任务结束后释放锁。
	defer func() { _, _ = c.svcCtx.Redis.Del(keys.AnalysisLockKey(ev.FeedID)) }()

	// 查 Feed 详情（媒体地址/类型/作者/标题）。
	fi, err := c.svcCtx.FeedRpc.GetFeed(ctx, &feedpb.GetFeedReq{FeedId: ev.FeedID, UserId: ev.UserID})
	if err != nil {
		return fmt.Errorf("get feed %d: %w", ev.FeedID, err)
	}
	if fi.Feed == nil {
		logx.Infof("feed %d not found, mark DISABLED and ack", ev.FeedID)
		return c.markDisabled(ctx, ev.FeedID)
	}

	// 非视频（feed_type != 2）→ DISABLED，直接 ACK。
	if fi.Feed.FeedType != 2 {
		return c.markDisabled(ctx, ev.FeedID)
	}
	if len(fi.Feed.MediaUrls) == 0 {
		return c.markDisabled(ctx, ev.FeedID)
	}

	// 幂等 ②/③：media_hash+model_version 判重（已 COMPLETED 且同版本 → 跳过）。
	dup, err := c.isDuplicate(ctx, ev.FeedID)
	if err != nil {
		return err
	}
	if dup {
		logx.Infof("feed %d already analyzed with same model version, skip", ev.FeedID)
		return nil
	}

	// 并发控制：占用信号量（阻塞等待）。
	select {
	case c.sem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-c.sem }()

	task := &pipeline.Task{
		FeedID:    ev.FeedID,
		AuthorID:  fi.Feed.AuthorId,
		FeedType:  fi.Feed.FeedType,
		MediaURL:  fi.Feed.MediaUrls[0],
		Title:     fi.Feed.Title,
		Desc:      fi.Feed.Description,
		CityCode:  fi.Feed.CityCode,
		CityName:  fi.Feed.CityName,
		CreatedAt: fi.Feed.CreatedAt,
	}
	if err := c.svcCtx.Pipeline.Run(ctx, task); err != nil {
		// 返回 error → MQ 重投（RocketMQ 会重新投递）。
		logx.Errorf("pipeline run failed feed_id=%d err=%v reconsume_times=%d",
			ev.FeedID, err, msg.ReconsumeTimes)
		return err
	}
	return nil
}

// isDuplicate media_hash+model_version 判重：已 COMPLETED 且同版本 → 跳过。
// feed-created 事件不含 media_hash（worker 下载后才计算），这里以「画像已 COMPLETED
// 且 model_version 等于当前版本」作为判重（若此前已分析完成同版本，无需重跑）。
func (c *FeedCreatedConsumer) isDuplicate(ctx context.Context, feedID int64) (bool, error) {
	data, err := c.svcCtx.ContentProfilesModel.FindOneByFeedId(ctx, feedID)
	if errors.Is(err, model.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return data.AnalysisStatus == "COMPLETED" && data.ModelVersion == c.svcCtx.Config.Media.ModelVersion, nil
}

// markDisabled 非视频/已删除：置 DISABLED 并 ACK（不分析）。
// 失败必须返回 error 触发 MQ 重投，否则 DISABLED 承诺无法兑现。
func (c *FeedCreatedConsumer) markDisabled(ctx context.Context, feedID int64) error {
	if err := c.svcCtx.ContentProfilesModel.UpdateStatus(ctx, feedID, "DISABLED", "", 0, 0); err != nil {
		logx.Errorf("mark disabled failed feed_id=%d err=%v", feedID, err)
		return err
	}
	return nil
}
