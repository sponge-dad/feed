// Package logic Gateway 业务逻辑层（HTTP 入口）。
package logic

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	"github.com/sponge-dad/feed/common/errorx"
	"github.com/sponge-dad/feed/common/idgen"

	"github.com/zeromicro/go-zero/core/logx"
)

type UploadTokenLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUploadTokenLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UploadTokenLogic {
	return &UploadTokenLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// bizDir 将上传业务类型映射到 file_key 中的业务目录，同时作为白名单。
var bizDir = map[string]string{
	"avatar": "avatar",
	"cover":  "cover",
	"image":  "image",
	"video":  "video",
}

// allowedExt 允许的文件后缀白名单（小写）。
var allowedExt = map[string]struct{}{
	"jpg": {}, "jpeg": {}, "png": {}, "webp": {}, "gif": {},
	"mp4": {}, "mov": {}, "webm": {},
}

// UploadToken 签发 COS 临时上传凭证。
// 流程：解析登录态 -> 白名单校验 file_type/file_ext -> 生成唯一 file_key -> 申请 STS 凭证 -> 组装响应。
func (l *UploadTokenLogic) UploadToken(req *types.UploadTokenReq) (*types.UploadTokenResp, error) {
	uid := middleware.MustGetUserID(l.ctx)
	if uid == 0 {
		return nil, errorx.New(errorx.Unauthorized)
	}

	// 1. file_type 白名单校验
	biz, ok := bizDir[strings.ToLower(req.FileType)]
	if !ok {
		return nil, errorx.NewWithMsg(errorx.ParamError, "不支持的 file_type")
	}

	// 2. file_ext 白名单校验（兼容带/不带点、大小写）
	ext := strings.ToLower(strings.TrimSpace(req.FileExt))
	if !isValidExt(ext) {
		return nil, errorx.NewWithMsg(errorx.ParamError, "不支持的文件后缀")
	}
	ext = strings.TrimPrefix(ext, ".")

	conf := l.svcCtx.Config.Cos

	// 3. 生成 file_key：{env}/{biz}/{uid}/{yyyyMMdd}/{snowflake}.{ext}
	//    snowflake 保证全局唯一，重复请求生成不同 key；临时凭证 resource 限定到该 key 前缀，防越权写他人目录。
	fileKey := buildFileKey(conf.Env, biz, uid, ext)

	// 4. 申请 STS 临时上传凭证（仅允许 cos:PutObject 到该 file_key）
	cred, err := l.svcCtx.Cos.Issue(fileKey)
	if err != nil {
		l.Errorf("issue cos upload token failed, uid=%d key=%s err=%v", uid, fileKey, err)
		return nil, errorx.New(errorx.UploadTokenFail)
	}

	// 5. 组装响应
	objectURL := strings.TrimSuffix(conf.BaseURL, "/") + "/" + fileKey
	return &types.UploadTokenResp{
		UploadURL: objectURL, // 客户端使用 STS 临时凭证 PUT 到此对象地址
		FileURL:   objectURL, // 上传完成后可直接访问的地址
		FileKey:   fileKey,
		Credentials: types.UploadCredentials{
			TmpSecretID:  cred.TmpSecretID,
			TmpSecretKey: cred.TmpSecretKey,
			SessionToken: cred.SessionToken,
			ExpiredTime:  cred.ExpiredTime,
		},
	}, nil
}

// buildFileKey 生成唯一对象键：{env}/{biz}/{uid}/{yyyyMMdd}/{snowflake}.{ext}
func buildFileKey(env, biz string, uid int64, ext string) string {
	return fmt.Sprintf("%s/%s/%d/%s/%s.%s",
		env,
		biz,
		uid,
		time.Now().Format("20060102"),
		idgen.NextString(),
		ext,
	)
}

// isValidExt 判断文件后缀是否在白名单内。自动兼容大小写与带点写法。
func isValidExt(raw string) bool {
	ext := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(raw)), ".")
	if ext == "" {
		return false
	}
	_, ok := allowedExt[ext]
	return ok
}
