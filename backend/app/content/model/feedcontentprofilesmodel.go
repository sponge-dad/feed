package model

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ FeedContentProfilesModel = (*customFeedContentProfilesModel)(nil)

type (
	// FeedContentProfilesModel is an interface to be customized, add more methods here,
	// and implement the added methods in customFeedContentProfilesModel.
	FeedContentProfilesModel interface {
		feedContentProfilesModel
		// UpsertByFeedID 幂等写画像（uk_feed_id 兜底），供 Content Worker 各阶段流转时原子更新。
		UpsertByFeedID(ctx context.Context, data *FeedContentProfiles) error
		// UpdateStatus 更新分析状态与失败信息（按 feed_id，无需回查主键）。
		UpdateStatus(ctx context.Context, feedID int64, status, errMsg string, degraded, retryCount int64) error
		// FindStuckTasks 捞取卡住的分析任务（状态为运行中且 updated_at 早于 before，用于运维恢复）。
		FindStuckTasks(ctx context.Context, before time.Time, limit int) ([]*FeedContentProfiles, error)
		// FindByCategory 按类目筛选已完成画像（同类对比、检索预筛）。
		FindByCategory(ctx context.Context, category, status string, limit int) ([]*FeedContentProfiles, error)
	}

	customFeedContentProfilesModel struct {
		*defaultFeedContentProfilesModel
	}
)

// NewFeedContentProfilesModel returns a model for the database table.
func NewFeedContentProfilesModel(conn sqlx.SqlConn, c cache.CacheConf, opts ...cache.Option) FeedContentProfilesModel {
	return &customFeedContentProfilesModel{
		defaultFeedContentProfilesModel: newFeedContentProfilesModel(conn, c, opts...),
	}
}

// upsertColumns 返回 on duplicate key update 子句（业务字段全量覆盖，排除主键/自增/审计时间）。
func upsertColumns(rows string) string {
	cols := strings.Split(rows, ",")
	for i, c := range cols {
		cols[i] = fmt.Sprintf("%s=values(%s)", c, c)
	}
	return strings.Join(cols, ",")
}

// UpsertByFeedID 以 feed_id 为幂等键 upsert 画像。
// Worker 各阶段流转（下载/抽帧/ASR/OCR/多模态/索引）都用它写最新状态。
func (m *customFeedContentProfilesModel) UpsertByFeedID(ctx context.Context, data *FeedContentProfiles) error {
	feedContentProfilesFeedIdKey := fmt.Sprintf("%s%v", cacheFeedContentProfilesFeedIdPrefix, data.FeedId)
	_, err := m.ExecCtx(ctx, func(ctx context.Context, conn sqlx.SqlConn) (sql.Result, error) {
		query := fmt.Sprintf("insert into %s (%s) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) on duplicate key update %s",
			m.table, feedContentProfilesRowsExpectAutoSet, upsertColumns(feedContentProfilesRowsExpectAutoSet))
		return conn.ExecCtx(ctx, query, data.FeedId, data.AuthorId, data.MediaHash, data.Category,
			data.Summary, data.Topics, data.Objects, data.Scenes, data.Styles, data.Transcript,
			data.TranscriptSegments, data.OcrText, data.Language, data.MediaDurationMs, data.KeyFrameCount,
			data.AnalysisStatus, data.Degraded, data.RetryCount, data.ModelVersion, data.ErrorMessage, data.AnalyzedAt)
	}, feedContentProfilesFeedIdKey)
	return err
}

// UpdateStatus 按 feed_id 更新状态与错误信息，并失效 feed_id 缓存。
func (m *customFeedContentProfilesModel) UpdateStatus(ctx context.Context, feedID int64, status, errMsg string, degraded, retryCount int64) error {
	feedContentProfilesFeedIdKey := fmt.Sprintf("%s%v", cacheFeedContentProfilesFeedIdPrefix, feedID)
	_, err := m.ExecCtx(ctx, func(ctx context.Context, conn sqlx.SqlConn) (sql.Result, error) {
		query := fmt.Sprintf("update %s set `analysis_status` = ?, `error_message` = ?, `degraded` = ?, `retry_count` = ? where `feed_id` = ?", m.table)
		return conn.ExecCtx(ctx, query, status, errMsg, degraded, retryCount, feedID)
	}, feedContentProfilesFeedIdKey)
	return err
}

// FindStuckTasks 捞取卡住的运行中任务（updated_at 早于 before），按更新时间升序（最久优先）。
func (m *customFeedContentProfilesModel) FindStuckTasks(ctx context.Context, before time.Time, limit int) ([]*FeedContentProfiles, error) {
	if limit <= 0 {
		limit = 100
	}
	query := fmt.Sprintf("select %s from %s where `analysis_status` in ('PENDING','DOWNLOADING','EXTRACTING','ASR_RUNNING','OCR_RUNNING','VISION_RUNNING','INDEXING') and `updated_at` < ? order by `updated_at` asc limit ?",
		feedContentProfilesRows, m.table)
	var rows []*FeedContentProfiles
	err := m.QueryRowsNoCacheCtx(ctx, &rows, query, before, limit)
	return rows, err
}

// FindByCategory 按类目 + 状态筛选画像（同类对比 / 检索预筛）。
func (m *customFeedContentProfilesModel) FindByCategory(ctx context.Context, category, status string, limit int) ([]*FeedContentProfiles, error) {
	if limit <= 0 {
		limit = 100
	}
	query := fmt.Sprintf("select %s from %s where `category` = ? and `analysis_status` = ? order by `updated_at` desc limit ?",
		feedContentProfilesRows, m.table)
	var rows []*FeedContentProfiles
	err := m.QueryRowsNoCacheCtx(ctx, &rows, query, category, status, limit)
	return rows, err
}
