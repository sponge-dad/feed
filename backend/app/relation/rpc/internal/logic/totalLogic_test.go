// totalLogic_test.go
//
// 职责：relation GetFollows/GetFans 的 Total 语义与回源测试（R-LS-02）。
//
// R-LS-02 修复说明：缓存命中时 Total 取自 Redis ZSet 基数(ZCARD，全量计数)，
// 而非分页片段长度，避免用户关注/粉丝数（网关展示）在关注数超过分页大小时被截断。
// 冷缓存（回源）路径无 CountByFollowerId 接口，Total 退化为分页片段长度（已知限制，见报告）。
package logic

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/sponge-dad/feed/app/relation/model"
	"github.com/sponge-dad/feed/app/relation/rpc/relation"
)

// TestGetFollows_CacheHitTotalIsZCard 验证 R-LS-02：缓存命中时 Total = 全量基数(15) 而非分页片段长度(10)。
func TestGetFollows_CacheHitTotalIsZCard(t *testing.T) {
	ctx, closeFn := newTestServiceContext(t)
	defer closeFn()

	key := followKey(1)
	for i := int64(100); i < 115; i++ {
		if _, err := ctx.Redis.Zadd(key, i, strconv.FormatInt(i, 10)); err != nil {
			t.Fatalf("prefill follow zset failed: %v", err)
		}
	}

	resp, err := NewGetFollowsLogic(context.Background(), ctx).GetFollows(&relation.GetFollowsReq{
		UserId: 1, Page: 1, PageSize: 10,
	})
	assert.NoError(t, err)
	assert.Len(t, resp.FolloweeIds, 10, "本页应返回 10 条")
	assert.Equal(t, int64(15), resp.Total, "R-LS-02：Total 应为缓存基数 15，而非分页片段长度 10")
}

// TestGetFans_CacheHitTotalIsZCard 验证 R-LS-02：粉丝侧缓存命中同样使用 ZCARD 基数。
func TestGetFans_CacheHitTotalIsZCard(t *testing.T) {
	ctx, closeFn := newTestServiceContext(t)
	defer closeFn()

	key := fansKey(2)
	for i := int64(200); i < 213; i++ {
		if _, err := ctx.Redis.Zadd(key, i, strconv.FormatInt(i, 10)); err != nil {
			t.Fatalf("prefill fans zset failed: %v", err)
		}
	}

	resp, err := NewGetFansLogic(context.Background(), ctx).GetFans(&relation.GetFansReq{
		UserId: 2, Page: 1, PageSize: 10,
	})
	assert.NoError(t, err)
	assert.Len(t, resp.FollowerIds, 10)
	assert.Equal(t, int64(13), resp.Total, "R-LS-02：粉丝 Total 应为缓存基数 13")
}

// TestGetFollows_CacheMissReturnsPageSlice 验证冷缓存(回源)路径：Total 退化成分页片段长度（已知限制）。
func TestGetFollows_CacheMissReturnsPageSlice(t *testing.T) {
	ctx, closeFn := newTestServiceContext(t)
	defer closeFn()

	mm, ok := ctx.RelationModel.(*memoryRelationsModel)
	assert.True(t, ok)
	for i := int64(100); i < 108; i++ {
		if _, err := mm.Insert(context.Background(), &model.Relations{
			Id: uint64(i), FollowerId: 1, FolloweeId: uint64(i), CreatedAt: i,
		}); err != nil {
			t.Fatalf("insert failed: %v", err)
		}
	}

	resp, err := NewGetFollowsLogic(context.Background(), ctx).GetFollows(&relation.GetFollowsReq{
		UserId: 1, Page: 1, PageSize: 20,
	})
	assert.NoError(t, err)
	assert.Len(t, resp.FolloweeIds, 8)
	assert.Equal(t, int64(8), resp.Total, "回源路径 Total 为分页片段长度(已知限制)")
}
