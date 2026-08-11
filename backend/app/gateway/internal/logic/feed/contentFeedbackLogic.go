package feed

import (
	"context"

	contentpb "github.com/sponge-dad/feed/app/content/rpc/content"
	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

// ContentFeedbackLogic 创作者纠错反馈逻辑（http-api.md #8，只记录不改画像）。
type ContentFeedbackLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewContentFeedbackLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ContentFeedbackLogic {
	return &ContentFeedbackLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// SubmitProfileFeedback 提交识别纠错反馈（作者本人校验在 Content RPC 完成）。
func (l *ContentFeedbackLogic) SubmitProfileFeedback(req *types.SubmitProfileFeedbackReq) (*types.SubmitProfileFeedbackResp, error) {
	me := middleware.MustGetUserID(l.ctx)
	if me == 0 {
		return nil, errorx.New(errorx.Unauthorized)
	}
	if req.FeedId <= 0 {
		return nil, errorx.New(errorx.ParamError)
	}
	if len(req.Comment) > 500 {
		return nil, errorx.NewWithMsg(errorx.ParamError, "反馈说明过长")
	}

	if _, err := l.svcCtx.ContentRpc.SubmitProfileFeedback(l.ctx, &contentpb.SubmitProfileFeedbackReq{
		FeedId:  req.FeedId,
		UserId:  me,
		Field:   req.Field,
		Comment: req.Comment,
	}); err != nil {
		return nil, err
	}
	return &types.SubmitProfileFeedbackResp{OK: true}, nil
}
