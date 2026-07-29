// batchGetUsersLogic_test.go
//
// BatchGetUsers RPC Logic 单元测试。
// 基线编号说明：docs/api-test-baseline.md 未为 BatchGetUsers 单独编号
// （仅在 U-UPD-03 中作为依赖出现），本文件按模块前缀补充 U-BGU-01 ~ U-BGU-08
// （详见 docs/test-implementation-report.md）。
package logic

import (
	"context"
	"errors"
	"testing"

	usermodel "github.com/sponge-dad/feed/app/user/model"
	"github.com/sponge-dad/feed/app/user/rpc/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedBatchUsers(m *stubUsersModel) {
	m.add(&usermodel.Users{Id: 1, Username: "u1", Nickname: "昵称1", Avatar: "a1", Status: 1})
	m.add(&usermodel.Users{Id: 2, Username: "u2", Nickname: "昵称2", Avatar: "a2", Status: 1})
	m.add(&usermodel.Users{Id: 3, Username: "u3", Nickname: "昵称3", Avatar: "a3", Status: 1})
}

// Baseline: U-BGU-01（全部缓存命中：不触发 MySQL 查询，顺序与请求一致）
func TestBatchGetUsers_AllCacheHit_NoDatabaseQuery(t *testing.T) {
	m := newStubUsersModel()
	seedBatchUsers(m)
	svcCtx, mr := newTestServiceContext(t, m)
	require.NoError(t, mr.Set("user:brief:1", `{"id":1,"nickname":"缓存昵称1","avatar":"ca1"}`))
	require.NoError(t, mr.Set("user:brief:2", `{"id":2,"nickname":"缓存昵称2","avatar":"ca2"}`))

	l := NewBatchGetUsersLogic(context.Background(), svcCtx)
	resp, err := l.BatchGetUsers(&user.BatchGetUsersReq{UserIds: []int64{2, 1}})
	require.NoError(t, err)

	require.Len(t, resp.Users, 2)
	// 返回顺序与请求顺序一致（当前实现按 in.UserIds 顺序组装）
	assert.Equal(t, int64(2), resp.Users[0].Id)
	assert.Equal(t, "缓存昵称2", resp.Users[0].Nickname)
	assert.Equal(t, int64(1), resp.Users[1].Id)
	assert.Equal(t, "缓存昵称1", resp.Users[1].Nickname)
	assert.Equal(t, 0, m.findByIdsCalls, "全部命中缓存时不得回源 MySQL")
}

// Baseline: U-BGU-02（部分命中：仅未命中 ID 回源，并回填缓存与 TTL）
func TestBatchGetUsers_PartialCacheHit_QueriesOnlyMissAndBackfills(t *testing.T) {
	m := newStubUsersModel()
	seedBatchUsers(m)
	svcCtx, mr := newTestServiceContext(t, m)
	require.NoError(t, mr.Set("user:brief:1", `{"id":1,"nickname":"缓存昵称1","avatar":"ca1"}`))

	l := NewBatchGetUsersLogic(context.Background(), svcCtx)
	resp, err := l.BatchGetUsers(&user.BatchGetUsersReq{UserIds: []int64{1, 2, 3}})
	require.NoError(t, err)

	require.Len(t, resp.Users, 3)
	assert.Equal(t, []int64{1, 2, 3}, []int64{resp.Users[0].Id, resp.Users[1].Id, resp.Users[2].Id})
	assert.Equal(t, "缓存昵称1", resp.Users[0].Nickname, "命中部分来自缓存")
	assert.Equal(t, "昵称2", resp.Users[1].Nickname, "未命中部分来自 MySQL")

	// 仅未命中的 2、3 回源
	require.Equal(t, 1, m.findByIdsCalls)
	assert.Equal(t, []int64{2, 3}, m.findByIdsArgs[0])

	// 回填 user:brief:{id} 且 TTL=600s
	for _, id := range []string{"2", "3"} {
		key := "user:brief:" + id
		require.True(t, mr.Exists(key), "回源结果应回填缓存 %s", key)
		ttl := mr.TTL(key)
		assert.InDelta(t, 600, ttl.Seconds(), 1, "brief 缓存 TTL 应为 600s")
	}
	cached, err := mr.Get("user:brief:2")
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":2,"nickname":"昵称2","avatar":"a2"}`, cached)
}

// Baseline: U-BGU-03（全部未命中：一次 IN 查询回源）
func TestBatchGetUsers_AllCacheMiss_SingleBatchQuery(t *testing.T) {
	m := newStubUsersModel()
	seedBatchUsers(m)
	svcCtx, _ := newTestServiceContext(t, m)

	l := NewBatchGetUsersLogic(context.Background(), svcCtx)
	resp, err := l.BatchGetUsers(&user.BatchGetUsersReq{UserIds: []int64{3, 1}})
	require.NoError(t, err)

	require.Len(t, resp.Users, 2)
	assert.Equal(t, int64(3), resp.Users[0].Id)
	assert.Equal(t, int64(1), resp.Users[1].Id)
	require.Equal(t, 1, m.findByIdsCalls, "必须一次 IN 查询，不得循环单查")
	assert.Equal(t, []int64{3, 1}, m.findByIdsArgs[0])
}

// Baseline: U-BGU-04（重复 ID 当前行为基线：响应按请求出现次数重复返回）
func TestBatchGetUsers_DuplicateIDs_CurrentlyReturnsDuplicateEntriesBaseline(t *testing.T) {
	m := newStubUsersModel()
	seedBatchUsers(m)
	svcCtx, _ := newTestServiceContext(t, m)

	l := NewBatchGetUsersLogic(context.Background(), svcCtx)
	resp, err := l.BatchGetUsers(&user.BatchGetUsersReq{UserIds: []int64{1, 1, 2}})
	require.NoError(t, err)

	// 当前实现按传入顺序逐个组装，重复 ID 会重复出现（行为基线，未去重）
	require.Len(t, resp.Users, 3)
	assert.Equal(t, int64(1), resp.Users[0].Id)
	assert.Equal(t, int64(1), resp.Users[1].Id)
	assert.Equal(t, int64(2), resp.Users[2].Id)
}

// Baseline: U-BGU-05（不存在的 ID 被跳过，不报错、不返回占位）
func TestBatchGetUsers_NonexistentIDs_SkippedSilently(t *testing.T) {
	m := newStubUsersModel()
	seedBatchUsers(m)
	svcCtx, _ := newTestServiceContext(t, m)

	l := NewBatchGetUsersLogic(context.Background(), svcCtx)
	resp, err := l.BatchGetUsers(&user.BatchGetUsersReq{UserIds: []int64{1, 404, 2}})
	require.NoError(t, err)

	require.Len(t, resp.Users, 2, "不存在的 ID 应被跳过")
	assert.Equal(t, int64(1), resp.Users[0].Id)
	assert.Equal(t, int64(2), resp.Users[1].Id)
}

// Baseline: U-BGU-06（空 ID 列表：直接返回空结果，无任何存储访问）
func TestBatchGetUsers_EmptyIDList_ReturnsEmptyWithoutStorageAccess(t *testing.T) {
	m := newStubUsersModel()
	svcCtx, _ := newTestServiceContext(t, m)

	l := NewBatchGetUsersLogic(context.Background(), svcCtx)
	resp, err := l.BatchGetUsers(&user.BatchGetUsersReq{UserIds: nil})
	require.NoError(t, err)
	assert.Empty(t, resp.Users)
	assert.Equal(t, 0, m.findByIdsCalls)
}

// Baseline: U-BGU-07（Redis 故障降级：MGET 失败退化为全量 MySQL 查询，主流程成功）
func TestBatchGetUsers_RedisDown_DegradesToFullDatabaseQuery(t *testing.T) {
	m := newStubUsersModel()
	seedBatchUsers(m)
	svcCtx, mr := newTestServiceContext(t, m)
	mr.SetError("redis: connection refused") // 所有 Redis 命令报错

	l := NewBatchGetUsersLogic(context.Background(), svcCtx)
	resp, err := l.BatchGetUsers(&user.BatchGetUsersReq{UserIds: []int64{1, 2}})
	require.NoError(t, err, "Redis 故障不得阻断主流程")

	require.Len(t, resp.Users, 2)
	assert.Equal(t, "昵称1", resp.Users[0].Nickname)
	assert.Equal(t, "昵称2", resp.Users[1].Nickname)
	// 全部 ID 回源 MySQL
	require.Equal(t, 1, m.findByIdsCalls)
	assert.Equal(t, []int64{1, 2}, m.findByIdsArgs[0])
}

// Baseline: U-BGU-08（MySQL 回源失败 → 返回错误）
func TestBatchGetUsers_DatabaseQueryFails_ReturnsError(t *testing.T) {
	m := newStubUsersModel()
	dbErr := errors.New("mysql: connection refused")
	m.findByIdsErr = dbErr
	svcCtx, _ := newTestServiceContext(t, m)

	l := NewBatchGetUsersLogic(context.Background(), svcCtx)
	resp, err := l.BatchGetUsers(&user.BatchGetUsersReq{UserIds: []int64{1}})
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, dbErr)
}
