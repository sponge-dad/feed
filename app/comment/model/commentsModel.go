// commentsModel.go
//
// 职责：扩展 comments 表的自定义数据访问方法，提供楼中楼两层平铺存储所需的
// 事务写入（评论 + 根评论 reply_count 联动）、幂等软删除、一级评论分页、
// 子回复批量预览（防 N+1）、游标分页与计数查询能力。
// 评论内容不走缓存（见 docs/design/comment/06-cache.md），内容读一律 MySQL，
// 因此关闭 go-zero 自带主键缓存（传空 cache.CacheConf）。
package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/stores/sqlc"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ CommentsModel = (*customCommentsModel)(nil)

// 评论状态常量，与 comments.status 字段对应。
const (
	CommentStatusNormal  int64 = 1
	CommentStatusDeleted int64 = 2
)

// ErrRootUnavailable 表示事务内联动根评论 reply_count 时发现根评论已被删除。
// 调用方应将其映射为「父评论不存在」业务错误。
var ErrRootUnavailable = errors.New("root comment unavailable")

type (
	// CommentsModel 是 comments 表的 model 层接口，包含 goctl 生成的基础 CRUD 和手写扩展方法。
	CommentsModel interface {
		commentsModel
		// InsertComment 在单事务内写入评论；若为子回复（RootId!=0），同事务内对根评论 reply_count 原子 +1。
		InsertComment(ctx context.Context, data *Comments) error
		// SoftDelete 幂等软删除评论并回填计数，返回 (是否本次删除, comment_count 应减量)。
		SoftDelete(ctx context.Context, comment *Comments) (bool, int64, error)
		// FindRootsByFeedId 分页查询帖子下正常状态的一级评论，时间倒序。
		FindRootsByFeedId(ctx context.Context, feedId, limit, offset uint64) ([]*Comments, error)
		// FindPreviewsByRootIds 一条 SQL 批量取多个楼层的前 N 条可见子回复（时间正序），避免逐楼 N+1。
		FindPreviewsByRootIds(ctx context.Context, rootIds []uint64, previewCount uint64) ([]*Comments, error)
		// FindRepliesByCursor 游标分页查询某楼可见子回复，时间正序，(created_at, id) 组合游标。
		FindRepliesByCursor(ctx context.Context, rootId uint64, cursorCreatedAt time.Time, cursorId, limit uint64) ([]*Comments, error)
		// CountByFeedId 统计帖子下可见评论总数（一级 + 子回复），用于 comment_count 缓存重建。
		CountByFeedId(ctx context.Context, feedId uint64) (int64, error)
		// CountByFeedIds 批量统计多个帖子的可见评论总数，一条 GROUP BY SQL 完成。
		CountByFeedIds(ctx context.Context, feedIds []uint64) (map[uint64]int64, error)
		// CountRepliesByRootId 统计某楼可见子回复数（根自身除外）。
		CountRepliesByRootId(ctx context.Context, rootId uint64) (int64, error)
		// FindTopRootsByLike 按 like_count 降序取帖子下 Top-K 一级评论，用于 comment_hot 重建。
		FindTopRootsByLike(ctx context.Context, feedId, limit uint64) ([]*Comments, error)
		// FindByIds 一条 IN 查询批量取可见评论，避免逐条 FindOne 的 N+1。
		FindByIds(ctx context.Context, ids []uint64) ([]*Comments, error)
		// UpdateLikeCount 覆盖更新评论点赞数（Interaction 服务事件同步专用）。
		UpdateLikeCount(ctx context.Context, id, likeCount uint64) error
	}

	customCommentsModel struct {
		*defaultCommentsModel
	}
)

// NewCommentsModel 创建 comments 表的 model 实例。
// 评论内容不缓存（内容读一律 MySQL，见 06-cache.md），因此用 no-op cache
// 关闭 go-zero 自带主键缓存（不能传空 cache.CacheConf，其会 log.Fatal）。
func NewCommentsModel(conn sqlx.SqlConn) CommentsModel {
	return &customCommentsModel{
		defaultCommentsModel: &defaultCommentsModel{
			CachedConn: sqlc.NewConnWithCache(conn, noopCache{}),
			table:      "`comments`",
		},
	}
}

// noopCache 是 cache.Cache 的空实现：Get 恒未命中、Set/Del 恒成功，
// 使 goctl 生成的带缓存 CRUD 退化为纯 DB 直查。
type noopCache struct{}

func (noopCache) Del(_ ...string) error                           { return nil }
func (noopCache) DelCtx(_ context.Context, _ ...string) error     { return nil }
func (noopCache) Get(_ string, _ any) error                       { return sql.ErrNoRows }
func (noopCache) GetCtx(_ context.Context, _ string, _ any) error { return sql.ErrNoRows }
func (noopCache) IsNotFound(err error) bool                       { return errors.Is(err, sql.ErrNoRows) }
func (noopCache) Set(_ string, _ any) error                       { return nil }
func (noopCache) SetCtx(_ context.Context, _ string, _ any) error { return nil }
func (noopCache) SetWithExpire(_ string, _ any, _ time.Duration) error {
	return nil
}
func (noopCache) SetWithExpireCtx(_ context.Context, _ string, _ any, _ time.Duration) error {
	return nil
}
func (noopCache) Take(val any, _ string, query func(val any) error) error {
	return query(val)
}
func (noopCache) TakeCtx(_ context.Context, val any, _ string, query func(val any) error) error {
	return query(val)
}
func (noopCache) TakeWithExpire(val any, _ string, query func(val any, expire time.Duration) error) error {
	return query(val, 0)
}
func (noopCache) TakeWithExpireCtx(_ context.Context, val any, _ string,
	query func(val any, expire time.Duration) error) error {
	return query(val, 0)
}

// InsertComment 单事务写入评论。
// 子回复场景对根评论 reply_count 用 UPDATE ... + 1 原子自增（不先读后写，避免计数竞态）；
// 若根评论已被并发删除（受影响 0 行），回滚并返回 ErrRootUnavailable。
func (m *customCommentsModel) InsertComment(ctx context.Context, data *Comments) error {
	return m.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		insertQuery := fmt.Sprintf("insert into %s (%s) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", m.table, commentsRowsExpectAutoSet)
		if _, err := session.ExecCtx(ctx, insertQuery, data.Id, data.FeedId, data.UserId, data.Content,
			data.RootId, data.ParentId, data.ReplyUserId, data.LikeCount, data.ReplyCount, data.Status); err != nil {
			return err
		}
		if data.RootId == 0 {
			return nil
		}
		incrQuery := fmt.Sprintf("update %s set `reply_count` = `reply_count` + 1 where `id` = ? and `status` = ?", m.table)
		result, err := session.ExecCtx(ctx, incrQuery, data.RootId, CommentStatusNormal)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return ErrRootUnavailable
		}
		return nil
	})
}

// SoftDelete 幂等软删除。
// 用 UPDATE ... WHERE status=1 的受影响行数做最后一道幂等：0 行说明已被删除，返回 (false, 0, nil)。
// 删子回复时同事务内根评论 reply_count - 1；删根评论时先统计楼内可见子回复数，减量 = 1 + 子回复数。
func (m *customCommentsModel) SoftDelete(ctx context.Context, comment *Comments) (bool, int64, error) {
	var deleted bool
	var decrement int64
	err := m.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		// 删根评论时先在事务内统计楼内可见子回复数，作为 comment_count 减量的一部分。
		var visibleReplies int64
		if comment.RootId == 0 {
			countQuery := fmt.Sprintf("select count(*) from %s where `root_id` = ? and `status` = ?", m.table)
			if err := session.QueryRowCtx(ctx, &visibleReplies, countQuery, comment.Id, CommentStatusNormal); err != nil {
				return err
			}
		}

		delQuery := fmt.Sprintf("update %s set `status` = ? where `id` = ? and `status` = ?", m.table)
		result, err := session.ExecCtx(ctx, delQuery, CommentStatusDeleted, comment.Id, CommentStatusNormal)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			// 已被并发删除，幂等返回，不重复减计数。
			return nil
		}
		deleted = true

		if comment.RootId == 0 {
			// 方案 A：仅软删根评论本身，子回复保留可见；comment_count 减量 = 1 + 楼内可见子回复数。
			decrement = 1 + visibleReplies
			return nil
		}

		// 删子回复：根评论 reply_count 原子 -1（根可能已删，0 行影响可接受，不视为错误）。
		decrement = 1
		decrQuery := fmt.Sprintf("update %s set `reply_count` = `reply_count` - 1 where `id` = ? and `status` = ? and `reply_count` > 0", m.table)
		if _, err := session.ExecCtx(ctx, decrQuery, comment.RootId, CommentStatusNormal); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return false, 0, err
	}
	return deleted, decrement, nil
}

// FindRootsByFeedId 分页查询一级评论，按创建时间和主键倒序保证稳定分页。
func (m *customCommentsModel) FindRootsByFeedId(ctx context.Context, feedId, limit, offset uint64) ([]*Comments, error) {
	query := fmt.Sprintf("select %s from %s where `feed_id` = ? and `root_id` = 0 and `status` = ? order by `created_at` desc, `id` desc limit ? offset ?", commentsRows, m.table)
	var comments []*Comments
	err := m.QueryRowsNoCacheCtx(ctx, &comments, query, feedId, CommentStatusNormal, limit, offset)
	return comments, err
}

// FindPreviewsByRootIds 用窗口函数一次取出多个楼层各自的前 N 条可见子回复（时间正序）。
// 所有 ID 均以占位符传参，避免 SQL 注入；调用方按 RootId 分组即可。
func (m *customCommentsModel) FindPreviewsByRootIds(ctx context.Context, rootIds []uint64, previewCount uint64) ([]*Comments, error) {
	if len(rootIds) == 0 || previewCount == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(rootIds))
	args := make([]any, 0, len(rootIds)+2)
	args = append(args, CommentStatusNormal)
	for i, id := range rootIds {
		placeholders[i] = "?"
		args = append(args, id)
	}
	args = append(args, previewCount)

	query := fmt.Sprintf(
		"select %s from (select %s, row_number() over (partition by `root_id` order by `created_at` asc, `id` asc) as `rn` from %s where `status` = ? and `root_id` in (%s)) t where t.`rn` <= ? order by `root_id`, `created_at` asc, `id` asc",
		commentsRows, commentsRows, m.table, strings.Join(placeholders, ","))
	var comments []*Comments
	err := m.QueryRowsNoCacheCtx(ctx, &comments, query, args...)
	return comments, err
}

// FindRepliesByCursor 游标分页查询楼内子回复。
// 游标为 (created_at, id) 组合，正序翻页避免漏评/重复；首页传零值游标。
func (m *customCommentsModel) FindRepliesByCursor(ctx context.Context, rootId uint64, cursorCreatedAt time.Time, cursorId, limit uint64) ([]*Comments, error) {
	query := fmt.Sprintf("select %s from %s where `root_id` = ? and `status` = ? and (`created_at` > ? or (`created_at` = ? and `id` > ?)) order by `created_at` asc, `id` asc limit ?", commentsRows, m.table)
	var comments []*Comments
	err := m.QueryRowsNoCacheCtx(ctx, &comments, query, rootId, CommentStatusNormal, cursorCreatedAt, cursorCreatedAt, cursorId, limit)
	return comments, err
}

// CountByFeedId 统计帖子下可见评论总数（一级 + 子回复）。
// 方案 A 下删根评论仅软删根本身、子回复保留 status=1 但整楼折叠不可见，
// 因此子回复必须排除「根已删」的楼，保证重建值与展示口径一致。
func (m *customCommentsModel) CountByFeedId(ctx context.Context, feedId uint64) (int64, error) {
	query := fmt.Sprintf(
		"select count(*) from %s c left join %s r on c.`root_id` = r.`id` where c.`feed_id` = ? and c.`status` = ? and (c.`root_id` = 0 or r.`status` = ?)",
		m.table, m.table)
	var count int64
	err := m.QueryRowNoCacheCtx(ctx, &count, query, feedId, CommentStatusNormal, CommentStatusNormal)
	return count, err
}

// feedCommentCount 是 CountByFeedIds 的 GROUP BY 扫描结构。
type feedCommentCount struct {
	FeedId uint64 `db:"feed_id"`
	Cnt    int64  `db:"cnt"`
}

// CountByFeedIds 一条 GROUP BY SQL 批量统计多个帖子的可见评论总数；无评论的帖子不在结果中。
// 口径与 CountByFeedId 一致：排除「根已删」楼内的子回复。
func (m *customCommentsModel) CountByFeedIds(ctx context.Context, feedIds []uint64) (map[uint64]int64, error) {
	if len(feedIds) == 0 {
		return map[uint64]int64{}, nil
	}

	placeholders := make([]string, len(feedIds))
	args := make([]any, 0, len(feedIds)+2)
	args = append(args, CommentStatusNormal)
	for i, id := range feedIds {
		placeholders[i] = "?"
		args = append(args, id)
	}
	args = append(args, CommentStatusNormal)

	query := fmt.Sprintf(
		"select c.`feed_id`, count(*) as `cnt` from %s c left join %s r on c.`root_id` = r.`id` where c.`status` = ? and c.`feed_id` in (%s) and (c.`root_id` = 0 or r.`status` = ?) group by c.`feed_id`",
		m.table, m.table, strings.Join(placeholders, ","))
	var rows []*feedCommentCount
	if err := m.QueryRowsNoCacheCtx(ctx, &rows, query, args...); err != nil {
		return nil, err
	}

	counts := make(map[uint64]int64, len(rows))
	for _, row := range rows {
		counts[row.FeedId] = row.Cnt
	}
	return counts, nil
}

// CountRepliesByRootId 统计某楼可见子回复数（根自身除外）。
func (m *customCommentsModel) CountRepliesByRootId(ctx context.Context, rootId uint64) (int64, error) {
	query := fmt.Sprintf("select count(*) from %s where `root_id` = ? and `status` = ?", m.table)
	var count int64
	err := m.QueryRowNoCacheCtx(ctx, &count, query, rootId, CommentStatusNormal)
	return count, err
}

// FindTopRootsByLike 按 like_count 降序取 Top-K 可见一级评论，用于 comment_hot ZSet 重建。
func (m *customCommentsModel) FindTopRootsByLike(ctx context.Context, feedId, limit uint64) ([]*Comments, error) {
	query := fmt.Sprintf("select %s from %s where `feed_id` = ? and `root_id` = 0 and `status` = ? order by `like_count` desc, `id` desc limit ?", commentsRows, m.table)
	var comments []*Comments
	err := m.QueryRowsNoCacheCtx(ctx, &comments, query, feedId, CommentStatusNormal, limit)
	return comments, err
}

// FindByIds 一条 IN 查询批量取可见评论（status=1），所有 ID 走占位符传参。
func (m *customCommentsModel) FindByIds(ctx context.Context, ids []uint64) ([]*Comments, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	args = append(args, CommentStatusNormal)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}

	query := fmt.Sprintf("select %s from %s where `status` = ? and `id` in (%s)", commentsRows, m.table, strings.Join(placeholders, ","))
	var comments []*Comments
	err := m.QueryRowsNoCacheCtx(ctx, &comments, query, args...)
	return comments, err
}

// UpdateLikeCount 覆盖更新评论点赞数；仅 Interaction 同步链路调用，Comment 自身不改点赞。
func (m *customCommentsModel) UpdateLikeCount(ctx context.Context, id, likeCount uint64) error {
	query := fmt.Sprintf("update %s set `like_count` = ? where `id` = ?", m.table)
	_, err := m.ExecNoCacheCtx(ctx, query, likeCount, id)
	return err
}
