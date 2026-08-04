// usersmodel.go
//
// 职责：users 表的自定义扩展点。
// goctl model mysql ddl 每次重新生成时只会覆盖 usersmodel_gen.go，
// 这个文件不会被覆盖，所以自定义查询方法（不由唯一索引自动生成的那种）要写在这里。
//
// 当前 FindOneByUsername / FindOneByPhone / FindOneByEmail 已经由 goctl
// 根据 user.sql 中的 UNIQUE KEY 自动生成在 usersmodel_gen.go 里，不需要在此手写。
//
// FindByIds 是本文件手写补充的批量查询方法：goctl 默认只生成单条查询（FindOne），
// 但 BatchGetUsers 这种场景必须一次性按 IN 查询多条，避免在 logic 层写 for 循环
// 挨个调用 FindOne 造成 N+1 查询（并发量大时会直接打爆数据库连接池）。
package model

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ UsersModel = (*customUsersModel)(nil)

type (
	// UsersModel is an interface to be customized, add more methods here,
	// and implement the added methods in customUsersModel.
	UsersModel interface {
		usersModel
		// FindByIds 按主键批量查询，一次 SQL 走 IN 查询，不走 model 内置的按主键缓存
		// （批量场景命中率通常不高，缓存意义有限，具体的业务缓存策略交给 logic 层决定）。
		FindByIds(ctx context.Context, ids []int64) ([]*Users, error)
	}

	customUsersModel struct {
		*defaultUsersModel
	}
)

// NewUsersModel returns a model for the database table.
func NewUsersModel(conn sqlx.SqlConn, c cache.CacheConf, opts ...cache.Option) UsersModel {
	return &customUsersModel{
		defaultUsersModel: newUsersModel(conn, c, opts...),
	}
}

// FindByIds 按 ID 列表批量查询用户，一次 SQL 完成，不循环单查。
func (m *customUsersModel) FindByIds(ctx context.Context, ids []int64) ([]*Users, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	// 拼 IN (?,?,?...) 占位符，参数化传值，避免 SQL 注入。
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf("select %s from %s where `id` in (%s)",
		usersRows, m.table, strings.Join(placeholders, ","))

	var resp []*Users
	// 用 QueryRowsNoCacheCtx：批量查询不走 model 内置缓存（缓存是按单条主键维度设计的，
	// 不适合批量场景），缓存策略交给上层 logic 用 Redis MGET/批量回写自行实现。
	err := m.QueryRowsNoCacheCtx(ctx, &resp, query, args...)
	if err != nil {
		return nil, err
	}
	return resp, nil
}
