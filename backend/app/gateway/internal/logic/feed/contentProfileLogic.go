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

// ContentProfileLogic 内容画像查询逻辑（http-api.md #7）。
type ContentProfileLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewContentProfileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ContentProfileLogic {
	return &ContentProfileLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// ContentProfile 查询内容画像（分级返回）。
// user_id 取自 JWT，客户端传的一律忽略。
func (l *ContentProfileLogic) ContentProfile(req *types.GetContentProfileReq) (*types.GetContentProfileResp, error) {
	me := middleware.MustGetUserID(l.ctx)
	if me == 0 {
		return nil, errorx.New(errorx.Unauthorized)
	}
	if req.FeedId <= 0 {
		return nil, errorx.New(errorx.ParamError)
	}

	resp, err := l.svcCtx.ContentRpc.GetContentProfile(l.ctx, &contentpb.GetContentProfileReq{
		FeedId:   req.FeedId,
		ViewerId: me,
	})
	if err != nil {
		return nil, err
	}
	p := resp.Profile
	if p == nil {
		return &types.GetContentProfileResp{}, nil
	}
	return &types.GetContentProfileResp{Profile: &types.ContentProfile{
		FeedId:          p.FeedId,
		AuthorId:        p.AuthorId,
		Category:        p.Category,
		Summary:         p.Summary,
		Topics:          p.Topics,
		Objects:         p.Objects,
		Scenes:          p.Scenes,
		Styles:          p.Styles,
		Transcript:      p.Transcript,
		OcrText:         p.OcrText,
		Language:        p.Language,
		MediaDurationMs: p.MediaDurationMs,
		KeyFrameCount:   p.KeyFrameCount,
		AnalysisStatus:  int32(p.AnalysisStatus),
		Degraded:        p.Degraded,
		ModelVersion:    p.ModelVersion,
		ErrorMessage:    p.ErrorMessage,
		AnalyzedAt:      p.AnalyzedAt,
	}}, nil
}
