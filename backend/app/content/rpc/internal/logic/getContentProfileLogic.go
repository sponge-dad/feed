package logic

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/sponge-dad/feed/app/content/rpc/content"
	"github.com/sponge-dad/feed/app/content/keys"
	"github.com/sponge-dad/feed/app/content/model"
	"github.com/sponge-dad/feed/app/content/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

type GetContentProfileLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetContentProfileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetContentProfileLogic {
	return &GetContentProfileLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetContentProfile 查询单条内容画像（分级返回）：
//   - 字幕/OCR 全文、错误信息仅作者本人或内部用户可见
//   - 其它调用方只返回 category/summary/topics/scenes 等公开字段（cache-aside，TTL 1h）
//   - 状态语义见 docs/design/agent/04-content-analysis.md §8
func (l *GetContentProfileLogic) GetContentProfile(in *content.GetContentProfileReq) (*content.GetContentProfileResp, error) {
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

	// 状态语义（§8）：DISABLED 按不存在处理；运行中返回 15002；失败返回 15003。
	switch {
	case data.AnalysisStatus == statusDisabled:
		return nil, errorx.New(errorx.ContentProfileNotFound)
	case isRunningStatus(data.AnalysisStatus):
		return nil, errorx.NewWithMsg(errorx.ContentAnalysisRunning, "内容分析进行中: "+data.AnalysisStatus)
	case data.AnalysisStatus == statusFailed:
		return nil, errorx.New(errorx.ContentAnalysisFailed)
	}

	// 分级权限：作者本人或内部用户可看全量。
	full := (in.ViewerId > 0 && in.ViewerId == data.AuthorId) || l.svcCtx.IsInternal(in.ViewerId)

	// 非作者/非内部：公开字段走 cache-aside；命中则直接返回。
	if !full {
		if cached, cerr := l.readCache(in.FeedId); cerr == nil && cached != nil {
			return &content.GetContentProfileResp{Profile: fromCache(cached)}, nil
		}
	}

	// COMPLETED 且未命中缓存时写公开字段缓存。
	if data.AnalysisStatus == statusCompleted {
		_ = l.writeCache(data)
	}

	return &content.GetContentProfileResp{Profile: profileToPB(data, full)}, nil
}

// readCache 读取公开字段缓存；未命中返回 (nil, nil)。
func (l *GetContentProfileLogic) readCache(feedID int64) (*publicProfileCache, error) {
	val, err := l.svcCtx.Redis.Get(keys.ProfileCacheKey(feedID))
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c publicProfileCache
	if err := json.Unmarshal([]byte(val), &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// writeCache 写公开字段缓存（失败仅记日志，不影响主流程）。
func (l *GetContentProfileLogic) writeCache(data *model.FeedContentProfiles) error {
	body, err := json.Marshal(toCache(data))
	if err != nil {
		return err
	}
	return l.svcCtx.Redis.Setex(keys.ProfileCacheKey(data.FeedId), string(body), keys.TTLProfileCache)
}
