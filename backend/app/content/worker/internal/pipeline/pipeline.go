// Package pipeline 内容分析流水线（状态机）。
//
// 状态流转（docs/design/agent/04-content-analysis.md §3）：
//
//	PENDING ─▶ DOWNLOADING ─▶ EXTRACTING ─▶ ASR_RUNNING ─▶ OCR_RUNNING
//	                                              │
//	                                            ┌──┘
//	                                            ▼
//	                                 VISION_RUNNING ─▶ INDEXING ─▶ COMPLETED
//
// 部分成功策略：ASR/OCR/多模态单步失败不整单失败——记录 error_message（脱敏），
// 跳过该字段继续后续阶段，最终 COMPLETED + degraded=1；
// 只有下载/抽帧失败（无任何可分析素材）才判 FAILED。
package pipeline

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sponge-dad/feed/app/content/model"
	"github.com/sponge-dad/feed/app/content/search"
	"github.com/sponge-dad/feed/app/content/worker/internal/asr"
	"github.com/sponge-dad/feed/app/content/worker/internal/config"
	"github.com/sponge-dad/feed/app/content/worker/internal/media"
	"github.com/sponge-dad/feed/app/content/worker/internal/ocr"
	"github.com/sponge-dad/feed/app/content/worker/internal/vision"

	"github.com/zeromicro/go-zero/core/logx"
)

// 分析状态（与 feed_content_profiles.analysis_status 一致）。
const (
	statusPending   = "PENDING"
	statusDownload  = "DOWNLOADING"
	statusExtract   = "EXTRACTING"
	statusASR       = "ASR_RUNNING"
	statusOCR       = "OCR_RUNNING"
	statusVision    = "VISION_RUNNING"
	statusIndexing  = "INDEXING"
	statusCompleted = "COMPLETED"
	statusFailed    = "FAILED"
)

// Task 一次分析任务（来自 feed-created 事件 + Feed RPC 详情）。
type Task struct {
	FeedID    int64
	AuthorID  int64
	FeedType  int32
	MediaURL  string
	Title     string
	Desc      string
	CityCode  string
	CityName  string
	CreatedAt int64 // unix ms
}

// Pipeline 分析流水线。
type Pipeline struct {
	cfg    *config.Config
	model  model.FeedContentProfilesModel
	es     *search.Client
	ffmpeg *media.FFmpeg
	dl     *media.Downloader
	asr    asr.Client
	ocr    ocr.Client
	vision vision.Client
	log    logx.Logger
}

// New 创建流水线。
func New(cfg *config.Config, m model.FeedContentProfilesModel, es *search.Client,
	ffmpeg *media.FFmpeg, dl *media.Downloader, asrClient asr.Client, ocrClient ocr.Client,
	visionClient vision.Client, logger logx.Logger) *Pipeline {
	return &Pipeline{
		cfg:    cfg,
		model:  m,
		es:     es,
		ffmpeg: ffmpeg,
		dl:     dl,
		asr:    asrClient,
		ocr:    ocrClient,
		vision: visionClient,
		log:    logger,
	}
}

// Run 执行完整分析流水线。返回 error 时由 consumer 触发 MQ 重投（任务级重试）。
func (p *Pipeline) Run(ctx context.Context, t *Task) error {
	dir := filepath.Join(p.cfg.Media.TempDir, strconv.FormatInt(t.FeedID, 10))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// 任务结束（含失败）整目录清理（安全约束 ⑥）。
	defer os.RemoveAll(dir)

	now := time.Now()
	profile := &model.FeedContentProfiles{
		FeedId:        t.FeedID,
		AuthorId:      t.AuthorID,
		AnalysisStatus: statusPending,
		ModelVersion:  p.cfg.Media.ModelVersion,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	// ---- DOWNLOADING：下载媒体（SSRF 白名单 + 大小上限）----
	profile.AnalysisStatus = statusDownload
	if err := p.persist(ctx, profile); err != nil {
		return err
	}
	videoPath := filepath.Join(dir, "media"+extFromURL(t.MediaURL))
	if _, err := p.dl.Download(ctx, t.MediaURL, videoPath, p.cfg.Media.AllowedMediaHosts); err != nil {
		return p.fail(ctx, t.FeedID, err)
	}
	profile.MediaHash = hashFile(videoPath)

	// ---- EXTRACTING：ffprobe 探测真实时长 + 提取音频/抽帧 ----
	profile.AnalysisStatus = statusExtract
	if err := p.persist(ctx, profile); err != nil {
		return err
	}
	probe, err := media.Probe(ctx, p.cfg.Media.FFprobePath, p.ffmpeg.Exec, videoPath)
	if err != nil {
		return p.fail(ctx, t.FeedID, err)
	}
	if probe.DurationMs > p.cfg.Media.MaxVideoDurationSec*1000 {
		// 不可恢复错误：时长超限直接 FAILED 并 ACK（failTerminal 不触发 MQ 重投，
		// 否则超长视频会反复重跑昂贵流水线直至重试上限）。
		return p.failTerminal(ctx, t.FeedID, fmt.Errorf("video_too_long duration_ms=%d", probe.DurationMs))
	}
	profile.MediaDurationMs = probe.DurationMs
	profile.KeyFrameCount = 0

	audioPath := filepath.Join(dir, "audio.wav")
	if err := p.ffmpeg.ExtractAudio(ctx, videoPath, audioPath); err != nil {
		// 无音频素材（纯视频/损坏）→ FAILED。
		return p.fail(ctx, t.FeedID, fmt.Errorf("extract_audio: %w", err))
	}
	frames, err := p.ffmpeg.ExtractKeyFrames(ctx, videoPath, dir, p.cfg.Media.KeyFrameMax)
	if err != nil {
		// 抽帧失败 → 无任何可分析素材 → FAILED。
		return p.fail(ctx, t.FeedID, fmt.Errorf("extract_keyframes: %w", err))
	}
	profile.KeyFrameCount = int64(len(frames))

	degraded := false
	var errMsgParts []string

	// ---- ASR_RUNNING：语音转文字（失败 → degraded，继续）----
	profile.AnalysisStatus = statusASR
	if err := p.persist(ctx, profile); err != nil {
		return err
	}
	if asrRes, aerr := p.retryCall(ctx, func(ctx context.Context) (any, error) {
		return p.asr.Transcribe(ctx, audioPath)
	}); aerr != nil {
		degraded = true
		errMsgParts = append(errMsgParts, "asr:"+safeErr(aerr))
	} else if r, ok := asrRes.(*asr.Result); ok {
		profile.Language = r.Language
		if !r.NoSpeech && len(r.Segments) > 0 {
			segs, _ := json.Marshal(r.Segments)
			profile.TranscriptSegments = sql.NullString{String: string(segs), Valid: true}
			profile.Transcript = sql.NullString{
				String: truncateTranscript(joinSegments(r.Segments), p.cfg.Media.TranscriptMaxChars),
				Valid:  true,
			}
		}
	}

	// ---- OCR_RUNNING：关键帧文字识别（失败 → degraded，继续）----
	profile.AnalysisStatus = statusOCR
	if err := p.persist(ctx, profile); err != nil {
		return err
	}
	if ocrRes, oerr := p.retryCall(ctx, func(ctx context.Context) (any, error) {
		return p.ocr.Recognize(ctx, frames)
	}); oerr != nil {
		degraded = true
		errMsgParts = append(errMsgParts, "ocr:"+safeErr(oerr))
	} else if items, ok := ocrRes.([]string); ok && len(items) > 0 {
		txt, _ := json.Marshal(items)
		profile.OcrText = sql.NullString{String: string(txt), Valid: true}
	}

	// ---- VISION_RUNNING：多模态摘要与标签（失败 → degraded，继续）----
	profile.AnalysisStatus = statusVision
	if err := p.persist(ctx, profile); err != nil {
		return err
	}
	visIn := vision.Input{
		Title:       truncateRunes(t.Title, 200),
		Description: truncateRunes(t.Desc, 200),
		Transcript:  profile.Transcript.String,
		OcrText:     parseStringArr(profile.OcrText),
		KeyFrames:   sampleKeyFrames(frames, 8),
	}
	if visRes, verr := p.retryCall(ctx, func(ctx context.Context) (any, error) {
		return p.vision.Analyze(ctx, visIn)
	}); verr != nil {
		degraded = true
		errMsgParts = append(errMsgParts, "vision:"+safeErr(verr))
	} else if r, ok := visRes.(*vision.Result); ok {
		profile.Category = p.mapCategory(r.Category)
		profile.Summary = sql.NullString{String: sanitizeSummary(r.Summary), Valid: true}
		profile.Topics = jsonArr(r.Topics)
		profile.Objects = jsonArr(r.Objects)
		profile.Scenes = jsonArr(r.Scenes)
		profile.Styles = jsonArr(r.Styles)
	}

	// ---- INDEXING：写 ES 检索索引（失败不重跑昂贵阶段）----
	profile.AnalysisStatus = statusIndexing
	if err := p.persist(ctx, profile); err != nil {
		return err
	}
	if derr := p.es.IndexProfile(ctx, p.buildDoc(t, profile)); derr != nil {
		// 索引失败不重跑昂贵阶段：置 FAILED 并 ACK（人工重试接口可恢复）。
		return p.failTerminal(ctx, t.FeedID, fmt.Errorf("es_index: %w", derr))
	}

	// ---- COMPLETED ----
	profile.AnalysisStatus = statusCompleted
	profile.Degraded = boolToInt(degraded)
	profile.ErrorMessage = strings.Join(errMsgParts, " | ")
	profile.AnalyzedAt = sql.NullTime{Time: time.Now(), Valid: true}
	if err := p.persist(ctx, profile); err != nil {
		return err
	}
	p.log.Infof("analysis completed feed_id=%d degraded=%v duration_ms=%d frames=%d",
		t.FeedID, degraded, profile.MediaDurationMs, len(frames))
	return nil
}

// persist 全量 upsert 画像（含状态流转）。
func (p *Pipeline) persist(ctx context.Context, profile *model.FeedContentProfiles) error {
	profile.UpdatedAt = time.Now()
	return p.model.UpsertByFeedID(ctx, profile)
}

// fail 整单失败：脱敏 error_message，置 FAILED。返回该错误（consumer 据此触发 MQ 重投）。
func (p *Pipeline) fail(ctx context.Context, feedID int64, err error) error {
	msg := safeErr(err)
	_ = p.model.UpdateStatus(ctx, feedID, statusFailed, msg, 0, 0)
	p.log.Errorf("analysis failed feed_id=%d err=%s", feedID, msg)
	return err
}

// failTerminal 整单失败（不可恢复）：置 FAILED 但返回 nil，让 consumer ACK 不触发 MQ 重投。
// 用于视频超长、ES 索引失败等「重跑昂贵流水线无意义」的永久性失败。
func (p *Pipeline) failTerminal(ctx context.Context, feedID int64, err error) error {
	msg := safeErr(err)
	_ = p.model.UpdateStatus(ctx, feedID, statusFailed, msg, 0, 0)
	p.log.Errorf("analysis failed(terminal) feed_id=%d err=%s", feedID, msg)
	return nil
}

// retryCall 外部服务调用重试（指数退避 1s、4s，共 3 次尝试）。
func (p *Pipeline) retryCall(ctx context.Context, fn func(ctx context.Context) (any, error)) (any, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		res, err := fn(ctx)
		if err == nil {
			return res, nil
		}
		lastErr = err
		if attempt < 2 {
			time.Sleep(time.Duration(1<<uint(attempt)) * time.Second)
		}
	}
	return nil, lastErr
}

// mapCategory 类目白名单映射：不在白名单 → 「其他」。
func (p *Pipeline) mapCategory(c string) string {
	c = strings.TrimSpace(c)
	for _, w := range p.cfg.Media.CategoryWhitelist {
		if c == w {
			return c
		}
	}
	return "其他"
}

// buildDoc 组装 ES 索引文档。
func (p *Pipeline) buildDoc(t *Task, profile *model.FeedContentProfiles) *search.Document {
	return &search.Document{
		FeedID:          t.FeedID,
		AuthorID:        t.AuthorID,
		FeedType:        int8(t.FeedType),
		Status:          1, // 索引时业务过滤由 SearchContent 回查，这里记 NORMAL
		Title:           t.Title,
		Description:     t.Desc,
		Summary:         profile.Summary.String,
		Transcript:      profile.Transcript.String,
		OcrText:         strings.Join(parseStringArr(profile.OcrText), "\n"),
		Category:        profile.Category,
		Topics:          parseStringArr(profile.Topics),
		Scenes:          parseStringArr(profile.Scenes),
		Objects:         parseStringArr(profile.Objects),
		Styles:          parseStringArr(profile.Styles),
		CityCode:        t.CityCode,
		CityName:        t.CityName,
		Language:        profile.Language,
		MediaDurationMs: profile.MediaDurationMs,
		PublishedAt:     t.CreatedAt,
		LikeCount:       0,
		CollectCount:    0,
	}
}

// ---------------- 工具函数 ----------------

// hashFile 计算文件 SHA-256（取前 32 字节 hex 作为 media_hash）。
func hashFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, 1<<16)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

// extFromURL 从 URL 提取扩展名（如 .mp4）。
func extFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ".mp4"
	}
	p := u.Path
	ext := filepath.Ext(p)
	if ext == "" {
		return ".mp4"
	}
	if len(ext) > 8 {
		return ".mp4"
	}
	return ext
}

// joinSegments 拼接分段字幕为全文。
func joinSegments(segs []asr.Segment) string {
	var b strings.Builder
	for _, s := range segs {
		if strings.TrimSpace(s.Text) != "" {
			b.WriteString(s.Text)
		}
	}
	return b.String()
}

// truncateTranscript 按 rune 截断字幕全文（超长取「开头 60% + 结尾 20%」）。
func truncateTranscript(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	head := runes[:int(float64(max)*0.6)]
	tail := runes[len(runes)-int(float64(max)*0.2):]
	return string(head) + "…" + string(tail)
}

// truncateRunes 按 rune 截断。
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// sanitizeSummary 摘要清洗：去除换行、控制字符；长度 20~200 字（过短保留原文，过长截断）。
func sanitizeSummary(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' || r < 32 {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	return truncateRunes(s, 200)
}

// jsonArr 将 []string 序列化为 JSON 字符串列。
func jsonArr(items []string) sql.NullString {
	items = sanitizeTags(items)
	if len(items) == 0 {
		return sql.NullString{}
	}
	b, _ := json.Marshal(items)
	return sql.NullString{String: string(b), Valid: true}
}

// sanitizeTags 标签清洗：去重、去纯符号/纯数字、单条截断 20 字符、上限 10 条。
func sanitizeTags(items []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, it := range items {
		s := strings.TrimSpace(it)
		if s == "" {
			continue
		}
		rs := []rune(s)
		if len(rs) > 20 {
			s = string(rs[:20])
		}
		if isGarbageTag(s) || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
		if len(out) >= 10 {
			break
		}
	}
	return out
}

var nonWord = regexp.MustCompile(`^[\W\d_]+$`)

// isGarbageTag 纯符号/纯数字/无实质内容。
func isGarbageTag(s string) bool {
	return nonWord.MatchString(s)
}

// parseStringArr 解析 JSON 数组字符串列。
func parseStringArr(col sql.NullString) []string {
	if !col.Valid || col.String == "" {
		return nil
	}
	var arr []string
	if err := json.Unmarshal([]byte(col.String), &arr); err != nil {
		return nil
	}
	return arr
}

// sampleKeyFrames 均匀采样 ≤max 张关键帧（首帧必送）。
func sampleKeyFrames(frames []string, max int) []string {
	if len(frames) <= max {
		return frames
	}
	step := float64(len(frames)-1) / float64(max-1)
	seen := make(map[int]bool)
	for i := 0; i < max; i++ {
		seen[int(float64(i)*step+0.5)] = true
	}
	out := make([]string, 0, max)
	for i, f := range frames {
		if seen[i] {
			out = append(out, f)
		}
	}
	return out
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

var urlRe = regexp.MustCompile(`https?://[^\s]+`)

// safeErr 错误信息脱敏（T091）：禁止写入含临时凭证的 COS 签名地址，
// URL 一律替换为 `[url:host]`。
func safeErr(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if len(msg) > 1024 {
		msg = msg[:1024]
	}
	// 去掉 URL 的 query（签名参数都在 query），只保留 host
	return urlRe.ReplaceAllStringFunc(msg, func(u string) string {
		if parsed, perr := url.Parse(u); perr == nil {
			return "[url:" + parsed.Host + "]"
		}
		return "[url]"
	})
}
