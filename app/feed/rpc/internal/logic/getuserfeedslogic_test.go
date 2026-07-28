// Package logic 的单元测试：GetUserFeeds（个人主页 offset 分页）。
package logic

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sponge-dad/feed/app/feed/model"
	"github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/common/errorx"
)

// TestGetUserFeeds 验证个人主页分页回填正确。
func TestGetUserFeeds(t *testing.T) {
	m := newStubFeedsModel()
	m.byUser[100] = []*model.Feeds{mkFeed(11, 100, time.Now()), mkFeed(12, 100, time.Now())}
	svcCtx := newTestSvc(t, m, &stubRelation{})
	l := NewGetUserFeedsLogic(context.Background(), svcCtx)

	resp, err := l.GetUserFeeds(&feed.GetUserFeedsReq{UserId: 100, Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, resp.Feeds, 2)
	assert.Equal(t, int64(11), resp.Feeds[0].FeedId)
	assert.Equal(t, int64(12), resp.Feeds[1].FeedId)
	assert.False(t, resp.Page.HasMore)
}

// TestGetUserFeeds_Param 验证非法用户 ID 返回 ParamError。
func TestGetUserFeeds_Param(t *testing.T) {
	m := newStubFeedsModel()
	svcCtx := newTestSvc(t, m, &stubRelation{})
	l := NewGetUserFeedsLogic(context.Background(), svcCtx)

	_, err := l.GetUserFeeds(&feed.GetUserFeedsReq{UserId: 0, Page: 1, PageSize: 10})
	require.Error(t, err)
	ce, ok := err.(*errorx.CodeError)
	require.True(t, ok)
	assert.Equal(t, errorx.ParamError, ce.Code)
}
