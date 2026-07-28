// Package ipx 提供客户端 IP 提取与 IP -> 城市 定位能力。
//
// 网关在同城流、发帖 IP 属地等场景需要将请求 IP 实时解析为城市信息。
// 当前实现为 V1 占位版：
//   - ClientIP 从 X-Forwarded-For / X-Real-IP / RemoteAddr 提取客户端 IP；
//   - StaticResolver 将任意合法 IP 解析为配置的默认城市（本地/内网开发用），
//     未配置默认城市时返回 ErrLocateFail，由上层映射为业务码 12006。
//
// 生产环境接入真实 GeoIP 库/服务时，只需提供新的 Resolver 实现。
package ipx

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
)

// ErrLocateFail 表示无法根据 IP 定位城市。
var ErrLocateFail = errors.New("ipx: locate city by ip failed")

// Location IP 定位结果。
type Location struct {
	CityCode string // 行政区划代码，如 440300
	CityName string // 城市名，如 深圳
	Province string // 省份名，如 广东
}

// Resolver 将 IP 解析为城市信息。
type Resolver interface {
	Resolve(ip string) (*Location, error)
}

// staticResolver 静态解析器：任何合法 IP 均返回默认城市；
// 未配置默认城市或 IP 非法时返回 ErrLocateFail。
type staticResolver struct {
	def *Location
}

// NewStaticResolver 创建静态解析器，def 为 nil 时所有解析都会失败。
func NewStaticResolver(def *Location) Resolver {
	return &staticResolver{def: def}
}

// Resolve 实现 Resolver。
func (r *staticResolver) Resolve(ip string) (*Location, error) {
	if net.ParseIP(ip) == nil {
		return nil, ErrLocateFail
	}
	if r.def == nil {
		return nil, ErrLocateFail
	}
	loc := *r.def
	return &loc, nil
}

// ClientIP 从 HTTP 请求中提取客户端 IP。
// 依次尝试 X-Forwarded-For 首个地址、X-Real-IP、RemoteAddr。
// 注意：XFF/X-Real-IP 可被伪造，仅应在网关前有可信代理时信任；
// 该 IP 只用于城市定位展示，不用于鉴权。
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if ip := strings.TrimSpace(parts[0]); net.ParseIP(ip) != nil {
			return ip
		}
	}
	if rip := strings.TrimSpace(r.Header.Get("X-Real-IP")); rip != "" && net.ParseIP(rip) != nil {
		return rip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// RemoteAddr 可能不带端口
		if net.ParseIP(r.RemoteAddr) != nil {
			return r.RemoteAddr
		}
		return ""
	}
	return host
}

// ctxKey context 私有 key 类型，避免与其它包冲突。
type ctxKey struct{}

// WithClientIP 将客户端 IP 写入 context。
func WithClientIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, ctxKey{}, ip)
}

// ClientIPFromContext 从 context 读取客户端 IP，不存在时返回空串。
func ClientIPFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKey{}).(string); ok {
		return v
	}
	return ""
}
