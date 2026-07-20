// uploadTokenHandler.go
//
// 职责：生成 COS 临时上传凭证。
//
// 当前为占位实现：返回符合 API 形状的结构体，但 credentials 为空。
// 后续需接入腾讯云 COS STS 或 AWS S3 Presigned Post 等真实云存储服务。
package handler

import (
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	"github.com/sponge-dad/feed/common/errorx"
	"github.com/sponge-dad/feed/common/response"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// UploadTokenHandler 处理 POST /api/v1/upload/token。
func UploadTokenHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		meID := middleware.UserIDFromContext(r.Context())
		if meID == 0 {
			response.HTTPError(r.Context(), w, http.StatusUnauthorized, errorx.Unauthorized, "未登录")
			return
		}

		var req types.UploadTokenReq
		if err := httpx.ParseJsonBody(r, &req); err != nil {
			response.Error(r.Context(), w, errorx.ParamError, "请求参数解析失败")
			return
		}

		// 简单校验文件类型。
		if req.FileType != "image" && req.FileType != "video" {
			response.Error(r.Context(), w, errorx.ParamError, "file_type 只能是 image 或 video")
			return
		}
		if req.FileExt == "" {
			req.FileExt = "jpg"
		}

		// TODO: 接入真实云存储 STS，目前返回占位凭证。
		logx.WithContext(r.Context()).Info("upload token is using placeholder credentials, please integrate real COS STS")

		dateStr := time.Now().Format("20060102")
		fileKey := fmt.Sprintf("%s/%d/%s/%s.%s", req.FileType, meID, dateStr, uuid.New().String(), req.FileExt)
		uploadURL := "https://feed-xxx.cos.ap-guangzhou.myqcloud.com"
		fileURL := fmt.Sprintf("https://cdn.xxx.com/%s", fileKey)

		response.Success(r.Context(), w, &types.UploadTokenResp{
			UploadURL: uploadURL,
			Credentials: types.UploadCredentials{
				ExpiredTime: time.Now().Add(time.Hour).Unix(),
			},
			FileKey: fileKey,
			FileURL: fileURL,
		})
	}
}
