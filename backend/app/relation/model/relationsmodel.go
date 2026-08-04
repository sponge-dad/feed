// relationsmodel.go
//
// 职责：Relation 服务的数据访问层封装。goctl 自动生成了基础 CRUD 和
// FindOneByFollowerIdFolloweeId（唯一索引），这里手动扩展
// FindByFollowerId / FindByFolloweeId 两个分页查询方法，用于支持
// GetFollows / GetFans 两个列表接口。
package model

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ RelationsModel = (*customRelationsModel)(nil)

type (
	// RelationsModel 是 model 层的对外接口，goctl 生成的基础方法 + 手动扩展方法都在这里。
	RelationsModel interface {
		relationsModel
		// FindByFollowerId 查询某用户关注列表（按关注时间倒序）。
		// 用于 GetFollows 接口，避免在 logic 里拼 SQL。
		FindByFollowerId(ctx context.Context, followerId uint64, limit, offset uint64) ([]*Relations, error)
		// FindByFolloweeId 查询某用户粉丝列表（按关注时间倒序）。
		// 用于 GetFans 接口。
		FindByFolloweeId(ctx context.Context, followeeId uint64, limit, offset uint64) ([]*Relations, error)
		// CountByFolloweeId 查询某用户的粉丝总数。
		// 用于 IsVip 回源重建粉丝数，避免受分页 1000 条限制。
		CountByFolloweeId(ctx context.Context, followeeId uint64) (int64, error)
	}

	customRelationsModel struct {
		*defaultRelationsModel
	}
)

// NewRelationsModel 创建 relations 表的 model 实例。
func NewRelationsModel(conn sqlx.SqlConn, c cache.CacheConf, opts ...cache.Option) RelationsModel {
	return &customRelationsModel{
		defaultRelationsModel: newRelationsModel(conn, c, opts...),
	}
}

// FindByFollowerId 按 follower_id 分页查询关注关系，按 created_at 倒序。
// 返回的是关系记录，logic 层再提取 followee_id 列表。
func (m *customRelationsModel) FindByFollowerId(ctx context.Context, followerId uint64, limit, offset uint64) ([]*Relations, error) {
	query := fmt.Sprintf("select %s from %s where `follower_id` = ? order by `created_at` desc limit ? offset ?", relationsRows, m.table)
	var resp []*Relations
	err := m.QueryRowsNoCacheCtx(ctx, &resp, query, followerId, limit, offset)
	return resp, err
}

// FindByFolloweeId 按 followee_id 分页查询粉丝关系，按 created_at 倒序。
func (m *customRelationsModel) FindByFolloweeId(ctx context.Context, followeeId uint64, limit, offset uint64) ([]*Relations, error) {
	query := fmt.Sprintf("select %s from %s where `followee_id` = ? order by `created_at` desc limit ? offset ?", relationsRows, m.table)
	var resp []*Relations
	err := m.QueryRowsNoCacheCtx(ctx, &resp, query, followeeId, limit, offset)
	return resp, err
}

// CountByFolloweeId 按 followee_id 统计粉丝总数。
func (m *customRelationsModel) CountByFolloweeId(ctx context.Context, followeeId uint64) (int64, error) {
	query := fmt.Sprintf("select count(*) from %s where `followee_id` = ?", m.table)
	var count int64
	err := m.QueryRowNoCacheCtx(ctx, &count, query, followeeId)
	return count, err
}

// Insert 重写生成版本的 Insert，显式写入 created_at。
// goctl 生成的 relationsRowsExpectAutoSet 会排除 created_at，但本表需要应用层写入时间戳。
func (m *customRelationsModel) Insert(ctx context.Context, data *Relations) (sql.Result, error) {
	relationsFollowerIdFolloweeIdKey := fmt.Sprintf("%s%v:%v", cacheRelationsFollowerIdFolloweeIdPrefix, data.FollowerId, data.FolloweeId)
	relationsIdKey := fmt.Sprintf("%s%v", cacheRelationsIdPrefix, data.Id)
	query := fmt.Sprintf("insert into %s (`id`,`follower_id`,`followee_id`,`created_at`) values (?, ?, ?, ?)", m.table)
	return m.ExecCtx(ctx, func(ctx context.Context, conn sqlx.SqlConn) (sql.Result, error) {
		return conn.ExecCtx(ctx, query, data.Id, data.FollowerId, data.FolloweeId, data.CreatedAt)
	}, relationsFollowerIdFolloweeIdKey, relationsIdKey)
}
