package media

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// lookupIP 可注入的 DNS 解析（默认 net.LookupIP；单测替换以控制解析结果）。
var lookupIP = net.LookupIP

// ValidateMediaURL 校验媒体下载地址（SSRF 防护，安全红线，见 13-security.md §5）：
//
//  1. 仅允许 http/https
//  2. host 必须匹配 AllowedMediaHosts 白名单（支持 `*.domain` 通配子域）
//  3. DNS 解析后的 IP 必须为公网地址；拒绝回环 / 内网 / 链路本地（127.* / 10.* /
//     172.16~31.* / 192.168.* / 169.254.* / ::1 / fc00::/7 等）
func ValidateMediaURL(rawURL string, allowedHosts []string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid media url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("media url scheme not allowed: %s", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("media url has empty host")
	}
	if !hostAllowed(host, allowedHosts) {
		return fmt.Errorf("media host not in whitelist: %s", host)
	}
	// DNS 解析并拒绝内网地址
	ips, err := lookupIP(host)
	if err != nil {
		return fmt.Errorf("media host resolve failed: %w", err)
	}
	for _, ip := range ips {
		if isBlockedIP(ip) {
			return fmt.Errorf("media host resolves to blocked ip: %s", ip.String())
		}
	}
	return nil
}

// safeResolve 解析 host 并返回一个已通过白名单与内网校验的公网 IP。
// 用于「连接时校验」：下载建立连接时再次解析并校验，消除 DNS rebinding 的 TOCTOU 窗口
// （校验结果绑定到实际连接，而非校验后丢弃由 transport 重新解析）。
func safeResolve(host string, allowedHosts []string) (net.IP, error) {
	if !hostAllowed(host, allowedHosts) {
		return nil, fmt.Errorf("media host not in whitelist: %s", host)
	}
	ips, err := lookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("media host resolve failed: %w", err)
	}
	for _, ip := range ips {
		if !isBlockedIP(ip) {
			return ip, nil
		}
	}
	return nil, fmt.Errorf("media host resolves to blocked ip only: %s", host)
}

// hostAllowed 匹配白名单（`*.example.com` 匹配任意子域）。
func hostAllowed(host string, allowed []string) bool {
	host = strings.ToLower(host)
	for _, a := range allowed {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "" {
			continue
		}
		if strings.HasPrefix(a, "*.") {
			suffix := a[1:] // ".example.com"
			if strings.HasSuffix(host, suffix) {
				return true
			}
			continue
		}
		if a == host {
			return true
		}
	}
	return false
}

// isBlockedIP 判断是否为应拒绝的内网/特殊地址。
func isBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	// 兼容 169.254 等（IsLinkLocalUnicast 已覆盖）
	if ip.To4() != nil {
		// 额外的 CGNAT 段 100.64.0.0/10 也拒绝（常见内网段）
		if ip.To4()[0] == 100 && ip.To4()[1] >= 64 && ip.To4()[1] <= 127 {
			return true
		}
	}
	return false
}
