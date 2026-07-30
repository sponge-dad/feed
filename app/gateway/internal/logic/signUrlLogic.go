// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package logic

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

type SignUrlLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSignUrlLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SignUrlLogic {
	return &SignUrlLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// SignUrl 为私有桶对象生成临时可访问的签名 URL。
// 安全：必须校验 file_key 归属当前登录用户，否则任意用户可签名他人私有文件（见 §6.6）。
func (l *SignUrlLogic) SignUrl(req *types.SignUrlReq) (resp *types.SignUrlResp, err error) {
	uid := middleware.MustGetUserID(l.ctx)
	if uid == 0 {
		return nil, errorx.New(errorx.Unauthorized)
	}

	conf := l.svcCtx.Config.Cos

	// 1. 校验 file_key 归属：{env}/{biz}/{uid}/{yyyyMMdd}/{snowflake}.{ext}
	if !ownsFileKey(req.FileKey, uid, conf.Env) {
		return nil, errorx.NewWithMsg(errorx.Forbidden, "无权访问该资源")
	}

	// 2. 计算有效期（秒）：入参 <=0 时取配置默认
	dur := req.Duration
	if dur <= 0 {
		dur = conf.SignDuration
	}
	if dur <= 0 {
		dur = 600
	}

	// 3. 生成 GET 预签名 URL
	signedURL, err := l.svcCtx.Cos.SignGet(req.FileKey, dur)
	if err != nil {
		l.Errorf("sign cos get url failed, uid=%d key=%s err=%v", uid, req.FileKey, err)
		return nil, errorx.New(errorx.ServerError)
	}

	return &types.SignUrlResp{
		SignedURL: signedURL,
		ExpiredAt: time.Now().Add(time.Duration(dur) * time.Second).Unix(),
	}, nil
}

// ownsFileKey 校验 file_key 是否属于指定用户：目录穿越防护 + 5 段结构 + env/biz/uid 三段匹配。
func ownsFileKey(key string, uid int64, env string) bool {
	if strings.Contains(key, "..") { // 防目录穿越
		return false
	}
	parts := strings.Split(key, "/")
	if len(parts) != 5 {
		return false
	}
	if parts[0] != env {
		return false
	}
	if _, ok := bizDir[parts[1]]; !ok { // 业务目录须为白名单内
		return false
	}
	return parts[2] == strconv.FormatInt(uid, 10)
}
