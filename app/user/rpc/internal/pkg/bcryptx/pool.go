// Package bcryptx 对 bcrypt 计算进行并发限流，防止 CPU 密集型操作把 User RPC 服务打满。
//
// 背景：注册和登录都需要 bcrypt（GenerateFromPassword / CompareHashAndPassword），
// 这两个函数都是纯 CPU 计算且会长时间占用一个 OS 线程。go-zero 默认会为每个 gRPC
// 请求启动一个 goroutine，如果并发请求数远超 CPU 核心数，大量 goroutine 会排队等待
// CPU 时间片，导致请求处理时间线性增长，最终触发 Gateway RPC 超时，表现为 503。
//
// 解决思路：用一个带缓冲的 channel 作为信号量，把 bcrypt 并发数限制在 CPU 核心数
// 附近（可配置）。超过容量的请求会排队而不是无限制争抢 CPU，这样虽然单个请求可能
// 因为排队略有延迟，但整体吞吐更稳定，错误率会显著下降。
package bcryptx

import (
	"runtime"

	"golang.org/x/crypto/bcrypt"
)

// Pool 限制 bcrypt 计算的并发度。
type Pool struct {
	sem chan struct{}
}

// NewPool 创建一个 bcrypt 计算池。
// workers <= 0 时默认使用 runtime.NumCPU()，与 CPU 核心数保持一致。
func NewPool(workers int) *Pool {
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	return &Pool{
		sem: make(chan struct{}, workers),
	}
}

// Hash 在池内执行 bcrypt.GenerateFromPassword，并发数受池容量限制。
func (p *Pool) Hash(password []byte, cost int) ([]byte, error) {
	p.sem <- struct{}{}
	defer func() { <-p.sem }()
	return bcrypt.GenerateFromPassword(password, cost)
}

// Compare 在池内执行 bcrypt.CompareHashAndPassword，并发数受池容量限制。
func (p *Pool) Compare(hashedPassword, password []byte) error {
	p.sem <- struct{}{}
	defer func() { <-p.sem }()
	return bcrypt.CompareHashAndPassword(hashedPassword, password)
}
