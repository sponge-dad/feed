// Package logic 中 COS 资源引用的统一校验与签名辅助。
//
// 写路径（updateMe / createFeed）在落库前必须保证：
//  1. 资源归属当前用户（file_key 形如 {env}/{biz}/{uid}/...），防止引用他人文件；
//  2. 资源确实已上传到 COS（HEAD 校验），防止写入失效/未上传的引用；
//  3. 拒绝任意外部链接（SSRF / 脏数据）。
//
// 读路径（getMe / getUser / getFeedDetail / 列表卡片）对私有桶地址做临时签名，
// 使客户端可真正访问。详见 docs/design/oss/00-overview.md §6。
package logic

import (
	"strings"

	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

// CanonicalizeCosRef 校验并规整客户端传入的 COS 资源引用（avatar/media/cover）。
// 入参 raw 可以是 file_key、本桶完整 URL，或回传的签名 URL。
// 返回：规范可存储地址（BaseURL + "/" + key）、解析出的 file_key。
// 校验失败（非本人资源 / 未上传 / 外链）返回业务错误。
//
// svcCtx.Cos 为空时跳过存在性校验（单测场景），仅做归属校验，保证逻辑可独立测试。
func CanonicalizeCosRef(svcCtx *svc.ServiceContext, raw string, uid int64) (canonicalURL string, key string, err error) {
	if raw == "" {
		return "", "", errorx.NewWithMsg(errorx.ParamError, "媒体资源引用不能为空")
	}
	baseURL := svcCtx.Config.Cos.BaseURL
	env := svcCtx.Config.Cos.Env
	key, ok := resolveOwnedCosKey(raw, baseURL, uid, env)
	if !ok {
		return "", "", errorx.NewWithMsg(errorx.Forbidden, "非法的媒体资源引用（非本人资源）")
	}
	if svcCtx.Cos != nil {
		exists, eerr := svcCtx.Cos.Exists(key)
		if eerr != nil {
			logx.Errorf("cos: Exists check failed for %s: %v", key, eerr)
			return "", "", errorx.New(errorx.ServerError)
		}
		if !exists {
			return "", "", errorx.NewWithMsg(errorx.ParamError, "媒体资源未上传或不存在")
		}
	}
	if baseURL == "" {
		// 无 COS 配置（单测场景）：跳过规整，直接返回 key。
		return key, key, nil
	}
	return strings.TrimSuffix(baseURL, "/") + "/" + key, key, nil
}

// SignCosRef 将存储的 COS 地址转换为带签名的临时可访问地址（私有桶读场景）。
// svcCtx.Cos 为空或签名失败均降级为原值，避免阻塞主流程。
func SignCosRef(svcCtx *svc.ServiceContext, raw string) string {
	if raw == "" || svcCtx.Cos == nil {
		return raw
	}
	signed, err := svcCtx.Cos.SignURLFromRaw(raw, int64(svcCtx.Config.Cos.SignDuration))
	if err != nil {
		logx.Errorf("cos: sign url failed, fallback to raw: %v", err)
		return raw
	}
	return signed
}

// resolveOwnedCosKey 从 raw 中解析出 file_key 并校验归属当前用户。
// raw 可以是 file_key、本桶完整 URL，或带 ? 查询参数的签名 URL。
func resolveOwnedCosKey(raw, baseURL string, uid int64, env string) (string, bool) {
	if raw == "" {
		return "", false
	}
	// 兼容客户端回传签名 URL（带 ? 查询参数）的情形。
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		raw = raw[:i]
	}
	base := strings.TrimSuffix(baseURL, "/")
	if strings.HasPrefix(raw, base+"/") {
		raw = strings.TrimPrefix(raw, base+"/")
	}
	return raw, ownsFileKey(raw, uid, env)
}

// CosKeyBiz 提取 file_key 中的业务目录段（{env}/{biz}/{uid}/... 的 biz）。
func CosKeyBiz(key string) string {
	parts := strings.Split(key, "/")
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}
