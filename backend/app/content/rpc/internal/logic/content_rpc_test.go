// content_rpc_test.go
//
// 职责：Content RPC logic 单元测试（T067）。
// 覆盖：GetContentProfile 分级（作者全量/非作者公开/内部全量）与状态语义、
// 公开字段 cache-aside、Retry 内部用户权限、BatchGet 顺序与跳过。
// 不依赖真实 MySQL/Redis/ES/MQ（model 用 stub、Redis 用 miniredis）。
package logic

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/sponge-dad/feed/app/content/model"
	"github.com/sponge-dad/feed/app/content/rpc/content"
	"github.com/sponge-dad/feed/app/content/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

// ---- model stub（仅实现被测路径需要的方法） ----

type stubProfiles struct {
	byFeed map[int64]*model.FeedContentProfiles
}

func newStubProfiles() *stubProfiles { return &stubProfiles{byFeed: map[int64]*model.FeedContentProfiles{}} }

func (m *stubProfiles) UpsertByFeedID(ctx context.Context, data *model.FeedContentProfiles) error { return nil }
func (m *stubProfiles) UpdateStatus(ctx context.Context, feedID int64, status, errMsg string, degraded, retryCount int64) error {
	return nil
}
func (m *stubProfiles) FindOneByFeedId(ctx context.Context, feedID int64) (*model.FeedContentProfiles, error) {
	if p, ok := m.byFeed[feedID]; ok {
		return p, nil
	}
	return nil, model.ErrNotFound
}
func (m *stubProfiles) FindStuckTasks(ctx context.Context, before time.Time, limit int) ([]*model.FeedContentProfiles, error) {
	return nil, nil
}
func (m *stubProfiles) FindByCategory(ctx context.Context, category, status string, limit int) ([]*model.FeedContentProfiles, error) {
	return nil, nil
}
func (m *stubProfiles) Insert(ctx context.Context, data *model.FeedContentProfiles) (sql.Result, error) {
	return nil, nil
}
func (m *stubProfiles) FindOne(ctx context.Context, id uint64) (*model.FeedContentProfiles, error) {
	return nil, model.ErrNotFound
}
func (m *stubProfiles) Update(ctx context.Context, data *model.FeedContentProfiles) error { return nil }
func (m *stubProfiles) Delete(ctx context.Context, id uint64) error                       { return nil }

// ---- 测试装配 ----

func completedProfile(feedID, authorID int64) *model.FeedContentProfiles {
	return &model.FeedContentProfiles{
		Id:              uint64(feedID),
		FeedId:          feedID,
		AuthorId:        authorID,
		Category:        "户外旅行",
		Summary:         sql.NullString{String: "西安周边露营攻略", Valid: true},
		Topics:          sql.NullString{String: `["露营"]`, Valid: true},
		Transcript:      sql.NullString{String: "周末去露营", Valid: true},
		AnalysisStatus:  "COMPLETED",
		ModelVersion:    "v1.0.0",
		AnalyzedAt:      sql.NullTime{Time: time.Now(), Valid: true},
	}
}

// newTestSvc 构造只注入被测依赖的 svc。
func newTestSvc(t *testing.T, m model.FeedContentProfilesModel) *svc.ServiceContext {
	mr := miniredis.RunT(t)
	return &svc.ServiceContext{
		Redis:                redis.MustNewRedis(redis.RedisConf{Host: mr.Addr(), Type: "node"}),
		ContentProfilesModel: m,
		InternalUsers:        map[int64]bool{1: true}, // 内部用户 1
	}
}

func errCode(t *testing.T, err error) int {
	t.Helper()
	require.Error(t, err)
	ce, ok := errorx.TryParse(err)
	require.True(t, ok, "error 应可解析为业务码: %v", err)
	return ce.Code
}

// ---- GetContentProfile ----

func TestGetContentProfile_AuthorSeesFull(t *testing.T) {
	stub := newStubProfiles()
	stub.byFeed[555] = completedProfile(555, 42)
	sctx := newTestSvc(t, stub)

	l := NewGetContentProfileLogic(context.Background(), sctx)
	resp, err := l.GetContentProfile(&content.GetContentProfileReq{FeedId: 555, ViewerId: 42})
	require.NoError(t, err)
	assert.Equal(t, "周末去露营", resp.Profile.Transcript) // 作者全量
	assert.Equal(t, int32(content.AnalysisStatus_COMPLETED), int32(resp.Profile.AnalysisStatus))
}

func TestGetContentProfile_OtherSeesPublicOnly(t *testing.T) {
	stub := newStubProfiles()
	stub.byFeed[555] = completedProfile(555, 42)
	sctx := newTestSvc(t, stub)

	l := NewGetContentProfileLogic(context.Background(), sctx)
	resp, err := l.GetContentProfile(&content.GetContentProfileReq{FeedId: 555, ViewerId: 100})
	require.NoError(t, err)
	assert.Equal(t, "", resp.Profile.Transcript) // 非作者隐藏字幕
	assert.Equal(t, "户外旅行", resp.Profile.Category)
	assert.Equal(t, []string{"露营"}, resp.Profile.Topics)
}

func TestGetContentProfile_InternalSeesFull(t *testing.T) {
	stub := newStubProfiles()
	stub.byFeed[555] = completedProfile(555, 42)
	sctx := newTestSvc(t, stub)

	l := NewGetContentProfileLogic(context.Background(), sctx)
	resp, err := l.GetContentProfile(&content.GetContentProfileReq{FeedId: 555, ViewerId: 1}) // 内部用户
	require.NoError(t, err)
	assert.Equal(t, "周末去露营", resp.Profile.Transcript)
}

func TestGetContentProfile_StatusSemantics(t *testing.T) {
	stub := newStubProfiles()
	run := completedProfile(555, 42)
	run.AnalysisStatus = "ASR_RUNNING"
	stub.byFeed[555] = run
	sctx := newTestSvc(t, stub)

	l := NewGetContentProfileLogic(context.Background(), sctx)
	_, err := l.GetContentProfile(&content.GetContentProfileReq{FeedId: 555, ViewerId: 100})
	assert.Equal(t, errorx.ContentAnalysisRunning, errCode(t, err), "运行中应返回 15002")
}

func TestGetContentProfile_NotFound(t *testing.T) {
	stub := newStubProfiles()
	sctx := newTestSvc(t, stub)

	l := NewGetContentProfileLogic(context.Background(), sctx)
	_, err := l.GetContentProfile(&content.GetContentProfileReq{FeedId: 999, ViewerId: 100})
	assert.Equal(t, errorx.ContentProfileNotFound, errCode(t, err), "不存在应返回 15001")
}

func TestGetContentProfile_CacheAside(t *testing.T) {
	stub := newStubProfiles()
	stub.byFeed[555] = completedProfile(555, 42)
	sctx := newTestSvc(t, stub)
	l := NewGetContentProfileLogic(context.Background(), sctx)

	// 第一次非作者查询：写公开字段缓存
	_, err := l.GetContentProfile(&content.GetContentProfileReq{FeedId: 555, ViewerId: 100})
	require.NoError(t, err)

	// 篡改 DB 数据后再次查询：应命中缓存返回旧值（证明走了 cache-aside）
	changed := completedProfile(555, 42)
	changed.Category = "已修改"
	stub.byFeed[555] = changed
	resp, err := l.GetContentProfile(&content.GetContentProfileReq{FeedId: 555, ViewerId: 100})
	require.NoError(t, err)
	assert.Equal(t, "户外旅行", resp.Profile.Category, "非作者应命中缓存而非读库")
}

// ---- BatchGet ----

func TestBatchGetContentProfile_OrderAndSkip(t *testing.T) {
	stub := newStubProfiles()
	stub.byFeed[1] = completedProfile(1, 42)
	stub.byFeed[2] = completedProfile(2, 42)
	stub.byFeed[3] = completedProfile(3, 42)
	stub.byFeed[3].AnalysisStatus = "PENDING" // 进行中 → 跳过
	// 4 不存在 → 跳过
	sctx := newTestSvc(t, stub)

	l := NewBatchGetContentProfileLogic(context.Background(), sctx)
	resp, err := l.BatchGetContentProfile(&content.BatchGetContentProfileReq{FeedIds: []int64{1, 2, 3, 4}})
	require.NoError(t, err)
	// 仅 1、2 返回，顺序保持
	require.Len(t, resp.Profiles, 2)
	assert.Equal(t, int64(1), resp.Profiles[0].FeedId)
	assert.Equal(t, int64(2), resp.Profiles[1].FeedId)
}

func TestBatchGetContentProfile_TooMany(t *testing.T) {
	stub := newStubProfiles()
	sctx := newTestSvc(t, stub)
	l := NewBatchGetContentProfileLogic(context.Background(), sctx)
	ids := make([]int64, 51)
	_, err := l.BatchGetContentProfile(&content.BatchGetContentProfileReq{FeedIds: ids})
	assert.Equal(t, errorx.ParamError, errCode(t, err), "超过 50 应返回参数错误")
}

// ---- Retry ----

func TestRetryContentAnalysis_RequiresInternal(t *testing.T) {
	stub := newStubProfiles()
	sctx := newTestSvc(t, stub)
	l := NewRetryContentAnalysisLogic(context.Background(), sctx)

	_, err := l.RetryContentAnalysis(&content.RetryContentAnalysisReq{FeedId: 555, OperatorId: 100})
	assert.Equal(t, errorx.Forbidden, errCode(t, err), "非内部用户应被拒绝")
}
