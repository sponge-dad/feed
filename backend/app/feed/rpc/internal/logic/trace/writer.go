package trace

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"strconv"
	"time"

	"github.com/sponge-dad/feed/app/feed/rpc/internal/keys"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

const metaField = "meta"

// Write 将一次请求的 Trace 写入 Redis Hash（feed:trace:{request_id}）：
//   - meta        field 存放聚合 JSON（FeedRequestTrace，含 sources/items 明细）
//   - f:{feed_id} field 存放该 feed 的来源标记，便于 GetFeedSource 单条查询
//
// 行为：
//   - 写入失败仅记录日志，不阻塞主流程（降级）。
//   - 采样：sampleRate<=0 全部跳过；<1 按比例采样（rand.Float64()>rate 时跳过）；==1 全量。
//   - ttl 为该 key 的过期秒数。
//   - 注意：传入的 ctx 若随请求取消会导致写入丢失，故内部改用 context.Background() 执行 Redis 调用。
func Write(ctx context.Context, rdb *redis.Redis, t *FeedRequestTrace, ttl int, sampleRate float64) {
	if t.RequestId == "" {
		return
	}
	if sampleRate <= 0 {
		return
	}
	if sampleRate < 1 && rand.Float64() > sampleRate {
		return
	}

	key := keys.FeedTraceKey(t.RequestId)
	// §6.2：meta 仅存放聚合信息，不含逐条 Items（Items 由各 f:{feed_id} 字段承载），
	// 既避免冗余存储，也符合容量估算（meta < 2KB）。
	// 注意：直接浅拷贝 *t 会复制 proto 内部 MessageState（含锁），触发 go vet 报错，
	// 故先序列化再反序列化到新实例后清空 Items。
	raw, err := json.Marshal(t)
	if err != nil {
		logx.WithContext(ctx).Errorf("trace: marshal failed req=%s err=%v", t.RequestId, err)
		return
	}
	meta := &FeedRequestTrace{}
	if err = json.Unmarshal(raw, meta); err != nil {
		logx.WithContext(ctx).Errorf("trace: marshal meta failed req=%s err=%v", t.RequestId, err)
		return
	}
	meta.Items = nil
	payload, err := json.Marshal(meta)
	if err != nil {
		logx.WithContext(ctx).Errorf("trace: marshal failed req=%s err=%v", t.RequestId, err)
		return
	}

	bg := context.Background()
	err = rdb.PipelinedCtx(bg, func(pipe redis.Pipeliner) error {
		args := make([]interface{}, 0, 2+2*len(t.Items))
		args = append(args, metaField, string(payload))
		for _, it := range t.Items {
			args = append(args, "f:"+strconv.FormatInt(it.FeedId, 10), it.Source)
		}
		pipe.HSet(bg, key, args...)
		pipe.Expire(bg, key, time.Duration(ttl)*time.Second)
		return nil
	})
	if err != nil {
		// redis.Nil 表示 key 不存在等，属正常空结果，忽略。
		if errors.Is(err, redis.Nil) {
			return
		}
		logx.WithContext(ctx).Errorf("trace: write redis failed req=%s err=%v", t.RequestId, err)
	}
}
