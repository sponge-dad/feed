package logic

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/sponge-dad/feed/app/content/rpc/content"
	"github.com/sponge-dad/feed/app/content/model"
	"github.com/sponge-dad/feed/app/content/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"
	feedevent "github.com/sponge-dad/feed/common/event/feed"
	"github.com/sponge-dad/feed/common/requestid"

	"github.com/zeromicro/go-zero/core/logx"
)

type RetryContentAnalysisLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewRetryContentAnalysisLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RetryContentAnalysisLogic {
	return &RetryContentAnalysisLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// RetryContentAnalysis 重置画像状态并重新入队分析（仅内部用户）：
//   - 状态置 PENDING、清空错误信息与重试计数
//   - 重新发送 feed-created 事件，Content Worker 消费后按幂等逻辑重跑
//   - force=true 时忽略 media_hash+model_version 判重（模型升级重跑）
func (l *RetryContentAnalysisLogic) RetryContentAnalysis(in *content.RetryContentAnalysisReq) (*content.RetryContentAnalysisResp, error) {
	if !l.svcCtx.IsInternal(in.OperatorId) {
		return nil, errorx.New(errorx.Forbidden)
	}
	if in.FeedId <= 0 {
		return nil, errorx.New(errorx.ParamError)
	}

	data, err := l.svcCtx.ContentProfilesModel.FindOneByFeedId(l.ctx, in.FeedId)
	switch {
	case errors.Is(err, model.ErrNotFound):
		return nil, errorx.New(errorx.ContentProfileNotFound)
	case err != nil:
		return nil, err
	}

	// 重置状态：PENDING、清空错误、重试计数归零。force 时不改变 media_hash（幂等键由 Worker 侧处理）。
	if err := l.svcCtx.ContentProfilesModel.UpdateStatus(l.ctx, in.FeedId,
		statusPending, "", 0, 0); err != nil {
		return nil, err
	}

	// 重新入队：发送 feed-created 事件（Worker 消费后执行分析）。
	event := feedevent.NewEventFeedCreated(in.FeedId, data.AuthorId, false, "", timeNow().UnixMilli(), requestid.FromContext(l.ctx))
	body, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	if err := l.svcCtx.Producer.SendSync(feedevent.TopicFeedCreated, body); err != nil {
		l.Logger.Errorf("retry analysis re-queue feed %d failed: %v", in.FeedId, err)
		return nil, err
	}

	l.Logger.Infof("retry content analysis: feed_id=%d force=%v", in.FeedId, in.Force)
	return &content.RetryContentAnalysisResp{}, nil
}
