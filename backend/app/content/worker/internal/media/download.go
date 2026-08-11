package media

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

// Downloader 媒体下载器。
type Downloader struct {
	Client   *http.Client
	MaxBytes int64 // 大小上限（默认 200MB），边下边校验
	Timeout  time.Duration
	// ValidateURL 可选注入的 URL 校验器（默认 ValidateMediaURL；单测可注入以绕开 DNS）。
	ValidateURL func(raw string, allowed []string) error
}

// Download 下载媒体到 dest：
//
//   - 前置校验 URL（白名单 + 内网拒绝）
//   - 连接时校验（DialContext 内 safeResolve 绑定已校验的公网 IP），消除 DNS rebinding 窗口
//   - 重定向目标经 CheckRedirect 逐跳重新校验（或拒绝跟随）
//   - 边下边校验 MaxBytes，超限立即中断
//
// 返回下载字节数。
func (d *Downloader) Download(ctx context.Context, rawURL, dest string, allowedHosts []string) (int64, error) {
	validate := d.ValidateURL
	if validate == nil {
		validate = ValidateMediaURL
	}
	if err := validate(rawURL, allowedHosts); err != nil {
		return 0, err
	}

	client := d.Client
	if client == nil {
		client = d.secureClient(allowedHosts)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download failed: status=%d url=%s", resp.StatusCode, rawURL)
	}

	maxBytes := d.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 200 * 1024 * 1024
	}
	// 服务端声明的 Content-Length 超限则直接拒绝
	if resp.ContentLength > maxBytes {
		return 0, fmt.Errorf("media too large: %d > %d", resp.ContentLength, maxBytes)
	}

	out, err := os.Create(dest)
	if err != nil {
		return 0, err
	}
	defer out.Close()

	// 限制读取上限（maxBytes+1 以便检测超限）
	limited := io.LimitReader(resp.Body, maxBytes+1)
	n, err := io.Copy(out, limited)
	if err != nil {
		return 0, err
	}
	if n > maxBytes {
		_ = os.Remove(dest)
		return 0, fmt.Errorf("media too large (>%d bytes)", maxBytes)
	}
	return n, nil
}

// secureClient 构造 SSRF 防护的 HTTP 客户端：
//
//   - DialContext 每次建连时 safeResolve 校验并固定直连公网 IP（校验与连接合一）
//   - CheckRedirect 对每个重定向目标重新执行 ValidateMediaURL（不信任跳转）
func (d *Downloader) secureClient(allowedHosts []string) *http.Client {
	timeout := d.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("invalid addr %q: %w", addr, err)
			}
			// 连接时校验：解析并绑定已通过校验的公网 IP，直连该 IP（防 DNS rebinding）。
			ip, err := safeResolve(host, allowedHosts)
			if err != nil {
				return nil, err
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		},
		ForceAttemptHTTP2: true,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			// 重定向目标逐跳重新校验（白名单 + 内网拒绝）。
			if err := ValidateMediaURL(req.URL.String(), allowedHosts); err != nil {
				return fmt.Errorf("redirect rejected: %w", err)
			}
			return nil
		},
	}
}
