// likesModel.go
//
// 职责：likes 表自定义扩展查询。
// goctl 生成的基础 CRUD 见 likesModel_gen.go（DO NOT EDIT），
// 这里补充计数统计、缓存重建回源、批量状态查询与带时间条件的状态更新。
// 所有 SQL 均使用参数绑定，禁止字符串拼接值，防止 SQL 注入。
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

var _ LikesModel = (*customLikesModel)(nil)

type (
	// LikesModel is an interface to be customized, add more methods here,
	// and implement the added methods in customLikesModel.
	LikesModel interface {
		likesModel
		// CountByFeedId 统计帖子的有效点赞数（status=1）。
		CountByFeedId(ctx context.Context, feedId uint64) (int64, error)
		// CountByFeedIds 批量统计多个帖子的有效点赞数，返回 feedId -> count。
		CountByFeedIds(ctx context.Context, feedIds []uint64) (map[uint64]int64, error)
		// FindUserIdsByFeedId 查询帖子的全部有效点赞用户 ID（用于重建 like:feed Set）。
		FindUserIdsByFeedId(ctx context.Context, feedId uint64) ([]uint64, error)
		// FindValidByUserId 查询用户有效点赞记录，按点赞时间倒序，最多 limit 条
		// （用于重建 user:likes ZSet）。
		FindValidByUserId(ctx context.Context, userId uint64, limit int) ([]*Likes, error)
		// FindByUserIdFeedIds 批量查询用户对多个帖子的点赞记录（含已取消）。
		FindByUserIdFeedIds(ctx context.Context, userId uint64, feedIds []uint64) ([]*Likes, error)
		// UpdateStatusIfNewer 条件更新状态：仅当记录 updated_at <= eventTime 时生效，
		// 并把 updated_at 写为 eventTime（乱序兜底，见 07-mq-event.md §2.3）。
		// 返回是否实际更新了行。
		UpdateStatusIfNewer(ctx context.Context, data *Likes, status int64, eventTime time.Time) (bool, error)
	}

	customLikesModel struct {
		*defaultLikesModel
	}
)

// NewLikesModel returns a model for the database table.
func NewLikesModel(conn sqlx.SqlConn, c cache.CacheConf, opts ...cache.Option) LikesModel {
	return &customLikesModel{
		defaultLikesModel: newLikesModel(conn, c, opts...),
	}
}

// CountByFeedId 统计帖子的有效点赞数（status=1）。
func (m *customLikesModel) CountByFeedId(ctx context.Context, feedId uint64) (int64, error) {
	var count int64
	query := fmt.Sprintf("select count(*) from %s where `feed_id` = ? and `status` = 1", m.table)
	err := m.QueryRowNoCacheCtx(ctx, &count, query, feedId)
	return count, err
}

// CountByFeedIds 批量统计多个帖子的有效点赞数。结果只包含有记录的 feedId。
func (m *customLikesModel) CountByFeedIds(ctx context.Context, feedIds []uint64) (map[uint64]int64, error) {
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

// FindUserIdsByFeedId 查询帖子的全部有效点赞用户 ID。
func (m *customLikesModel) FindUserIdsByFeedId(ctx context.Context, feedId uint64) ([]uint64, error) {
	var userIds []uint64
	query := fmt.Sprintf("select `user_id` from %s where `feed_id` = ? and `status` = 1", m.table)
	if err := m.QueryRowsNoCacheCtx(ctx, &userIds, query, feedId); err != nil {
		return nil, err
	}
	return userIds, nil
}

// FindValidByUserId 查询用户有效点赞记录，按点赞时间倒序。
func (m *customLikesModel) FindValidByUserId(ctx context.Context, userId uint64, limit int) ([]*Likes, error) {
	var rows []*Likes
	query := fmt.Sprintf(
		"select %s from %s where `user_id` = ? and `status` = 1 order by `created_at` desc, `id` desc limit ?",
		likesRows, m.table)
	if err := m.QueryRowsNoCacheCtx(ctx, &rows, query, userId, limit); err != nil {
		return nil, err
	}
	return rows, nil
}

// FindByUserIdFeedIds 批量查询用户对多个帖子的点赞记录（含已取消）。
func (m *customLikesModel) FindByUserIdFeedIds(ctx context.Context, userId uint64, feedIds []uint64) ([]*Likes, error) {
	if len(feedIds) == 0 {
		return nil, nil
	}
	placeholders, args := buildInArgs(feedIds)
	query := fmt.Sprintf("select %s from %s where `user_id` = ? and `feed_id` in (%s)",
		likesRows, m.table, placeholders)
	var rows []*Likes
	if err := m.QueryRowsNoCacheCtx(ctx, &rows, query, append([]any{userId}, args...)...); err != nil {
		return nil, err
	}
	return rows, nil
}

// UpdateStatusIfNewer 条件更新状态并同步失效 go-zero 行缓存。
func (m *customLikesModel) UpdateStatusIfNewer(ctx context.Context, data *Likes, status int64, eventTime time.Time) (bool, error) {
	likesIdKey := fmt.Sprintf("%s%v", cacheLikesIdPrefix, data.Id)
	likesUserIdFeedIdKey := fmt.Sprintf("%s%v:%v", cacheLikesUserIdFeedIdPrefix, data.UserId, data.FeedId)
	ret, err := m.ExecCtx(ctx, func(ctx context.Context, conn sqlx.SqlConn) (sql.Result, error) {
		query := fmt.Sprintf(
			"update %s set `status` = ?, `updated_at` = ? where `id` = ? and `updated_at` <= ?",
			m.table)
		return conn.ExecCtx(ctx, query, status, eventTime, data.Id, eventTime)
	}, likesIdKey, likesUserIdFeedIdKey)
	if err != nil {
		return false, err
	}
	affected, err := ret.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// buildInArgs 为 IN 查询构造占位符与参数列表（参数化，防注入）。
func buildInArgs(ids []uint64) (string, []any) {
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	return placeholders, args
}
