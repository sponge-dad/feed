// registerHandler.go
//
// 职责：处理用户注册 HTTP 请求。
// 流程：解析 body -> IP 定位城市 -> 调用 User.Register RPC -> 返回 user + token。
package handler

import (
	"net/http"

	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	userClient "github.com/sponge-dad/feed/app/user/rpc/userClient"
	"github.com/sponge-dad/feed/common/errorx"
	"github.com/sponge-dad/feed/common/response"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// RegisterHandler 处理 POST /api/v1/users/register。
func RegisterHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.RegisterReq
		if err := httpx.ParseJsonBody(r, &req); err != nil {
			response.Error(r.Context(), w, errorx.ParamError, "请求参数解析失败")
			return
		}

		// 简单参数校验：用户名、密码、昵称不能为空。
		if req.Username == "" || req.Password == "" || req.Nickname == "" {
			response.Error(r.Context(), w, errorx.ParamError, "用户名、密码、昵称不能为空")
			return
		}

		// 根据请求 IP 解析城市。当前为简化实现，本地/不可解析时默认深圳。
		cityCode, cityName := locateCity(r)

		resp, err := svcCtx.UserClient.Register(r.Context(), &userClient.RegisterReq{
			Username:  req.Username,
			Password:  req.Password,
			Nickname:  req.Nickname,
			CityCode:  cityCode,
			CityName:  cityName,
		})
		if err != nil {
			// 业务错误已在 User RPC 中包装为 errorx.CodeError，直接透传。
			writeError(r.Context(), w, err)
			return
		}

		response.Success(r.Context(), w, &types.RegisterResp{
			User:  userInfoToUser(resp.User),
			Token: resp.Token,
		})
	}
}

// locateCity 根据请求 IP 返回城市编码和名称。
// 当前为占位实现：本地/不可解析时默认深圳，后续可替换为 ip2region 或在线 IP 库。
func locateCity(r *http.Request) (string, string) {
	// 优先取 X-Forwarded-For，再取 RemoteAddr，便于网关前面有反向代理的场景。
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.Header.Get("X-Real-Ip")
	}
	if ip == "" {
		ip = r.RemoteAddr
	}

	logx.WithContext(r.Context()).Infof("register ip: %s", ip)

	// TODO: 接入真实 IP 定位库，例如 ip2region 或腾讯位置服务。
	if ip == "127.0.0.1" || ip == "localhost" || ip == "::1" || ip == "" {
		return "440300", "深圳"
	}
	return "440300", "深圳"
}
