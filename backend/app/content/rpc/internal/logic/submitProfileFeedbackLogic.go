package logic

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/sponge-dad/feed/app/content/rpc/content"
	"github.com/sponge-dad/feed/app/content/model"
	"github.com/sponge-dad/feed/app/content/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

type SubmitProfileFeedbackLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSubmitProfileFeedbackLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SubmitProfileFeedbackLogic {
	return &SubmitProfileFeedbackLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// feedbackTTL 反馈记录保留时长（30 天）。
const feedbackTTL = 30 * 24 * 3600

// feedbackRecord 创作者纠错反馈记录。
type feedbackRecord struct {
	FeedID    int64  `json:"feed_id"`
	UserID    int64  `json:"user_id"`
	Field     string `json:"field"`
	Comment   string `json:"comment"`
	CreatedAt int64  `json:"created_at"` // unix ms
}

// SubmitProfileFeedback 创作者纠错反馈：
//   - 仅作者本人可提交（画像 author_id == user_id）
//   - ⚠️ 只记录不改画像：写入 Redis list content:feedback:{feed_id}（TTL 30 天）+ 审计日志，
//     后续人工审核流程在此基础扩展，不直接覆盖任何画像字段
func (l *SubmitProfileFeedbackLogic) SubmitProfileFeedback(in *content.SubmitProfileFeedbackReq) (*content.SubmitProfileFeedbackResp, error) {
	if in.FeedId <= 0 || in.UserId <= 0 {
		return nil, errorx.New(errorx.ParamError)
	}
	if len(in.Comment) > 500 {
		return nil, errorx.NewWithMsg(errorx.ParamError, "反馈说明过长")
	}

	data, err := l.svcCtx.ContentProfilesModel.FindOneByFeedId(l.ctx, in.FeedId)
	switch {
	case errors.Is(err, model.ErrNotFound):
		return nil, errorx.New(errorx.ContentProfileNotFound)
	case err != nil:
		return nil, err
	}

	// 仅作者本人。
	if data.AuthorId != in.UserId {
		return nil, errorx.New(errorx.ContentProfileForbidden)
	}

	rec := &feedbackRecord{
		FeedID:    in.FeedId,
		UserID:    in.UserId,
		Field:     in.Field,
		Comment:   in.Comment,
		CreatedAt: time.Now().UnixMilli(),
	}
	body, err := json.Marshal(rec)
	if err != nil {
		return nil, err
	}

	// 记录到 Redis list（内容已脱敏校验，仅作者输入文本）。
	if _, err := l.svcCtx.Redis.Rpush(feedbackKey(in.FeedId), string(body)); err != nil {
		return nil, err
	}
	_ = l.svcCtx.Redis.Expire(feedbackKey(in.FeedId), feedbackTTL)

	// 审计日志（不改画像）。
	l.Logger.Infof("profile feedback recorded: feed_id=%d field=%s len=%d", in.FeedId, in.Field, len(in.Comment))
	return &content.SubmitProfileFeedbackResp{}, nil
}

// feedbackKey 反馈记录 key（Redis list）。
func feedbackKey(feedID int64) string {
	return "content:feedback:" + strconv.FormatInt(feedID, 10)
}
