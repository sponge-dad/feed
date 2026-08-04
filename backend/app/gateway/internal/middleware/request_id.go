package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"regexp"

	"github.com/sponge-dad/feed/common/requestid"
	"github.com/zeromicro/go-zero/core/logx"
)

var legalHeader = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func RequestIDMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if match := legalHeader.MatchString(id); !match {
			id = generateRequestId()
		}
		ctx := requestid.WithRequestID(r.Context(), id)
		//ctx = context.WithValue(ctx, "request_id", id)  新方式如上，使用typed key作为key
		w.Header().Set("X-Request-ID", id)
		// 绑定到go-zero日志
		ctx = logx.WithFields(ctx, logx.Field("request_id", id))
		next(w, r.WithContext(ctx))
	}
}

func generateRequestId() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
