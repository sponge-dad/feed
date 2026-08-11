// Package media 封装 FFmpeg/ffprobe 子进程调用与媒体下载。
//
// 安全约束（docs/design/agent/13-security.md §5）：
//   - 固定二进制路径，不从 PATH 查找
//   - 参数数组传递，禁止 shell 拼接
//   - context.WithTimeout 超时 kill 进程组，避免僵尸进程
package media

import (
	"bytes"
	"context"
	"os/exec"
	"syscall"
	"time"
)

// Executor 可注入的命令执行器。
//
// 单元测试注入 fake 实现（断言参数、返回固定输出），CI 不依赖真实 FFmpeg 二进制
// （docs/design/agent/14-acceptance-test.md：CI 必须 mock FFmpeg）。
type Executor interface {
	// Run 执行命令，返回 stdout；超时或非零退出返回 error。
	Run(ctx context.Context, bin string, args ...string) ([]byte, error)
}

// OSExecutor 真实实现：参数数组传递（无 shell）+ 进程组隔离 + 超时强杀。
type OSExecutor struct {
	// Timeout 单条命令超时（默认 120s）。
	Timeout time.Duration
}

func (e OSExecutor) Run(ctx context.Context, bin string, args ...string) ([]byte, error) {
	timeout := e.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// 参数以数组传递，禁止 sh -c / 字符串拼接（RCE 红线）。
	cmd := exec.CommandContext(ctx, bin, args...)
	// 独立进程组：超时 kill 整个进程组（ffmpeg 可能派生子进程）。
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return buf.Bytes(), &TimeoutError{bin: bin}
	}
	if err != nil {
		return buf.Bytes(), &RunError{bin: bin, err: err, output: buf.String()}
	}
	return buf.Bytes(), nil
}

// TimeoutError 命令超时（进程组已被强杀）。
type TimeoutError struct{ bin string }

func (e *TimeoutError) Error() string { return "command timeout: " + e.bin }

// RunError 命令执行失败（非零退出）。
type RunError struct {
	bin    string
	err    error
	output string
}

func (e *RunError) Error() string {
	return "command failed: " + e.bin + ": " + e.err.Error() + " output=" + e.output
}

// Unwrap 暴露底层错误。
func (e *RunError) Unwrap() error { return e.err }
