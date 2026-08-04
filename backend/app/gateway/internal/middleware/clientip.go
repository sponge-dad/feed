// clientip.go
//
// 职责：全局中间件，提取客户端 IP 并注入请求 context，
// 供 logic 层（同城流定位、发帖 IP 属地）通过 ipx.ClientIPFromContext 读取。
package middleware

import (
	"net/http"

	"github.com/sponge-dad/feed/common/ipx"
)

// ClientIPMiddleware 提取客户端 IP 写入 context。
func ClientIPMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := ipx.WithClientIP(r.Context(), ipx.ClientIP(r))
		next(w, r.WithContext(ctx))
	}
}
