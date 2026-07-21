package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	"github.com/sponge-dad/feed/common/errorx"

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

func (l *UploadTokenLogic) UploadToken(req *types.UploadTokenReq) (*types.UploadTokenResp, error) {
	// 上传服务尚未接入，返回占位错误。
	return nil, errorx.NewWithMsg(errorx.UploadTokenFail, "上传服务暂未接入")
}
