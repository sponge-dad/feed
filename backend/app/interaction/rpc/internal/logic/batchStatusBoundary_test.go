// batchStatusBoundary_test.go
//
// 职责：BatchGetUserInteractionStatus 入参校验与边界加强（5.5 Interaction 弱测试加强）。
// 关注空请求、非法 user/feedID、超过批量上限(100)、返回顺序与请求一致等边界。
package logic

import (
	"context"
	"testing"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/keys"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBatchGetUserInteractionStatus_Empty_NoError 空 feedIDs 应返回空列表且不报错、不回源。
func TestBatchGetUserInteractionStatus_Empty_NoError(t *testing.T) {
	env := newTestEnv(t)
	resp, err := NewBatchGetUserInteractionStatusLogic(context.Background(), env.svcCtx).
		BatchGetUserInteractionStatus(&interaction.BatchGetUserInteractionStatusReq{UserId: 1})
	require.NoError(t, err)
	assert.Empty(t, resp.StatusList)
}

// TestBatchGetUserInteractionStatus_InvalidUser_ParamError user_id<=0 直接拒绝。
func TestBatchGetUserInteractionStatus_InvalidUser_ParamError(t *testing.T) {
	env := newTestEnv(t)
	_, err := NewBatchGetUserInteractionStatusLogic(context.Background(), env.svcCtx).
		BatchGetUserInteractionStatus(&interaction.BatchGetUserInteractionStatusReq{FeedIds: []int64{1}})
	requireBizCode(t, err, errorx.ParamError)
}

// TestBatchGetUserInteractionStatus_InvalidFeedID_ParamError 任一 feedID<=0 直接拒绝。
func TestBatchGetUserInteractionStatus_InvalidFeedID_ParamError(t *testing.T) {
	env := newTestEnv(t)
	_, err := NewBatchGetUserInteractionStatusLogic(context.Background(), env.svcCtx).
		BatchGetUserInteractionStatus(&interaction.BatchGetUserInteractionStatusReq{UserId: 1, FeedIds: []int64{1, 0, 3}})
	requireBizCode(t, err, errorx.ParamError)
}

// TestBatchGetUserInteractionStatus_ExceedLimit_ParamError 超过单次上限(100)拒绝。
func TestBatchGetUserInteractionStatus_ExceedLimit_ParamError(t *testing.T) {
	env := newTestEnv(t)
	big := make([]int64, maxBatchSize+1)
	for i := range big {
		big[i] = int64(i + 1)
	}
	_, err := NewBatchGetUserInteractionStatusLogic(context.Background(), env.svcCtx).
		BatchGetUserInteractionStatus(&interaction.BatchGetUserInteractionStatusReq{UserId: 1, FeedIds: big})
	requireBizCode(t, err, errorx.ParamError)
}

// TestBatchGetUserInteractionStatus_OrderPreserved 返回顺序与请求 feedIDs 严格一致。
func TestBatchGetUserInteractionStatus_OrderPreserved(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	// 预置：f2 已点赞、f4 已收藏（MySQL 冷数据），其余无记录
	env.likes.seed(1, 2, 1, testTime())
	env.collects.seed(1, 4, 1, testTime())
	env.mr.Del(keys.LikeFeed(2))
	env.mr.Del(keys.CollectFeed(4))

	ids := []int64{3, 1, 4, 2, 5}
	resp, err := NewBatchGetUserInteractionStatusLogic(ctx, env.svcCtx).
		BatchGetUserInteractionStatus(&interaction.BatchGetUserInteractionStatusReq{UserId: 1, FeedIds: ids})
	require.NoError(t, err)
	require.Len(t, resp.StatusList, len(ids))

	for i, st := range resp.StatusList {
		assert.Equal(t, ids[i], st.FeedId, "第 %d 项顺序应与请求一致", i)
	}
	// 逐项断言（非整段回源导致错位）
	assert.False(t, resp.StatusList[0].IsLiked, "f3 无记录")
	assert.False(t, resp.StatusList[1].IsLiked, "f1 无记录")
	assert.True(t, resp.StatusList[2].IsCollected, "f4 已收藏")
	assert.True(t, resp.StatusList[3].IsLiked, "f2 回源 MySQL 命中")
	assert.False(t, resp.StatusList[4].IsCollected, "f5 无记录")
}
