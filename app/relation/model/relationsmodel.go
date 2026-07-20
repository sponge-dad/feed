// relationsmodel.go
//
// 职责：Relation 服务的数据访问层封装。goctl 自动生成了基础 CRUD 和
// FindOneByFollowerIdFolloweeId（唯一索引），这里手动扩展
// FindByFollowerId / FindByFolloweeId 两个分页查询方法，用于支持
// GetFollows / GetFans 两个列表接口。
package model

import (
	"context"
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
