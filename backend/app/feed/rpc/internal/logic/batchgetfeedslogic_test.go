// Package logic 的单元测试：BatchGetFeeds（批量回填）。
package logic

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sponge-dad/feed/app/feed/rpc/feed"
)

// TestBatchGetFeeds 验证批量查询仅返回存在的帖子（不存在的 ID 被忽略）。
func TestBatchGetFeeds(t *testing.T) {
	m := newStubFeedsModel()
	m.byID[31] = mkFeed(31, 1, time.Now())
	m.byID[32] = mkFeed(32, 1, time.Now())
	// 33 不在库里，应被忽略
	svcCtx := newTestSvc(t, m, &stubRelation{})
	l := NewBatchGetFeedsLogic(context.Background(), svcCtx)

	resp, err := l.BatchGetFeeds(&feed.BatchGetFeedsReq{FeedIds: []int64{31, 32, 33}})
	require.NoError(t, err)
	require.Len(t, resp.Feeds, 2)
	assert.Contains(t, resp.Feeds, int64(31))
	assert.Contains(t, resp.Feeds, int64(32))
}

// TestBatchGetFeeds_Empty 验证空请求返回空映射。
func TestBatchGetFeeds_Empty(t *testing.T) {
	m := newStubFeedsModel()
	svcCtx := newTestSvc(t, m, &stubRelation{})
	l := NewBatchGetFeedsLogic(context.Background(), svcCtx)

	resp, err := l.BatchGetFeeds(&feed.BatchGetFeedsReq{FeedIds: nil})
	require.NoError(t, err)
	assert.Empty(t, resp.Feeds)
}

// TestBatchGetFeeds_Oversize 验证单次批量上限保护（>100 截断）。
func TestBatchGetFeeds_Oversize(t *testing.T) {
	m := newStubFeedsModel()
	ids := make([]int64, 0, 150)
	for i := int64(1); i <= 150; i++ {
		m.byID[uint64(i)] = mkFeed(uint64(i), 1, time.Now())
		ids = append(ids, i)
	}
	svcCtx := newTestSvc(t, m, &stubRelation{})
	l := NewBatchGetFeedsLogic(context.Background(), svcCtx)

	resp, err := l.BatchGetFeeds(&feed.BatchGetFeedsReq{FeedIds: ids})
	require.NoError(t, err)
	assert.Len(t, resp.Feeds, maxBatchFeedIDs)
}
