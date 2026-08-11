// interest_test.go
//
// 职责：兴趣画像纯逻辑单元测试。
// 覆盖：key 约定、行为权重表（EXPOSE=0 不入画像）、BuildSnapshot 分类拆分、
// SnapshotJSON 结构、去重 key。
//
// 说明：ApplyEvent/Decay 依赖 Lua 脚本（ZINCRBY/SADD/EXPIRE/ZREMRANGEBYSCORE 原子合并），
// miniredis 不支持 EVAL，这两个函数的脚本执行路径需在真实 Redis 环境验证
// （06-user-interest.md §7 已知约束）；此处仅测不依赖脚本的纯函数。
package interest

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"time"
)

func TestActionWeights_ExposeNotTracked(t *testing.T) {
	// EXPOSE 权重 0：不入画像
	assert.Equal(t, int64(0), ActionWeights["EXPOSE"])
	assert.Equal(t, int64(5), ActionWeights["FINISH"])
	assert.Equal(t, int64(-2), ActionWeights["SKIP"])
}

func TestKeyConventions(t *testing.T) {
	assert.Equal(t, "user:interest:42", InterestKey(42))
	assert.Equal(t, "interest:dedup:42:555:FINISH", DedupKey(42, 555, "FINISH"))
	assert.Equal(t, "content:profile:555", ProfileCacheKey(555))
	assert.Equal(t, "interest:active:20260810", ActiveKey(time.Date(2026, 8, 10, 10, 0, 0, 0, time.Local)))
}

func TestBuildSnapshot_SplitsCategoriesAndTopics(t *testing.T) {
	mr := miniredis.RunT(t)
	rds := redis.MustNewRedis(redis.RedisConf{Host: mr.Addr(), Type: "node"})

	key := InterestKey(42)
	_, err := mr.ZAdd(key, 5, "c:户外旅行")
	require.NoError(t, err)
	_, err = mr.ZAdd(key, 3, "t:露营")
	require.NoError(t, err)
	_, err = mr.ZAdd(key, 2, "t:装备")
	require.NoError(t, err)

	snap, err := BuildSnapshot(context.Background(), rds, 42)
	require.NoError(t, err)
	assert.Equal(t, int64(3), snap.TotalActions)
	require.Len(t, snap.Categories, 1)
	assert.Equal(t, "户外旅行", snap.Categories[0].Name)
	assert.Equal(t, 5.0, snap.Categories[0].Score)
	require.Len(t, snap.Topics, 2)
	assert.Equal(t, "露营", snap.Topics[0].Name)
}

func TestBuildSnapshot_EmptyUser(t *testing.T) {
	mr := miniredis.RunT(t)
	rds := redis.MustNewRedis(redis.RedisConf{Host: mr.Addr(), Type: "node"})
	snap, err := BuildSnapshot(context.Background(), rds, 1)
	require.NoError(t, err)
	assert.Equal(t, int64(0), snap.TotalActions)
	assert.Empty(t, snap.Categories)
	assert.Empty(t, snap.Topics)
}

func TestSnapshotJSON_Structure(t *testing.T) {
	s := &Snapshot{
		Categories:   []Item{{Name: "户外旅行", Score: 5}},
		Topics:       []Item{{Name: "露营", Score: 3}},
		TotalActions: 8,
		CalculatedAt: time.Now(),
	}
	jsonStr := s.SnapshotJSON()
	assert.Contains(t, jsonStr, `"categories"`)
	assert.Contains(t, jsonStr, `"topics"`)
	assert.Contains(t, jsonStr, `"total_actions":8`)
	assert.Contains(t, jsonStr, `"window_days":30`)
}
