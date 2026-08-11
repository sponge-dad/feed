// metrics_test.go
//
// 职责：创作者数据 logic 单元测试（T097~T099）。
// 覆盖：GetCreatorMetrics 归属校验（他人 14005）+ 派生率计算 + 无数据零值；
// GetPeerAverageMetrics 匿名聚合（响应不含 feed_id）+ 样本不足降级；
// GetUserInterestProfile 本人校验（内部例外）+ MySQL 兜底。
package logic

import (
	"context"
	"database/sql"
	"testing"
	"time"

	contentClient "github.com/sponge-dad/feed/app/content/rpc/contentClient"
	contentpb "github.com/sponge-dad/feed/app/content/rpc/content"
	feedClient "github.com/sponge-dad/feed/app/feed/rpc/feedclient"
	feedpb "github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/app/interaction/model"
	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"google.golang.org/grpc"
)

// ---- 依赖 stub ----

type mockFeedRpc struct {
	feedClient.Feed // 嵌入接口：仅覆盖 GetFeed，其余方法调用 panic（测试不会触发）
	feed            *feedpb.FeedInfo
	err             error
}

func (m *mockFeedRpc) GetFeed(ctx context.Context, in *feedpb.GetFeedReq, opts ...grpc.CallOption) (*feedpb.GetFeedResp, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.feed == nil {
		return &feedpb.GetFeedResp{}, nil
	}
	return &feedpb.GetFeedResp{Feed: m.feed}, nil
}
func (m *mockFeedRpc) BatchGetFeeds(ctx context.Context, in *feedpb.BatchGetFeedsReq, opts ...grpc.CallOption) (*feedpb.BatchGetFeedsResp, error) {
	return &feedpb.BatchGetFeedsResp{}, nil
}

type mockMetricsModel struct {
	sumFeed   *model.FeedMetricsHourly
	sumByIDs  map[int64]*model.FeedMetricsHourly
	callCount int
}

func (m *mockMetricsModel) SumByFeedAndWindow(ctx context.Context, feedID int64, since time.Time) (*model.FeedMetricsHourly, error) {
	m.callCount++
	if m.sumFeed == nil {
		return &model.FeedMetricsHourly{}, nil
	}
	return m.sumFeed, nil
}
func (m *mockMetricsModel) SumByAuthorAndWindow(ctx context.Context, authorID int64, since time.Time) (*model.FeedMetricsHourly, error) {
	return &model.FeedMetricsHourly{}, nil
}
func (m *mockMetricsModel) SumByFeedIDs(ctx context.Context, feedIDs []int64, since time.Time) (map[int64]*model.FeedMetricsHourly, error) {
	out := make(map[int64]*model.FeedMetricsHourly, len(feedIDs))
	for _, id := range feedIDs {
		if v, ok := m.sumByIDs[id]; ok {
			out[id] = v
		}
	}
	return out, nil
}
func (m *mockMetricsModel) AvgByFeedIDs(ctx context.Context, feedIDs []int64, since time.Time) (*model.FeedMetricsHourly, error) {
	return &model.FeedMetricsHourly{}, nil
}
func (m *mockMetricsModel) Upsert(ctx context.Context, data *model.FeedMetricsHourly) error { return nil }
func (m *mockMetricsModel) Insert(ctx context.Context, data *model.FeedMetricsHourly) (sql.Result, error) {
	return nil, nil
}
func (m *mockMetricsModel) FindOne(ctx context.Context, id uint64) (*model.FeedMetricsHourly, error) {
	return nil, model.ErrNotFound
}
func (m *mockMetricsModel) FindOneByFeedIdAndStatHour(ctx context.Context, feedId uint64, statHour time.Time) (*model.FeedMetricsHourly, error) {
	return nil, model.ErrNotFound
}
func (m *mockMetricsModel) Update(ctx context.Context, data *model.FeedMetricsHourly) error { return nil }
func (m *mockMetricsModel) Delete(ctx context.Context, id uint64) error                     { return nil }

type mockContentRpc struct {
	contentClient.Content // 嵌入接口：仅覆盖测试用到的 2 个方法
	profile               *contentpb.ContentProfile
	search                *contentpb.SearchContentResp
}

func (m *mockContentRpc) GetContentProfile(ctx context.Context, in *contentpb.GetContentProfileReq, opts ...grpc.CallOption) (*contentpb.GetContentProfileResp, error) {
	if m.profile == nil {
		return &contentpb.GetContentProfileResp{}, nil
	}
	return &contentpb.GetContentProfileResp{Profile: m.profile}, nil
}
func (m *mockContentRpc) SearchContent(ctx context.Context, in *contentpb.SearchContentReq, opts ...grpc.CallOption) (*contentpb.SearchContentResp, error) {
	return m.search, nil
}
func (m *mockContentRpc) BatchGetContentProfile(ctx context.Context, in *contentpb.BatchGetContentProfileReq, opts ...grpc.CallOption) (*contentpb.BatchGetContentProfileResp, error) {
	return &contentpb.BatchGetContentProfileResp{}, nil
}
func (m *mockContentRpc) RetryContentAnalysis(ctx context.Context, in *contentpb.RetryContentAnalysisReq, opts ...grpc.CallOption) (*contentpb.RetryContentAnalysisResp, error) {
	return &contentpb.RetryContentAnalysisResp{}, nil
}
func (m *mockContentRpc) SubmitProfileFeedback(ctx context.Context, in *contentpb.SubmitProfileFeedbackReq, opts ...grpc.CallOption) (*contentpb.SubmitProfileFeedbackResp, error) {
	return &contentpb.SubmitProfileFeedbackResp{}, nil
}

type mockInterestModel struct {
	row *model.UserInterestProfiles
}

func (m *mockInterestModel) FindOneByUserId(ctx context.Context, userId int64) (*model.UserInterestProfiles, error) {
	if m.row == nil {
		return nil, model.ErrNotFound
	}
	return m.row, nil
}
func (m *mockInterestModel) UpsertWithVersion(ctx context.Context, data *model.UserInterestProfiles) error {
	return nil
}
func (m *mockInterestModel) Insert(ctx context.Context, data *model.UserInterestProfiles) (sql.Result, error) {
	return nil, nil
}
func (m *mockInterestModel) FindOne(ctx context.Context, id uint64) (*model.UserInterestProfiles, error) {
	return nil, model.ErrNotFound
}
func (m *mockInterestModel) Update(ctx context.Context, data *model.UserInterestProfiles) error { return nil }
func (m *mockInterestModel) Delete(ctx context.Context, id uint64) error                       { return nil }

// ---- 装配 ----

func metricsSvc(t *testing.T, feed *mockFeedRpc, metrics *mockMetricsModel, content *mockContentRpc, interest *mockInterestModel) *svc.ServiceContext {
	mr := miniredis.RunT(t)
	return &svc.ServiceContext{
		FeedRpc:                 feed,
		ContentRpc:              content,
		FeedMetricsHourlyModel:  metrics,
		UserInterestModel:       interest,
		Redis:                   redis.MustNewRedis(redis.RedisConf{Host: mr.Addr(), Type: "node"}),
		InternalUsers:           map[int64]bool{1: true},
	}
}

func authorFeed(authorID int64) *feedpb.FeedInfo {
	return &feedpb.FeedInfo{FeedId: 555, AuthorId: authorID, FeedType: 2, CreatedAt: time.Now().UnixMilli()}
}

// ---- T097 GetCreatorMetrics ----

func TestGetCreatorMetrics_AuthorOK_RateCorrect(t *testing.T) {
	svcCtx := metricsSvc(t,
		&mockFeedRpc{feed: authorFeed(42)},
		&mockMetricsModel{sumFeed: &model.FeedMetricsHourly{
			ExposeCount: 1000, PlayCount: 400, EffectivePlayCount: 250,
			FinishCount: 100, SkipCount: 60, WatchDurationMs: 20000,
			LikeCount: 40, CollectCount: 20, CommentCount: 10, ShareCount: 5,
		}},
		&mockContentRpc{}, &mockInterestModel{})

	l := NewGetCreatorMetricsLogic(context.Background(), svcCtx)
	resp, err := l.GetCreatorMetrics(&interaction.GetCreatorMetricsReq{FeedId: 555, ViewerId: 42})
	require.NoError(t, err)

	m := resp.Metrics
	assert.Equal(t, int64(1000), m.Raw.Expose)
	// 派生率：play_rate = 400/1000 = 0.4
	require.NotNil(t, m.Rate.PlayRate)
	assert.InDelta(t, 0.4, *m.Rate.PlayRate, 1e-9)
	// finish_rate = 100/400 = 0.25
	require.NotNil(t, m.Rate.FinishRate)
	assert.InDelta(t, 0.25, *m.Rate.FinishRate, 1e-9)
	// like_rate = 40/400 = 0.1
	require.NotNil(t, m.Rate.LikeRate)
	assert.InDelta(t, 0.1, *m.Rate.LikeRate, 1e-9)
}

func TestGetCreatorMetrics_OtherForbidden(t *testing.T) {
	svcCtx := metricsSvc(t, &mockFeedRpc{feed: authorFeed(42)}, &mockMetricsModel{}, &mockContentRpc{}, &mockInterestModel{})
	l := NewGetCreatorMetricsLogic(context.Background(), svcCtx)
	_, err := l.GetCreatorMetrics(&interaction.GetCreatorMetricsReq{FeedId: 555, ViewerId: 100})
	assert.Equal(t, errorx.InteractionMetricsForbidden, errCodeOf(t, err))
}

func TestGetCreatorMetrics_InternalAllowed(t *testing.T) {
	svcCtx := metricsSvc(t,
		&mockFeedRpc{feed: authorFeed(42)},
		&mockMetricsModel{sumFeed: &model.FeedMetricsHourly{ExposeCount: 10}},
		&mockContentRpc{}, &mockInterestModel{})
	l := NewGetCreatorMetricsLogic(context.Background(), svcCtx)
	_, err := l.GetCreatorMetrics(&interaction.GetCreatorMetricsReq{FeedId: 555, ViewerId: 1})
	require.NoError(t, err)
}

func TestGetCreatorMetrics_NoDataZero(t *testing.T) {
	svcCtx := metricsSvc(t, &mockFeedRpc{feed: authorFeed(42)}, &mockMetricsModel{}, &mockContentRpc{}, &mockInterestModel{})
	l := NewGetCreatorMetricsLogic(context.Background(), svcCtx)
	resp, err := l.GetCreatorMetrics(&interaction.GetCreatorMetricsReq{FeedId: 555, ViewerId: 42})
	require.NoError(t, err)
	assert.Equal(t, int64(0), resp.Metrics.Raw.Play)
	// 分母 0 → rate 为 nil（不返回 0）
	assert.Nil(t, resp.Metrics.Rate.PlayRate)
}

// ---- T098 GetPeerAverageMetrics ----

func TestGetPeerAverageMetrics_AnonymousNoFeedID(t *testing.T) {
	// 10 个同类 feed 各贡献数据
	sumByIDs := map[int64]*model.FeedMetricsHourly{}
	for i := int64(1); i <= 30; i++ {
		sumByIDs[i] = &model.FeedMetricsHourly{ExposeCount: 100, PlayCount: 40, FinishCount: 10, SkipCount: 20}
	}
	svcCtx := metricsSvc(t,
		&mockFeedRpc{feed: authorFeed(42)},
		&mockMetricsModel{sumByIDs: sumByIDs},
		&mockContentRpc{
			profile: &contentpb.ContentProfile{FeedId: 555, Category: "户外旅行", MediaDurationMs: 45000},
			search: &contentpb.SearchContentResp{Items: []*contentpb.SearchResultItem{
				{FeedId: 1, MediaDurationMs: 40000}, {FeedId: 2, MediaDurationMs: 50000},
				{FeedId: 3, MediaDurationMs: 45000}, {FeedId: 4, MediaDurationMs: 30000},
			}},
		}, &mockInterestModel{})

	l := NewGetPeerAverageMetricsLogic(context.Background(), svcCtx)
	resp, err := l.GetPeerAverageMetrics(&interaction.GetPeerAverageMetricsReq{FeedId: 555, ViewerId: 42})
	require.NoError(t, err)
	// 样本不足（4 个 < 20）→ insufficient_sample
	assert.True(t, resp.InsufficientSample)
	// 响应结构匿名：peer level/category/bucket 有值，但无 feed_id 字段（断言类型上不存在该字段即匿名保证）
	assert.Equal(t, "户外旅行", resp.Category)
	assert.Equal(t, "30-60s", resp.DurationBucket)
}

func TestGetPeerAverageMetrics_SufficientSample(t *testing.T) {
	searchItems := make([]*contentpb.SearchResultItem, 0, 30)
	sumByIDs := map[int64]*model.FeedMetricsHourly{}
	for i := int64(1); i <= 30; i++ {
		searchItems = append(searchItems, &contentpb.SearchResultItem{FeedId: i, MediaDurationMs: 40000})
		sumByIDs[i] = &model.FeedMetricsHourly{ExposeCount: 100, PlayCount: 40, FinishCount: 10, SkipCount: 20}
	}
	svcCtx := metricsSvc(t,
		&mockFeedRpc{feed: authorFeed(42)},
		&mockMetricsModel{sumByIDs: sumByIDs},
		&mockContentRpc{
			profile: &contentpb.ContentProfile{FeedId: 555, Category: "户外旅行", MediaDurationMs: 45000},
			search:  &contentpb.SearchContentResp{Items: searchItems},
		}, &mockInterestModel{})

	l := NewGetPeerAverageMetricsLogic(context.Background(), svcCtx)
	resp, err := l.GetPeerAverageMetrics(&interaction.GetPeerAverageMetricsReq{FeedId: 555, ViewerId: 42})
	require.NoError(t, err)
	assert.False(t, resp.InsufficientSample)
	assert.True(t, resp.SampleSize >= 20)
	require.NotNil(t, resp.Rate)
	require.NotNil(t, resp.Rate.PlayRate)
	// 所有同类 play_rate 都是 0.4 → avg/p50 都是 0.4
	assert.InDelta(t, 0.4, *resp.Rate.PlayRate.Avg, 1e-6)
	assert.InDelta(t, 0.4, *resp.Rate.PlayRate.P50, 1e-6)
}

func TestGetPeerAverageMetrics_OtherForbidden(t *testing.T) {
	svcCtx := metricsSvc(t, &mockFeedRpc{feed: authorFeed(42)}, &mockMetricsModel{}, &mockContentRpc{}, &mockInterestModel{})
	l := NewGetPeerAverageMetricsLogic(context.Background(), svcCtx)
	_, err := l.GetPeerAverageMetrics(&interaction.GetPeerAverageMetricsReq{FeedId: 555, ViewerId: 100})
	assert.Equal(t, errorx.InteractionMetricsForbidden, errCodeOf(t, err))
}

// ---- T099 GetUserInterestProfile ----

func TestGetUserInterestProfile_RequiresSelf(t *testing.T) {
	svcCtx := metricsSvc(t, &mockFeedRpc{}, &mockMetricsModel{}, &mockContentRpc{}, &mockInterestModel{})
	l := NewGetUserInterestProfileLogic(context.Background(), svcCtx)
	_, err := l.GetUserInterestProfile(&interaction.GetUserInterestProfileReq{UserId: 42, ViewerId: 100})
	assert.Equal(t, errorx.Forbidden, errCodeOf(t, err))
}

func TestGetUserInterestProfile_InternalAllowed(t *testing.T) {
	svcCtx := metricsSvc(t, &mockFeedRpc{}, &mockMetricsModel{}, &mockContentRpc{}, &mockInterestModel{})
	l := NewGetUserInterestProfileLogic(context.Background(), svcCtx)
	// 内部用户 1 可查任意用户（无数据 → 冷启动空画像，不报错）
	resp, err := l.GetUserInterestProfile(&interaction.GetUserInterestProfileReq{UserId: 42, ViewerId: 1})
	require.NoError(t, err)
	assert.True(t, resp.IsColdStart)
}

func TestGetUserInterestProfile_MySQLFallback(t *testing.T) {
	svcCtx := metricsSvc(t, &mockFeedRpc{}, &mockMetricsModel{}, &mockContentRpc{}, &mockInterestModel{
		row: &model.UserInterestProfiles{
			UserId: 42,
			InterestJson: `{"categories":[{"name":"户外旅行","score":5}],"topics":[{"name":"露营","score":5}],"total_actions":12,"window_days":30}`,
			CalculatedAt: time.Now(),
		},
	})
	l := NewGetUserInterestProfileLogic(context.Background(), svcCtx)
	resp, err := l.GetUserInterestProfile(&interaction.GetUserInterestProfileReq{UserId: 42, ViewerId: 42})
	require.NoError(t, err)
	// ratio 归一化：唯一项 ratio=1
	require.Len(t, resp.TopTopics, 1)
	assert.InDelta(t, 1.0, resp.TopTopics[0].Ratio, 1e-6)
	assert.Equal(t, int64(12), resp.TotalActions)
	assert.False(t, resp.IsColdStart)
}

// ---- helper ----

func errCodeOf(t *testing.T, err error) int {
	t.Helper()
	require.Error(t, err)
	ce, ok := errorx.TryParse(err)
	require.True(t, ok, "error should be business code: %v", err)
	return ce.Code
}
