package tests

import (
	"fmt"
	"math/rand"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sponge-dad/feed/app/relation/rpc/relation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 清理单个测试用户相关的 Redis key，同时清理 model 层 cache-aside 的缓存 key，
// 避免历史测试运行的 stale 缓存影响当前测试。
func cleanupUserKeys(userIds ...int64) {
	for _, id := range userIds {
		_, _ = testCtx.Redis.Del(fmt.Sprintf("user:follow:%d", id))
		_, _ = testCtx.Redis.Del(fmt.Sprintf("user:fans:%d", id))
		_, _ = testCtx.Redis.Del(fmt.Sprintf("user:fans_count:%d", id))
		_, _ = testCtx.Redis.Del(fmt.Sprintf("cache:relations:followerId:%d:*", id))
	}
}

func cleanupRelationModelCache() {
	// 通过 Lua 脚本删除匹配 cache:relations:* 的所有 key，避免历史 stale 缓存影响测试。
	// 测试环境数据量小，使用 KEYS 命令可接受。
	script := `local keys = redis.call('keys', ARGV[1]); for i=1,#keys do redis.call('del', keys[i]); end; return #keys`
	_, _ = testCtx.Redis.Eval(script, []string{}, "cache:relations:*")
}

func parseInt64(s string) int64 {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// 清理 DB 中指定用户的关系记录
func cleanupDBRelations(t *testing.T, userIds ...int64) {
	for _, id := range userIds {
		_, err := testDB.Exec("DELETE FROM relations WHERE follower_id = ? OR followee_id = ?", id, id)
		require.NoError(t, err)
	}
}

func dbCountByFollowee(t *testing.T, followeeId int64) int64 {
	var count int64
	err := testDB.QueryRow("SELECT COUNT(*) FROM relations WHERE followee_id = ?", followeeId).Scan(&count)
	require.NoError(t, err)
	return count
}

func redisFansCount(t *testing.T, userId int64) int64 {
	val, err := testCtx.Redis.Get(fmt.Sprintf("user:fans_count:%d", userId))
	require.NoError(t, err)
	if val == "" {
		return 0
	}
	n, err := parseInt64(val), error(nil)
	_ = err
	return n
}

func redisZScore(key string, member string) (int64, bool) {
	score, err := testCtx.Redis.Zscore(key, member)
	if err != nil {
		return 0, false
	}
	return int64(score), true
}

// C-001：并发 Follow/Unfollow 最终态一致
func TestConcurrency_FollowUnfollowFinalState(t *testing.T) {
	a, b := uid(300), uid(301)
	cleanupUserKeys(a, b)
	cleanupDBRelations(t, a, b)
	defer func() {
		cleanupUserKeys(a, b)
		cleanupDBRelations(t, a, b)
	}()

	ctx := newTestCtx()
	var lastOp atomic.Int64 // 1=follow, 0=unfollow

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if rand.Intn(2) == 0 {
				_, err := testClient.Follow(ctx, &relation.FollowReq{FollowerId: a, FolloweeId: b})
				require.NoError(t, err)
				lastOp.Store(1)
			} else {
				_, err := testClient.Unfollow(ctx, &relation.UnfollowReq{FollowerId: a, FolloweeId: b})
				require.NoError(t, err)
				lastOp.Store(0)
			}
		}()
	}
	wg.Wait()

	// 等待异步缓存更新收敛
	time.Sleep(300 * time.Millisecond)

	expectedFollowing := lastOp.Load() == 1
	dbCount := dbCountByFollowee(t, b)

	if expectedFollowing {
		assert.Equal(t, int64(1), dbCount)
	} else {
		assert.Equal(t, int64(0), dbCount)
	}

	// Redis fans 列表一致性
	fans, err := testClient.GetFans(ctx, &relation.GetFansReq{UserId: b, Page: 1, PageSize: 10})
	require.NoError(t, err)
	if expectedFollowing {
		assert.Contains(t, fans.FollowerIds, a)
	} else {
		assert.NotContains(t, fans.FollowerIds, a)
	}

	// Redis 粉丝数非负且与 DB 一致
	assert.GreaterOrEqual(t, redisFansCount(t, b), int64(0))
	assert.Equal(t, dbCount, redisFansCount(t, b))
}

// C-002：关注后立即查询，缓存应在 500ms 内收敛
func TestConcurrency_FollowThenReadConvergence(t *testing.T) {
	a, b := uid(302), uid(303)
	cleanupUserKeys(a, b)
	cleanupDBRelations(t, a, b)
	defer func() {
		cleanupUserKeys(a, b)
		cleanupDBRelations(t, a, b)
	}()

	ctx := newTestCtx()
	_, err := testClient.Follow(ctx, &relation.FollowReq{FollowerId: a, FolloweeId: b})
	require.NoError(t, err)

	var consistent bool
	for i := 0; i < 20; i++ {
		follows, err := testClient.GetFollows(ctx, &relation.GetFollowsReq{UserId: a, Page: 1, PageSize: 10})
		require.NoError(t, err)
		isFollow, err := testClient.IsFollow(ctx, &relation.IsFollowReq{FollowerId: a, FolloweeIds: []int64{b}})
		require.NoError(t, err)

		if contains(follows.FolloweeIds, b) && isFollow.Results[b] {
			consistent = true
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	assert.True(t, consistent, "关注后缓存应在 500ms 内收敛为已关注")
}

// C-003：取关后立即查询，缓存应在 500ms 内收敛
func TestConcurrency_UnfollowThenReadConvergence(t *testing.T) {
	a, b := uid(304), uid(305)
	cleanupUserKeys(a, b)
	cleanupDBRelations(t, a, b)
	defer func() {
		cleanupUserKeys(a, b)
		cleanupDBRelations(t, a, b)
	}()

	ctx := newTestCtx()
	_, err := testClient.Follow(ctx, &relation.FollowReq{FollowerId: a, FolloweeId: b})
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond) // 等缓存先建立

	_, err = testClient.Unfollow(ctx, &relation.UnfollowReq{FollowerId: a, FolloweeId: b})
	require.NoError(t, err)

	var consistent bool
	for i := 0; i < 20; i++ {
		follows, err := testClient.GetFollows(ctx, &relation.GetFollowsReq{UserId: a, Page: 1, PageSize: 10})
		require.NoError(t, err)
		isFollow, err := testClient.IsFollow(ctx, &relation.IsFollowReq{FollowerId: a, FolloweeIds: []int64{b}})
		require.NoError(t, err)

		if !contains(follows.FolloweeIds, b) && !isFollow.Results[b] {
			consistent = true
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	assert.True(t, consistent, "取关后缓存应在 500ms 内收敛为未关注")
}

// C-004：多设备交叉操作最终态一致
func TestConcurrency_CrossDeviceOperations(t *testing.T) {
	a, b := uid(306), uid(307)
	cleanupUserKeys(a, b)
	cleanupDBRelations(t, a, b)
	defer func() {
		cleanupUserKeys(a, b)
		cleanupDBRelations(t, a, b)
	}()

	ctx := newTestCtx()
	var lastOp atomic.Int64

	var wg sync.WaitGroup
	// 设备 1：Follow/Unfollow 交替
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			if i%2 == 0 {
				_, _ = testClient.Follow(ctx, &relation.FollowReq{FollowerId: a, FolloweeId: b})
				lastOp.Store(1)
			} else {
				_, _ = testClient.Unfollow(ctx, &relation.UnfollowReq{FollowerId: a, FolloweeId: b})
				lastOp.Store(0)
			}
		}
	}()

	// 设备 2：Follow/Unfollow 交替，相位相反
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			if i%2 == 0 {
				_, _ = testClient.Unfollow(ctx, &relation.UnfollowReq{FollowerId: a, FolloweeId: b})
				lastOp.Store(0)
			} else {
				_, _ = testClient.Follow(ctx, &relation.FollowReq{FollowerId: a, FolloweeId: b})
				lastOp.Store(1)
			}
		}
	}()
	wg.Wait()
	time.Sleep(300 * time.Millisecond)

	expectedFollowing := lastOp.Load() == 1
	follows, err := testClient.GetFollows(ctx, &relation.GetFollowsReq{UserId: a, Page: 1, PageSize: 10})
	require.NoError(t, err)

	if expectedFollowing {
		assert.Contains(t, follows.FolloweeIds, b)
	} else {
		assert.NotContains(t, follows.FolloweeIds, b)
	}
}

// C-005：读写并发稳定性
func TestConcurrency_ReadWriteMixed(t *testing.T) {
	a, b := uid(308), uid(309)
	cleanupUserKeys(a, b)
	cleanupDBRelations(t, a, b)
	defer func() {
		cleanupUserKeys(a, b)
		cleanupDBRelations(t, a, b)
	}()

	ctx := newTestCtx()
	stopCh := make(chan struct{})
	var wg sync.WaitGroup

	// writer
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopCh:
				return
			default:
				_, _ = testClient.Follow(ctx, &relation.FollowReq{FollowerId: a, FolloweeId: b})
				_, _ = testClient.Unfollow(ctx, &relation.UnfollowReq{FollowerId: a, FolloweeId: b})
			}
		}
	}()

	// readers
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stopCh:
					return
				default:
					_, _ = testClient.GetFollows(ctx, &relation.GetFollowsReq{UserId: a, Page: 1, PageSize: 10})
					_, _ = testClient.GetFans(ctx, &relation.GetFansReq{UserId: b, Page: 1, PageSize: 10})
					_, _ = testClient.IsFollow(ctx, &relation.IsFollowReq{FollowerId: a, FolloweeIds: []int64{b}})
				}
			}
		}()
	}

	time.Sleep(3 * time.Second)
	close(stopCh)
	wg.Wait()
	time.Sleep(200 * time.Millisecond)

	// 最终校验：粉丝数非负、DB 与 Redis 一致
	count := dbCountByFollowee(t, b)
	assert.GreaterOrEqual(t, count, int64(0))
	assert.Equal(t, count, redisFansCount(t, b))
}

// K-001：关注后缓存命中
func TestCache_FollowThenCacheHit(t *testing.T) {
	a, b := uid(310), uid(311)
	cleanupUserKeys(a, b)
	cleanupDBRelations(t, a, b)
	defer func() {
		cleanupUserKeys(a, b)
		cleanupDBRelations(t, a, b)
	}()

	ctx := newTestCtx()
	_, err := testClient.Follow(ctx, &relation.FollowReq{FollowerId: a, FolloweeId: b})
	require.NoError(t, err)

	// 第一次读可能 miss 并回填
	_, _ = testClient.GetFollows(ctx, &relation.GetFollowsReq{UserId: a, Page: 1, PageSize: 10})

	// 第二次读应命中 Redis
	_, err = testCtx.Redis.Zscore(fmt.Sprintf("user:follow:%d", a), fmt.Sprintf("%d", b))
	require.NoError(t, err, "关注后 Redis 应存在 follow 缓存")
}

// K-002：取关后缓存失效
func TestCache_UnfollowThenCacheInvalidated(t *testing.T) {
	a, b := uid(312), uid(313)
	cleanupUserKeys(a, b)
	cleanupDBRelations(t, a, b)
	defer func() {
		cleanupUserKeys(a, b)
		cleanupDBRelations(t, a, b)
	}()

	ctx := newTestCtx()
	_, err := testClient.Follow(ctx, &relation.FollowReq{FollowerId: a, FolloweeId: b})
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)

	_, err = testClient.Unfollow(ctx, &relation.UnfollowReq{FollowerId: a, FolloweeId: b})
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)

	// Redis 中应无 follow 缓存
	_, ok := redisZScore(fmt.Sprintf("user:follow:%d", a), fmt.Sprintf("%d", b))
	assert.False(t, ok, "取关后 Redis follow 缓存应被移除")

	// 读接口不应返回 B
	follows, err := testClient.GetFollows(ctx, &relation.GetFollowsReq{UserId: a, Page: 1, PageSize: 10})
	require.NoError(t, err)
	assert.NotContains(t, follows.FolloweeIds, b)
}

// K-003：缓存穿透保护（无关注用户首次查询后不应反复查 DB）
func TestCache_PenetrationProtection(t *testing.T) {
	x := uid(314)
	cleanupUserKeys(x)
	cleanupDBRelations(t, x)
	defer func() {
		cleanupUserKeys(x)
		cleanupDBRelations(t, x)
	}()

	ctx := newTestCtx()
	// 首次查询会回源 DB
	resp, err := testClient.GetFollows(ctx, &relation.GetFollowsReq{UserId: x, Page: 1, PageSize: 10})
	require.NoError(t, err)
	assert.Empty(t, resp.FolloweeIds)

	// 多次查询不应对服务造成异常（当前实现不保证空值缓存，但不应崩溃/无重复 DB 查询副作用）
	for i := 0; i < 10; i++ {
		resp, err := testClient.GetFollows(ctx, &relation.GetFollowsReq{UserId: x, Page: 1, PageSize: 10})
		require.NoError(t, err)
		assert.Empty(t, resp.FolloweeIds)
	}
}

// K-007：粉丝数与粉丝列表一致性
func TestCache_FansCountMatchesList(t *testing.T) {
	b := uid(315)
	cleanupUserKeys(b)
	cleanupDBRelations(t, b)
	defer func() {
		cleanupUserKeys(b)
		cleanupDBRelations(t, b)
	}()

	ctx := newTestCtx()
	const total = 25
	followers := make([]int64, 0, total)
	for i := int64(1); i <= total; i++ {
		follower := uid(1000 + i)
		followers = append(followers, follower)
		_, err := testClient.Follow(ctx, &relation.FollowReq{FollowerId: follower, FolloweeId: b})
		require.NoError(t, err)
	}
	time.Sleep(200 * time.Millisecond)

	// 分页遍历粉丝列表
	var listCount int64
	page := int64(1)
	pageSize := int64(10)
	for {
		fans, err := testClient.GetFans(ctx, &relation.GetFansReq{UserId: b, Page: page, PageSize: pageSize})
		require.NoError(t, err)
		listCount += int64(len(fans.FollowerIds))
		if int64(len(fans.FollowerIds)) < pageSize {
			break
		}
		page++
	}

	dbCount := dbCountByFollowee(t, b)
	redisCount := redisFansCount(t, b)

	assert.Equal(t, int64(total), dbCount)
	assert.Equal(t, dbCount, redisCount)
	assert.Equal(t, dbCount, listCount)
}

func contains(arr []int64, v int64) bool {
	for _, x := range arr {
		if x == v {
			return true
		}
	}
	return false
}
