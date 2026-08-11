// pipeline_test.go
//
// 职责：内容分析流水线的单元测试（T065）。
// 覆盖：全链路成功 → COMPLETED；ASR 失败 → degraded=1 仍 COMPLETED；
// 下载失败 → FAILED；非白名单类目映射为「其他」。
//
// 依赖注入：model 用 stub、FFmpeg 用 fake、媒体用 httptest、ASR/OCR/vision 用 fake，
// 不依赖真实 MySQL / Redis / MQ / FFmpeg / 外部 AI 服务（14-acceptance-test.md：CI 必须 mock）。
package pipeline

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sponge-dad/feed/app/content/model"
	"github.com/sponge-dad/feed/app/content/search"
	"github.com/sponge-dad/feed/app/content/worker/internal/asr"
	"github.com/sponge-dad/feed/app/content/worker/internal/config"
	"github.com/sponge-dad/feed/app/content/worker/internal/media"
	"github.com/sponge-dad/feed/app/content/worker/internal/ocr"
	"github.com/sponge-dad/feed/app/content/worker/internal/vision"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/logx"
)

// ---- model stub（实现 FeedContentProfilesModel 接口） ----

type updateCall struct {
	feedID     int64
	status     string
	errMsg     string
	degraded   int64
	retryCount int64
}

type stubModel struct {
	upserts []*model.FeedContentProfiles
	updates []updateCall
}

func (m *stubModel) UpsertByFeedID(ctx context.Context, data *model.FeedContentProfiles) error {
	cp := *data // 深拷贝：pipeline 复用同一 profile，避免中间状态被最终状态覆盖
	m.upserts = append(m.upserts, &cp)
	return nil
}
func (m *stubModel) UpdateStatus(ctx context.Context, feedID int64, status, errMsg string, degraded, retryCount int64) error {
	m.updates = append(m.updates, updateCall{feedID: feedID, status: status, errMsg: errMsg, degraded: degraded, retryCount: retryCount})
	return nil
}
func (m *stubModel) FindOneByFeedId(ctx context.Context, feedID int64) (*model.FeedContentProfiles, error) {
	return nil, model.ErrNotFound
}
func (m *stubModel) FindStuckTasks(ctx context.Context, before time.Time, limit int) ([]*model.FeedContentProfiles, error) {
	return nil, nil
}
func (m *stubModel) FindByCategory(ctx context.Context, category, status string, limit int) ([]*model.FeedContentProfiles, error) {
	return nil, nil
}
func (m *stubModel) Insert(ctx context.Context, data *model.FeedContentProfiles) (sql.Result, error) {
	return nil, nil
}
func (m *stubModel) FindOne(ctx context.Context, id uint64) (*model.FeedContentProfiles, error) {
	return nil, model.ErrNotFound
}
func (m *stubModel) Update(ctx context.Context, data *model.FeedContentProfiles) error { return nil }
func (m *stubModel) Delete(ctx context.Context, id uint64) error                       { return nil }

// ---- ffmpeg fake（模拟抽帧/音频生成） ----

type fakeFFmpeg struct {
	calls [][]string
	fail  map[string]bool // 命令包含该子串 → 返回错误
}

func (f *fakeFFmpeg) Run(ctx context.Context, bin string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, args)
	// ffprobe 探测：返回固定时长信息
	if strings.Contains(bin, "ffprobe") {
		return []byte(`{"format":{"duration":"12.5"},"streams":[{"codec_type":"video","codec_name":"h264","width":1280,"height":720}]}`), nil
	}
	cmd := strings.Join(args, " ")
	for sub := range f.fail {
		if strings.Contains(cmd, sub) {
			return nil, errFake
		}
	}
	if strings.Contains(cmd, "-vf") {
		for _, a := range args {
			if strings.Contains(a, "%03d") {
				dir := filepath.Dir(a)
				base := filepath.Base(a)
				_ = writeFakeFile(filepath.Join(dir, strings.Replace(base, "%03d", "001", 1)))
				_ = writeFakeFile(filepath.Join(dir, strings.Replace(base, "%03d", "002", 1)))
			}
		}
	}
	if strings.Contains(cmd, "wav") && strings.Contains(cmd, "audio") && !strings.Contains(cmd, "-i") {
		_ = writeFakeFile(args[len(args)-1])
	}
	return nil, nil
}

type fakeErr struct{}

func (e *fakeErr) Error() string { return "fake error" }

var errFake = &fakeErr{}

func writeFakeFile(p string) error {
	return os.WriteFile(p, []byte("x"), 0o644)
}

// ---- 测试装配 ----

type pipelineFixture struct {
	pipe     *Pipeline
	model    *stubModel
	asr      *asr.FakeClient
	ocr      *ocr.FakeClient
	vision   *vision.FakeClient
	mediaSrv *httptest.Server
	ffmpeg   *fakeFFmpeg
}

func newFixture(t *testing.T, fail map[string]bool) *pipelineFixture {
	mediaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/boom" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte("fake-video-bytes"))
	}))
	t.Cleanup(mediaSrv.Close)

	esSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// go-elasticsearch 客户端的产品校验要求该响应头。
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		_, _ = w.Write([]byte(`{"acknowledged":true,"result":"created"}`))
	}))
	t.Cleanup(esSrv.Close)

	es, err := search.NewClient(esSrv.URL, "feed_content", "feed_content_write")
	require.NoError(t, err)

	cfg := &config.Config{}
	cfg.Fill()
	cfg.Media.FFmpegPath = "/fake/ffmpeg"
	cfg.Media.FFprobePath = "/fake/ffprobe"
	cfg.Media.TempDir = t.TempDir()
	cfg.Media.AllowedMediaHosts = []string{"127.0.0.1"}
	cfg.Media.FFmpegTimeoutSec = 5

	fx := &pipelineFixture{
		model:    &stubModel{},
		mediaSrv: mediaSrv,
		asr:      &asr.FakeClient{Result: &asr.Result{Segments: []asr.Segment{{StartMs: 0, EndMs: 1500, Text: "周末去露营"}}, Language: "zh-CN"}},
		ocr:      &ocr.FakeClient{Items: []string{"营地入口"}},
		vision:   &vision.FakeClient{},
		ffmpeg:   &fakeFFmpeg{fail: fail},
	}

	ff := &media.FFmpeg{Path: cfg.Media.FFmpegPath, Exec: fx.ffmpeg, MaxFrames: 5}
	// 单测注入普通 Client：绕过安全 DialContext（SSRF/DNS 校验由 media 包单测覆盖）。
	dl := &media.Downloader{
		Client:      &http.Client{},
		ValidateURL: func(raw string, allowed []string) error { return nil },
	}
	fx.pipe = New(cfg, fx.model, es, ff, dl, fx.asr, fx.ocr, fx.vision, logx.WithContext(context.Background()))
	return fx
}

func (fx *pipelineFixture) task() *Task {
	return &Task{
		FeedID:    555001,
		AuthorID:  42,
		FeedType:  2,
		MediaURL:  fx.mediaSrv.URL + "/v.mp4",
		Title:     "西安周边露营地推荐",
		Desc:      "周末带娃去露营",
		CityCode:  "440300",
		CityName:  "深圳",
		CreatedAt: time.Now().UnixMilli(),
	}
}

// ---- 用例 ----

func TestPipeline_FullSuccess(t *testing.T) {
	fx := newFixture(t, nil)
	err := fx.pipe.Run(context.Background(), fx.task())
	require.NoError(t, err)

	last := fx.model.upserts[len(fx.model.upserts)-1]
	assert.Equal(t, "COMPLETED", last.AnalysisStatus)
	assert.Equal(t, int64(0), last.Degraded)
	assert.Equal(t, "zh-CN", last.Language)
	assert.Contains(t, last.Transcript.String, "露营")
	assert.Equal(t, "户外旅行", last.Category) // fake vision 默认类目在白名单内
	assert.NotEmpty(t, last.MediaHash)
	assert.True(t, last.AnalyzedAt.Valid)

	statuses := statusesOf(fx.model.upserts)
	assert.Contains(t, statuses, "DOWNLOADING")
	assert.Contains(t, statuses, "EXTRACTING")
	assert.Contains(t, statuses, "ASR_RUNNING")
	assert.Contains(t, statuses, "VISION_RUNNING")
	assert.Contains(t, statuses, "COMPLETED")
}

func TestPipeline_ASRFailureDegraded(t *testing.T) {
	fx := newFixture(t, nil)
	fx.asr.Err = errFake
	err := fx.pipe.Run(context.Background(), fx.task())
	require.NoError(t, err)

	last := fx.model.upserts[len(fx.model.upserts)-1]
	assert.Equal(t, "COMPLETED", last.AnalysisStatus)
	assert.Equal(t, int64(1), last.Degraded) // 部分失败降级
	assert.Contains(t, last.ErrorMessage, "asr")
	assert.Equal(t, "", last.Transcript.String)
}

func TestPipeline_ExtractAudioFailureFailed(t *testing.T) {
	fx := newFixture(t, map[string]bool{"-f wav": true})
	err := fx.pipe.Run(context.Background(), fx.task())
	require.Error(t, err)
	lastUpdate := fx.model.updates[len(fx.model.updates)-1]
	assert.Equal(t, "FAILED", lastUpdate.status)
}

func TestPipeline_DownloadFailureFailed(t *testing.T) {
	fx := newFixture(t, nil)
	task := fx.task()
	task.MediaURL = fx.mediaSrv.URL + "/boom"
	err := fx.pipe.Run(context.Background(), task)
	require.Error(t, err)
	lastUpdate := fx.model.updates[len(fx.model.updates)-1]
	assert.Equal(t, "FAILED", lastUpdate.status)
}

func TestPipeline_CategoryMappedToOther(t *testing.T) {
	fx := newFixture(t, nil)
	fx.vision.Result = &vision.Result{
		Category: "不存在的类目",
		Summary:  "测试摘要",
		Topics:   []string{"测试"},
	}
	err := fx.pipe.Run(context.Background(), fx.task())
	require.NoError(t, err)
	last := fx.model.upserts[len(fx.model.upserts)-1]
	assert.Equal(t, "其他", last.Category)
}

// ---- 辅助 ----

func statusesOf(upserts []*model.FeedContentProfiles) []string {
	var out []string
	for _, u := range upserts {
		out = append(out, u.AnalysisStatus)
	}
	return out
}
