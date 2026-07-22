// feedsModel.go
//
// 职责：扩展 feeds 表的自定义数据访问方法，提供个人主页、同城流、
// 批量详情查询与幂等软删除能力。列表查询只返回正常状态帖子，并统一按发布时间倒序。
package model

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ FeedsModel = (*customFeedsModel)(nil)

const (
	feedStatusNormal  int64 = 1
	feedStatusDeleted int64 = 2
)

type (
	// FeedsModel 是 feeds 表的 model 层接口，包含 goctl 生成的基础 CRUD 和手写扩展方法。
	FeedsModel interface {
		feedsModel
		// FindByUserId 分页查询指定用户发布的正常状态帖子。
		FindByUserId(ctx context.Context, userId, limit, offset uint64) ([]*Feeds, error)
		// FindByCityCode 分页查询指定城市发布的正常状态帖子。
		FindByCityCode(ctx context.Context, cityCode string, limit, offset uint64) ([]*Feeds, error)
		// FindByIds 批量查询正常状态帖子，避免上层循环单查造成 N+1 查询。
		FindByIds(ctx context.Context, ids []uint64) ([]*Feeds, error)
		// SoftDeleteByUserId 将指定用户的帖子标记为已删除，并使主键缓存失效。
		SoftDeleteByUserId(ctx context.Context, feedId, userId uint64) (bool, error)
	}

	customFeedsModel struct {
		*defaultFeedsModel
	}
)

// NewFeedsModel 创建 feeds 表的 model 实例。
func NewFeedsModel(conn sqlx.SqlConn, c cache.CacheConf, opts ...cache.Option) FeedsModel {
	return &customFeedsModel{
		defaultFeedsModel: newFeedsModel(conn, c, opts...),
	}
}

// FindByUserId 分页查询用户主页帖子，按发布时间和主键倒序保证稳定分页。
func (m *customFeedsModel) FindByUserId(ctx context.Context, userId, limit, offset uint64) ([]*Feeds, error) {
	query := fmt.Sprintf("select %s from %s where `user_id` = ? and `status` = ? order by `created_at` desc, `id` desc limit ? offset ?", feedsRows, m.table)
	var feeds []*Feeds
	err := m.QueryRowsNoCacheCtx(ctx, &feeds, query, userId, feedStatusNormal, limit, offset)
	return feeds, err
}

// FindByCityCode 分页查询同城帖子，按发布时间和主键倒序保证稳定分页。
func (m *customFeedsModel) FindByCityCode(ctx context.Context, cityCode string, limit, offset uint64) ([]*Feeds, error) {
	query := fmt.Sprintf("select %s from %s where `city_code` = ? and `status` = ? order by `created_at` desc, `id` desc limit ? offset ?", feedsRows, m.table)
	var feeds []*Feeds
	err := m.QueryRowsNoCacheCtx(ctx, &feeds, query, cityCode, feedStatusNormal, limit, offset)
	return feeds, err
}

// FindByIds 按 ID 列表批量查询正常状态帖子，一次 SQL 完成。
func (m *customFeedsModel) FindByIds(ctx context.Context, ids []uint64) ([]*Feeds, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	// 动态生成与 ID 数量一致的占位符，所有 ID 仍作为参数传入，避免 SQL 注入。
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	args = append(args, feedStatusNormal)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}

	query := fmt.Sprintf("select %s from %s where `status` = ? and `id` in (%s)", feedsRows, m.table, strings.Join(placeholders, ","))
	var feeds []*Feeds
	err := m.QueryRowsNoCacheCtx(ctx, &feeds, query, args...)
	return feeds, err
}

// SoftDeleteByUserId 将作者自己的帖子更新为已删除状态。
// SQL 同时校验 user_id，防止调用方遗漏权限条件；重复删除不会重复修改数据。
func (m *customFeedsModel) SoftDeleteByUserId(ctx context.Context, feedId, userId uint64) (bool, error) {
	feedsIdKey := fmt.Sprintf("%s%v", cacheFeedsIdPrefix, feedId)
	result, err := m.ExecCtx(ctx, func(ctx context.Context, conn sqlx.SqlConn) (result sql.Result, err error) {
		query := fmt.Sprintf("update %s set `status` = ? where `id` = ? and `user_id` = ? and `status` <> ?", m.table)
		return conn.ExecCtx(ctx, query, feedStatusDeleted, feedId, userId, feedStatusDeleted)
	}, feedsIdKey)
	if err != nil {
		return false, err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}
