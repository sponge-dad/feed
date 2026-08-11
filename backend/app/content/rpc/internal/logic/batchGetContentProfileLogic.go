package logic

import (
	"context"
	"errors"

	"github.com/sponge-dad/feed/app/content/rpc/content"
	"github.com/sponge-dad/feed/app/content/model"
	"github.com/sponge-dad/feed/app/content/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

type BatchGetContentProfileLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewBatchGetContentProfileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BatchGetContentProfileLogic {
	return &BatchGetContentProfileLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// maxBatchSize 批量上限（契约 §1：≤50）。
const maxBatchSize = 50

// BatchGetContentProfile 批量查询内容画像：
//   - ≤50 个 feed_id，仅公开字段（供兴趣画像与推荐解释使用）
//   - 仅返回 COMPLETED 且未禁用的画像；缺失/进行中/失败的项跳过（不报错）
//   - 结果按请求顺序返回
func (l *BatchGetContentProfileLogic) BatchGetContentProfile(in *content.BatchGetContentProfileReq) (*content.BatchGetContentProfileResp, error) {
	if len(in.FeedIds) == 0 {
		return &content.BatchGetContentProfileResp{}, nil
	}
	if len(in.FeedIds) > maxBatchSize {
		return nil, errorx.NewWithMsg(errorx.ParamError, "批量查询超过上限(50)")
	}

	profiles := make([]*content.ContentProfile, 0, len(in.FeedIds))
	for _, feedID := range in.FeedIds {
		data, err := l.svcCtx.ContentProfilesModel.FindOneByFeedId(l.ctx, feedID)
		if errors.Is(err, model.ErrNotFound) {
			continue // 缺失项跳过
		}
		if err != nil {
			return nil, err
		}
		// 仅已完成且未禁用；公开字段（full=false）。
		if data.AnalysisStatus == statusCompleted {
			profiles = append(profiles, profileToPB(data, false))
		}
	}

	return &content.BatchGetContentProfileResp{Profiles: profiles}, nil
}
