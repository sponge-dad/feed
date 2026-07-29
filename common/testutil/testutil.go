// Package testutil 提供集成测试共用的环境辅助能力：
// 动态空闲端口申请、基础设施探活（短超时 TCP 拨测）、服务就绪等待、DSN 解析。
//
// 仅供 *_test.go / tests 包使用，禁止业务代码引用。
package testutil

import (
	"fmt"
	"net"
	"strings"
	"time"
)

// probeTimeout 单次 TCP 探活超时，避免基础设施缺失时长时间阻塞。
const probeTimeout = 500 * time.Millisecond

// FreePort 向内核申请一个当前空闲的 TCP 端口并立即释放，
// 返回 "127.0.0.1:<port>" 形式的监听地址。
// 用于集成测试动态选择监听端口，避免与业务服务或其他测试包的硬编码端口冲突。
func FreePort() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("testutil: acquire free port: %w", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		return "", fmt.Errorf("testutil: release probe listener: %w", err)
	}
	return addr, nil
}

// DialOK 以短超时探测目标 TCP 地址是否可达，用于集成测试统一环境探活。
func DialOK(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, probeTimeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// WaitReady 轮询等待目标 TCP 地址可连接（测试内启动的 RPC Server 就绪探测），
// 超时返回错误，替代固定 time.Sleep。
func WaitReady(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, probeTimeout)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("testutil: server %s not ready within %s", addr, timeout)
}

// MySQLAddrFromDSN 从 go-sql-driver DSN 中提取 tcp 地址，用于连接前探活。
func MySQLAddrFromDSN(dsn string) string {
	start := strings.Index(dsn, "tcp(")
	if start < 0 {
		return "127.0.0.1:3306"
	}
	rest := dsn[start+len("tcp("):]
	end := strings.Index(rest, ")")
	if end < 0 {
		return "127.0.0.1:3306"
	}
	return rest[:end]
}
