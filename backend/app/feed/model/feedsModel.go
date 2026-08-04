// feedsModel.go
//
// 职责：扩展 feeds 表的自定义数据访问方法，提供个人主页、同城流、
// 批量详情查询与幂等软删除能力。列表查询只返回正常状态帖子，并统一按发布时间倒序。
package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlc"
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
		// IncrCommentCount 增量更新某帖的评论数镜像列（由 comment-event 消费者按 CREATE +1 / DELETE -1 调用）。
		// delta 可正可负；SQL 层保证下限为 0（GREATEST(comment_count + delta, 0)），不会出现负数。
		IncrCommentCount(ctx context.Context, feedID uint64, delta int64) error
	}

	customFeedsModel struct {
		*defaultFeedsModel
		// rds 用于维护业务详情缓存 feed:{feed_id} 的失效（cache-aside）。
		rds redisCache
	}
)

// redisCache 抽象详情缓存删除依赖，便于单测注入 mock。
// 与 go-zero 自带主键缓存解耦：feed 详情缓存统一由业务层 Redis 的 feed:{feed_id} Hash 管理。
type redisCache interface {
	Del(keys ...string) (int, error)
}

// NewFeedsModel 创建 feeds 表的 model 实例。
// conn 为 MySQL 连接；rds 为业务 Redis（详情缓存 feed:{feed_id} 的失效依赖）。
// go-zero 自带主键缓存已关闭（注入直通 no-op cache），详情缓存统一走业务 Hash。
// 注意：不能传空 cache.CacheConf 给 sqlc.NewConn，会触发 "no cache nodes" fatal。
func NewFeedsModel(conn sqlx.SqlConn, rds redisCache, _ ...cache.Option) FeedsModel {
	return &customFeedsModel{
		defaultFeedsModel: &defaultFeedsModel{
			CachedConn: sqlc.NewConnWithCache(conn, noopCache{}),
			table:      "`feeds`",
		},
		rds: rds,
	}
}

// noopCache 是直通缓存实现：所有读操作直接回源 DB，写操作为空操作。
// 用于关闭 goctl 生成 model 的内置主键缓存，同时满足 CachedConn 对 cache.Cache 的依赖。
type noopCache struct{}

// Del 空操作，无内置缓存可删。
func (noopCache) Del(...string) error { return nil }

// DelCtx 空操作，无内置缓存可删。
func (noopCache) DelCtx(context.Context, ...string) error { return nil }

// Get 恒返回未命中。
func (noopCache) Get(string, any) error { return sql.ErrNoRows }

// GetCtx 恒返回未命中。
func (noopCache) GetCtx(context.Context, string, any) error { return sql.ErrNoRows }

// IsNotFound 判断是否为未命中错误。
func (noopCache) IsNotFound(err error) bool { return errors.Is(err, sql.ErrNoRows) }

// Set 空操作，不写缓存。
func (noopCache) Set(string, any) error { return nil }

// SetCtx 空操作，不写缓存。
func (noopCache) SetCtx(context.Context, string, any) error { return nil }

// SetWithExpire 空操作，不写缓存。
func (noopCache) SetWithExpire(string, any, time.Duration) error { return nil }

// SetWithExpireCtx 空操作，不写缓存。
func (noopCache) SetWithExpireCtx(context.Context, string, any, time.Duration) error { return nil }

// Take 直接回源查询。
func (noopCache) Take(val any, _ string, query func(any) error) error { return query(val) }

// TakeCtx 直接回源查询。
func (noopCache) TakeCtx(_ context.Context, val any, _ string, query func(any) error) error {
	return query(val)
}

// TakeWithExpire 直接回源查询。
func (noopCache) TakeWithExpire(val any, _ string, query func(any, time.Duration) error) error {
	return query(val, time.Minute)
}

// TakeWithExpireCtx 直接回源查询。
func (noopCache) TakeWithExpireCtx(_ context.Context, val any, _ string, query func(any, time.Duration) error) error {
	return query(val, time.Minute)
}

var _ cache.Cache = noopCache{}

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

// SoftDeleteByUserId 将作者自己的帖子标记为已删除，并使业务详情缓存失效。
// SQL 同时校验 user_id，防止调用方遗漏权限条件；重复删除不会重复修改数据。
// 软删除后删除业务详情缓存 feed:{feed_id}（Hash），由 GetFeed 下次读取时回源重建。
func (m *customFeedsModel) SoftDeleteByUserId(ctx context.Context, feedId, userId uint64) (bool, error) {
	feedDetailKey := fmt.Sprintf("feed:%d", feedId)
	result, err := m.ExecCtx(ctx, func(ctx context.Context, conn sqlx.SqlConn) (result sql.Result, err error) {
		query := fmt.Sprintf("update %s set `status` = ? where `id` = ? and `user_id` = ? and `status` <> ?", m.table)
		return conn.ExecCtx(ctx, query, feedStatusDeleted, feedId, userId, feedStatusDeleted)
	})
	if err != nil {
		return false, err
	}

	// 失效业务详情缓存：缓存删除失败不阻塞软删除主流程，仅记录日志。
	if m.rds != nil {
		if _, derr := m.rds.Del(feedDetailKey); derr != nil {
			logx.Errorf("del feed detail cache failed key=%s err=%v", feedDetailKey, derr)
		}
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// IncrCommentCount 增量更新 feeds.comment_count 镜像列。
// 由 Feed Worker 消费 comment-event 时按 action_type 调用：CREATE +1，DELETE -1。
// SQL 使用 GREATEST(comment_count + delta, 0) 保证计数不会因异常 DELETE 事件变为负数。
func (m *customFeedsModel) IncrCommentCount(ctx context.Context, feedID uint64, delta int64) error {
	query := fmt.Sprintf("update %s set `comment_count` = GREATEST(`comment_count` + ?, 0) where `id` = ?", m.table)
	_, err := m.ExecCtx(ctx, func(ctx context.Context, conn sqlx.SqlConn) (result sql.Result, err error) {
		return conn.ExecCtx(ctx, query, delta, feedID)
	})
	return err
}
