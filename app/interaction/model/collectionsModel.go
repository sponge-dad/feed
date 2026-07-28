// collectionsModel.go
//
// 职责：collections 表自定义扩展查询，与 likesModel.go 同构。
// goctl 生成的基础 CRUD 见 collectionsModel_gen.go（DO NOT EDIT）。
// 所有 SQL 均使用参数绑定，禁止字符串拼接值，防止 SQL 注入。
package model

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ CollectionsModel = (*customCollectionsModel)(nil)

type (
	// CollectionsModel is an interface to be customized, add more methods here,
	// and implement the added methods in customCollectionsModel.
	CollectionsModel interface {
		collectionsModel
		// CountByFeedId 统计帖子的有效收藏数（status=1）。
		CountByFeedId(ctx context.Context, feedId uint64) (int64, error)
		// CountByFeedIds 批量统计多个帖子的有效收藏数，返回 feedId -> count。
		CountByFeedIds(ctx context.Context, feedIds []uint64) (map[uint64]int64, error)
		// FindUserIdsByFeedId 查询帖子的全部有效收藏用户 ID（用于重建 collect:feed Set）。
		FindUserIdsByFeedId(ctx context.Context, feedId uint64) ([]uint64, error)
		// FindValidByUserId 查询用户有效收藏记录，按收藏时间倒序，最多 limit 条
		// （用于重建 user:collects ZSet）。
		FindValidByUserId(ctx context.Context, userId uint64, limit int) ([]*Collections, error)
		// FindByUserIdFeedIds 批量查询用户对多个帖子的收藏记录（含已取消）。
		FindByUserIdFeedIds(ctx context.Context, userId uint64, feedIds []uint64) ([]*Collections, error)
		// UpdateStatusIfNewer 条件更新状态：仅当记录 updated_at <= eventTime 时生效，
		// 并把 updated_at 写为 eventTime（乱序兜底，见 07-mq-event.md §2.3）。
		UpdateStatusIfNewer(ctx context.Context, data *Collections, status int64, eventTime time.Time) (bool, error)
	}

	customCollectionsModel struct {
		*defaultCollectionsModel
	}
)

// NewCollectionsModel returns a model for the database table.
func NewCollectionsModel(conn sqlx.SqlConn, c cache.CacheConf, opts ...cache.Option) CollectionsModel {
	return &customCollectionsModel{
		defaultCollectionsModel: newCollectionsModel(conn, c, opts...),
	}
}

// CountByFeedId 统计帖子的有效收藏数（status=1）。
func (m *customCollectionsModel) CountByFeedId(ctx context.Context, feedId uint64) (int64, error) {
	var count int64
	query := fmt.Sprintf("select count(*) from %s where `feed_id` = ? and `status` = 1", m.table)
	err := m.QueryRowNoCacheCtx(ctx, &count, query, feedId)
	return count, err
}

// CountByFeedIds 批量统计多个帖子的有效收藏数。结果只包含有记录的 feedId。
func (m *customCollectionsModel) CountByFeedIds(ctx context.Context, feedIds []uint64) (map[uint64]int64, error) {
	result := make(map[uint64]int64, len(feedIds))
	if len(feedIds) == 0 {
		return result, nil
	}
	placeholders, args := buildInArgs(feedIds)
	query := fmt.Sprintf(
		"select `feed_id`, count(*) as cnt from %s where `feed_id` in (%s) and `status` = 1 group by `feed_id`",
		m.table, placeholders)

	var rows []struct {
		FeedId uint64 `db:"feed_id"`
		Cnt    int64  `db:"cnt"`
	}
	if err := m.QueryRowsNoCacheCtx(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	for _, r := range rows {
		result[r.FeedId] = r.Cnt
	}
	return result, nil
}

// FindUserIdsByFeedId 查询帖子的全部有效收藏用户 ID。
func (m *customCollectionsModel) FindUserIdsByFeedId(ctx context.Context, feedId uint64) ([]uint64, error) {
	var userIds []uint64
	query := fmt.Sprintf("select `user_id` from %s where `feed_id` = ? and `status` = 1", m.table)
	if err := m.QueryRowsNoCacheCtx(ctx, &userIds, query, feedId); err != nil {
		return nil, err
	}
	return userIds, nil
}

// FindValidByUserId 查询用户有效收藏记录，按收藏时间倒序。
func (m *customCollectionsModel) FindValidByUserId(ctx context.Context, userId uint64, limit int) ([]*Collections, error) {
	var rows []*Collections
	query := fmt.Sprintf(
		"select %s from %s where `user_id` = ? and `status` = 1 order by `created_at` desc, `id` desc limit ?",
		collectionsRows, m.table)
	if err := m.QueryRowsNoCacheCtx(ctx, &rows, query, userId, limit); err != nil {
		return nil, err
	}
	return rows, nil
}

// FindByUserIdFeedIds 批量查询用户对多个帖子的收藏记录（含已取消）。
func (m *customCollectionsModel) FindByUserIdFeedIds(ctx context.Context, userId uint64, feedIds []uint64) ([]*Collections, error) {
	if len(feedIds) == 0 {
		return nil, nil
	}
	placeholders, args := buildInArgs(feedIds)
	query := fmt.Sprintf("select %s from %s where `user_id` = ? and `feed_id` in (%s)",
		collectionsRows, m.table, placeholders)
	var rows []*Collections
	if err := m.QueryRowsNoCacheCtx(ctx, &rows, query, append([]any{userId}, args...)...); err != nil {
		return nil, err
	}
	return rows, nil
}

// UpdateStatusIfNewer 条件更新状态并同步失效 go-zero 行缓存。
func (m *customCollectionsModel) UpdateStatusIfNewer(ctx context.Context, data *Collections, status int64, eventTime time.Time) (bool, error) {
	idKey := fmt.Sprintf("%s%v", cacheCollectionsIdPrefix, data.Id)
	userFeedKey := fmt.Sprintf("%s%v:%v", cacheCollectionsUserIdFeedIdPrefix, data.UserId, data.FeedId)
	ret, err := m.ExecCtx(ctx, func(ctx context.Context, conn sqlx.SqlConn) (sql.Result, error) {
		query := fmt.Sprintf(
			"update %s set `status` = ?, `updated_at` = ? where `id` = ? and `updated_at` <= ?",
			m.table)
		return conn.ExecCtx(ctx, query, status, eventTime, data.Id, eventTime)
	}, idKey, userFeedKey)
	if err != nil {
		return false, err
	}
	affected, err := ret.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}
