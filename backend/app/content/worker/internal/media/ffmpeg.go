package media

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// FFmpeg FFmpeg 安全调用封装（6 层防护见 research.md R7）。
//
//  1. 固定二进制路径：路径由配置注入，不从 PATH 查找
//  2. 参数数组传递：经 Executor.Run，禁止 shell 拼接
//  3. 媒体下载 URL：AllowedMediaHosts 白名单 + 拒绝内网地址（见 ValidateMediaURL）
//  4. 超时强杀：Executor 内部 context.WithTimeout + kill 进程组
//  5. 资源上限：MaxVideoBytes / MaxVideoDurationSec 由调用方校验
//  6. 独立临时子目录：每任务 TaskDir，完成后整目录清理
type FFmpeg struct {
	Path     string
	Timeout  time.Duration
	Exec     Executor
	MaxFrames int
}

// ExtractAudio 提取 16kHz 单声道 wav（多数 ASR 服务的推荐输入）。
func (f *FFmpeg) ExtractAudio(ctx context.Context, input, output string) error {
	if f.Exec == nil {
		f.Exec = OSExecutor{Timeout: f.Timeout}
	}
	_, err := f.Exec.Run(ctx, f.Path,
		"-nostdin", "-y", "-i", input, "-vn", "-ac", "1", "-ar", "16000", "-f", "wav", output)
	return err
}

// ExtractKeyFrames 抽取关键帧：
//  1. 场景切换帧（select scene > 0.3）
//  2. 不足 max 时用固定间隔 fps=1/3 兜底
//  3. 合并后按时间均匀采样裁剪到 max，首帧必须保留
//
// 返回帧文件路径列表（按文件名排序）。
func (f *FFmpeg) ExtractKeyFrames(ctx context.Context, input, dir string, max int) ([]string, error) {
	if f.Exec == nil {
		f.Exec = OSExecutor{Timeout: f.Timeout}
	}
	if max <= 0 {
		max = f.MaxFrames
	}
	if max <= 0 {
		max = 20
	}

	// 场景切换帧（失败不中断：记录日志后降级到固定间隔兜底）。
	sceneDir := filepath.Join(dir, "scene")
	if err := os.MkdirAll(sceneDir, 0o755); err != nil {
		return nil, err
	}
	if _, serr := f.Exec.Run(ctx, f.Path,
		"-nostdin", "-y", "-i", input,
		"-vf", "select='gt(scene,0.3)',scale=720:-1", "-vsync", "vfr",
		filepath.Join(sceneDir, "frame_%03d.jpg")); serr != nil {
		logx.Errorf("scene frame extract failed, fallback to interval sampling: %v", serr)
	}

	frames := listJPG(sceneDir)

	// 不足则固定间隔兜底。
	if len(frames) < max {
		fallbackDir := filepath.Join(dir, "fallback")
		if err := os.MkdirAll(fallbackDir, 0o755); err != nil {
			return nil, err
		}
		if _, ferr := f.Exec.Run(ctx, f.Path,
			"-nostdin", "-y", "-i", input,
			"-vf", "fps=1/3,scale=720:-1",
			filepath.Join(fallbackDir, "fallback_%03d.jpg")); ferr != nil {
			// 兜底失败且已有部分帧时降级返回；无任何帧时报错。
			logx.Errorf("fallback frame extract failed: %v", ferr)
		}
		frames = mergeUnique(frames, listJPG(fallbackDir))
	}

	if len(frames) == 0 {
		return nil, fmt.Errorf("no key frames extracted from %s", input)
	}

	// 按时间均匀采样裁剪到 max（首帧必须保留）
	sort.Strings(frames)
	selected := sampleFrames(frames, max)
	return selected, nil
}

// listJPG 列出目录下所有 .jpg 文件（完整路径）。
func listJPG(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(strings.ToLower(e.Name()), ".jpg") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

// mergeUnique 合并并去重（按完整路径）。
func mergeUnique(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, s := range append(a, b...) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// sampleFrames 均匀采样到 max 张，首帧必留。
func sampleFrames(frames []string, max int) []string {
	if len(frames) <= max {
		return frames
	}
	// 步长采样，保证覆盖首尾
	step := float64(len(frames)-1) / float64(max-1)
	idxSet := make(map[int]bool)
	for i := 0; i < max; i++ {
		idxSet[int(float64(i)*step+0.5)] = true
	}
	out := make([]string, 0, max)
	for i, f := range frames {
		if idxSet[i] {
			out = append(out, f)
		}
	}
	// 兜底：不足 max 时补前面的
	for _, f := range frames {
		if len(out) >= max {
			break
		}
		has := false
		for _, o := range out {
			if o == f {
				has = true
				break
			}
		}
		if !has {
			out = append(out, f)
		}
	}
	return out
}


