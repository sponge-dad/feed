package logic

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/sponge-dad/feed/app/interaction/interest"
	"github.com/sponge-dad/feed/app/interaction/model"
	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetUserInterestProfileLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetUserInterestProfileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserInterestProfileLogic {
	return &GetUserInterestProfileLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// coldStartThreshold 冷启动判定：有效行为 < 10。
const coldStartThreshold = 10

// maxTopN top_n 上限。
const maxTopN = 50

// GetUserInterestProfile 用户兴趣画像（06-user-interest.md §5）：
//   - 权限：user_id 必须等于 viewer_id（内部用户例外）→ 否则 Forbidden
//   - 读取顺序：Redis ZSet（实时）→ 未命中回查 MySQL 快照（兜底）
//   - 返回归一化相对占比（ratio = score/Σscore），不暴露原始权重与衰减因子
//   - 有效行为 < 10 → is_cold_start=true + 空列表
func (l *GetUserInterestProfileLogic) GetUserInterestProfile(in *interaction.GetUserInterestProfileReq) (*interaction.GetUserInterestProfileResp, error) {
	if in.UserId <= 0 || in.ViewerId <= 0 {
		return nil, errParam()
	}
	// 权限：只能查本人（内部用户例外——viewer 是调用者身份）。
	if !l.svcCtx.IsInternal(in.ViewerId) && in.ViewerId != in.UserId {
		return nil, errorx.New(errorx.Forbidden)
	}
	topN := int(in.TopN)
	if topN <= 0 {
		topN = 10
	}
	if topN > maxTopN {
		topN = maxTopN
	}

	// Redis ZSet 实时数据。
	snap, err := interest.BuildSnapshot(l.ctx, l.svcCtx.Redis, in.UserId)
	if err == nil && len(snap.Categories)+len(snap.Topics) > 0 {
		return l.toResp(in.UserId, snap, topN), nil
	}
	// 未命中 → MySQL 快照兜底。
	mysqlSnap, ts, err := l.loadFromMySQL(in.UserId)
	if err != nil {
		return nil, err
	}
	if mysqlSnap == nil {
		// 完全无数据：按冷启动处理。
		return &interaction.GetUserInterestProfileResp{UserId: in.UserId, IsColdStart: true}, nil
	}
	return l.toRespWithTime(in.UserId, mysqlSnap, topN, ts), nil
}

// toResp 从 Redis 快照构造响应（calculated_at=now）。
func (l *GetUserInterestProfileLogic) toResp(userID int64, snap *interest.Snapshot, topN int) *interaction.GetUserInterestProfileResp {
	return l.toRespWithTime(userID, snap, topN, snap.CalculatedAt)
}

// toRespWithTime 归一化占比并构造响应。
func (l *GetUserInterestProfileLogic) toRespWithTime(userID int64, snap *interest.Snapshot, topN int, ts time.Time) *interaction.GetUserInterestProfileResp {
	resp := &interaction.GetUserInterestProfileResp{
		UserId:        userID,
		TotalActions:  snap.TotalActions,
		CalculatedAt:  ts.UnixMilli(),
		IsColdStart:   snap.TotalActions < coldStartThreshold,
		TopTopics:     ratioItems(snap.Topics, topN),
		TopCategories: ratioItems(snap.Categories, topN),
	}
	return resp
}

// loadFromMySQL 回查 MySQL 快照；不存在返回 (nil, zero, nil)。
func (l *GetUserInterestProfileLogic) loadFromMySQL(userID int64) (*interest.Snapshot, time.Time, error) {
	row, err := l.svcCtx.UserInterestModel.FindOneByUserId(l.ctx, userID)
	if errors.Is(err, model.ErrNotFound) {
		return nil, time.Time{}, nil
	}
	if err != nil {
		return nil, time.Time{}, err
	}
	var snap interest.Snapshot
	var payload struct {
		Categories   []interest.Item `json:"categories"`
		Topics       []interest.Item `json:"topics"`
		TotalActions int64           `json:"total_actions"`
	}
	if err := json.Unmarshal([]byte(row.InterestJson), &payload); err != nil {
		return nil, time.Time{}, err
	}
	snap.Categories = payload.Categories
	snap.Topics = payload.Topics
	snap.TotalActions = payload.TotalActions
	return &snap, row.CalculatedAt, nil
}

// ratioItems 归一化占比（score/Σscore，保留 3 位小数），按 score 降序取前 topN。
func ratioItems(items []interest.Item, topN int) []*interaction.InterestItem {
	var total float64
	for _, it := range items {
		total += it.Score
	}
	out := make([]*interaction.InterestItem, 0, len(items))
	for _, it := range items {
		if len(out) >= topN {
			break
		}
		r := 0.0
		if total > 0 {
			r = it.Score / total
		}
		r = float64(int(r*1000)) / 1000 // 保留 3 位小数
		out = append(out, &interaction.InterestItem{Name: it.Name, Ratio: r})
	}
	return out
}
