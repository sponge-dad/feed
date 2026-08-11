package media

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeExecutor 记录调用参数，可按需生成文件或返回输出。
type fakeExecutor struct {
	calls   [][]string // 每次调用的 args
	output  []byte
	err     error
	timeout time.Duration
	// genFrames 为 true 时在 ExtractKeyFrames 的 -vf 调用时生成帧文件（模拟抽帧输出）。
	genFrames bool
}

func (f *fakeExecutor) Run(ctx context.Context, bin string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, args)
	if f.timeout > 0 {
		time.Sleep(f.timeout)
	}
	if f.genFrames && contains(args, "-vf") {
		// 模拟场景切换抽帧：在输出模板目录生成两个 jpg
		generateFrames(args)
	}
	return f.output, f.err
}

func contains(args []string, s string) bool {
	for _, a := range args {
		if a == s {
			return true
		}
	}
	return false
}

// generateFrames 从 args 中解析输出模板（如 dir/frame_%03d.jpg）并生成两个文件。
func generateFrames(args []string) {
	for _, a := range args {
		if strings.Contains(a, "%03d") {
			tmpl := filepath.Base(a)
			dir := filepath.Dir(a)
			_ = os.WriteFile(filepath.Join(dir, strings.Replace(tmpl, "%03d", "001", 1)), []byte("frame"), 0o644)
			_ = os.WriteFile(filepath.Join(dir, strings.Replace(tmpl, "%03d", "002", 1)), []byte("frame"), 0o644)
		}
	}
}

// ---- T062 executor 行为 ----

func TestExtractAudio_ArgsAreArrayPassed(t *testing.T) {
	fe := &fakeExecutor{}
	ff := &FFmpeg{Path: "/usr/local/bin/ffmpeg", Exec: fe}

	dir := t.TempDir()
	err := ff.ExtractAudio(context.Background(), filepath.Join(dir, "in.mp4"), filepath.Join(dir, "out.wav"))
	require.NoError(t, err)
	require.Len(t, fe.calls, 1)

	got := fe.calls[0]
	// 参数数组传递，无 shell；顺序固定：-nostdin -y -i in -vn -ac 1 -ar 16000 -f wav out
	assert.Equal(t, "-nostdin", got[0])
	assert.Equal(t, "-y", got[1])
	assert.Equal(t, "-i", got[2])
	assert.Equal(t, "-ac", got[5])
	assert.Equal(t, "1", got[6])
	assert.Equal(t, "-ar", got[7])
	assert.Equal(t, "16000", got[8])
	assert.Equal(t, "-f", got[9])
	assert.Equal(t, "wav", got[10])
	assert.Contains(t, got[len(got)-1], "out.wav")
}

func TestExtractAudio_CommandErrorPropagates(t *testing.T) {
	fe := &fakeExecutor{err: &RunError{bin: "ffmpeg", err: os.ErrNotExist, output: "boom"}}
	ff := &FFmpeg{Path: "ffmpeg", Exec: fe}
	err := ff.ExtractAudio(context.Background(), "a.mp4", "b.wav")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

func TestExtractKeyFrames_SceneThenFallback(t *testing.T) {
	fe := &fakeExecutor{genFrames: true}
	ff := &FFmpeg{Path: "ffmpeg", Exec: fe, MaxFrames: 5}

	dir := t.TempDir()
	frames, err := ff.ExtractKeyFrames(context.Background(), filepath.Join(dir, "in.mp4"), dir, 5)
	require.NoError(t, err)
	require.NotEmpty(t, frames)
	for _, f := range frames {
		assert.Equal(t, ".jpg", filepath.Ext(f))
	}
	// 第一次调用是场景切换（-vf 选择器 + vsync vfr）
	assert.Equal(t, "-vf", fe.calls[0][4])
	assert.Contains(t, fe.calls[0][5], "scene")
	assert.Equal(t, "-vsync", fe.calls[0][6])
}

func TestProbe_ParsesValidJSON(t *testing.T) {
	fe := &fakeExecutor{output: []byte(`{"format":{"duration":"12.5"},"streams":[{"codec_type":"video","codec_name":"h264","width":1920,"height":1080}]}`)}
	res, err := Probe(context.Background(), "ffprobe", fe, "x.mp4")
	require.NoError(t, err)
	assert.Equal(t, int64(12500), res.DurationMs)
	assert.Equal(t, 1920, res.Width)
	assert.Equal(t, 1080, res.Height)
	assert.Equal(t, "h264", res.CodecName)
}

func TestProbe_CorruptOutput(t *testing.T) {
	fe := &fakeExecutor{output: []byte(`not-json`)}
	_, err := Probe(context.Background(), "ffprobe", fe, "x.mp4")
	require.Error(t, err)
}

func TestProbe_MissingDuration(t *testing.T) {
	fe := &fakeExecutor{output: []byte(`{"format":{},"streams":[]}`)}
	res, err := Probe(context.Background(), "ffprobe", fe, "x.mp4")
	require.NoError(t, err)
	assert.Equal(t, int64(0), res.DurationMs)
	assert.Zero(t, res.Width)
}

// ---- T064 URL 白名单 / SSRF ----

// stubDNS 注入固定的 DNS 解析结果（避免依赖外网/离线环境）。
func stubDNS(t *testing.T, table map[string]string) {
	t.Helper()
	old := lookupIP
	lookupIP = func(host string) ([]net.IP, error) {
		if ip, ok := table[host]; ok {
			return []net.IP{net.ParseIP(ip)}, nil
		}
		return nil, fmt.Errorf("no such host: %s", host)
	}
	t.Cleanup(func() { lookupIP = old })
}

func TestValidateMediaURL_Whitelist(t *testing.T) {
	stubDNS(t, map[string]string{
		"bucket.cos.ap-guangzhou.myqcloud.com": "1.2.3.4",
		"img.myqcloud.com":                    "1.2.3.5",
		"example.com":                         "1.2.3.6",
	})
	allowed := []string{"*.myqcloud.com", "example.com"}
	cases := []struct {
		name string
		url  string
		ok   bool
	}{
		{"http 允许", "http://bucket.cos.ap-guangzhou.myqcloud.com/a.mp4", true},
		{"https 允许", "https://bucket.cos.ap-guangzhou.myqcloud.com/a.mp4", true},
		{"精确域名允许", "https://example.com/v.mp4", true},
		{"子域通配允许", "https://img.myqcloud.com/v.mp4", true},
		{"外域拒绝", "https://evil.com/v.mp4", false},
		{"非 http 协议拒绝", "file:///etc/passwd", false},
		{"ftp 拒绝", "ftp://bucket.myqcloud.com/v.mp4", false},
		{"无 host 拒绝", "https:///v.mp4", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateMediaURL(tc.url, allowed)
			if tc.ok {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestValidateMediaURL_BlockedHosts(t *testing.T) {
	// 白名单后缀匹配但 DNS 解析到内网 IP → 必须拒绝（SSRF 红线）。
	stubDNS(t, map[string]string{
		"127.0.0.1.myqcloud.com": "127.0.0.1",
		"10.0.0.1.myqcloud.com":  "10.0.0.1",
		"172.16.0.1.myqcloud.com": "172.16.0.1",
		"192.168.1.1.myqcloud.com": "192.168.1.1",
		"169.254.1.1.myqcloud.com": "169.254.1.1",
	})
	allowed := []string{"*.myqcloud.com"}
	cases := []string{
		"http://127.0.0.1.myqcloud.com/a.mp4",   // 解析到回环
		"http://10.0.0.1.myqcloud.com/a.mp4",    // 内网 C 段
		"http://172.16.0.1.myqcloud.com/a.mp4",  // 内网 B 段
		"http://192.168.1.1.myqcloud.com/a.mp4", // 内网 C 段
		"http://169.254.1.1.myqcloud.com/a.mp4", // 链路本地
		"http://evil.com/a.mp4",                 // 完全外域
		"http://localhost/a.mp4",                // 本地
	}
	for _, u := range cases {
		t.Run(u, func(t *testing.T) {
			err := ValidateMediaURL(u, allowed)
			require.Error(t, err)
		})
	}
}

// TestIsBlockedIP 直接验证内网/特殊 IP 判定。
func TestIsBlockedIP(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", true},
		{"10.1.2.3", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"192.168.1.1", true},
		{"169.254.0.1", true},
		{"100.64.0.1", true}, // CGNAT
		{"::1", true},
		{"fe80::1", true},
		{"8.8.8.8", false},
		{"114.114.114.114", false},
	}
	for _, tc := range cases {
		t.Run(tc.ip, func(t *testing.T) {
			assert.Equal(t, tc.want, isBlockedIP(parseIP(tc.ip)), "isBlockedIP(%s)", tc.ip)
		})
	}
}

func parseIP(s string) net.IP {
	ip := net.ParseIP(s)
	if ip == nil {
		panic("bad ip: " + s)
	}
	return ip
}
