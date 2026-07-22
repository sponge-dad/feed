// feedsModel_test.go
//
// 职责：验证 feeds 自定义 model 方法的 SQL 条件、分页参数、批量查询和缓存失效行为。
package model

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlc"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var feedColumns = []string{
	"id", "user_id", "feed_type", "title", "description", "media_urls", "cover_url",
	"city_code", "city_name", "ip_location", "status", "is_vip_feed", "like_count",
	"comment_count", "collect_count", "created_at", "updated_at",
}

func TestFindByUserId(t *testing.T) {
	model, mock, _ := newMockFeedsModel(t)
	query := fmt.Sprintf("select %s from `feeds` where `user_id` = ? and `status` = ? order by `created_at` desc, `id` desc limit ? offset ?", feedsRows)
	mock.ExpectQuery(regexp.QuoteMeta(query)).
		WithArgs(uint64(42), feedStatusNormal, uint64(20), uint64(40)).
		WillReturnRows(newFeedRows(1001, 42, "440300"))

	feeds, err := model.FindByUserId(context.Background(), 42, 20, 40)

	require.NoError(t, err)
	require.Len(t, feeds, 1)
	require.Equal(t, uint64(1001), feeds[0].Id)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFindByCityCodeUsesParameterizedFilter(t *testing.T) {
	model, mock, _ := newMockFeedsModel(t)
	cityCode := "440300' OR 1=1 --"
	query := fmt.Sprintf("select %s from `feeds` where `city_code` = ? and `status` = ? order by `created_at` desc, `id` desc limit ? offset ?", feedsRows)
	mock.ExpectQuery(regexp.QuoteMeta(query)).
		WithArgs(cityCode, feedStatusNormal, uint64(10), uint64(0)).
		WillReturnRows(newFeedRows(1002, 43, cityCode))

	feeds, err := model.FindByCityCode(context.Background(), cityCode, 10, 0)

	require.NoError(t, err)
	require.Len(t, feeds, 1)
	require.Equal(t, cityCode, feeds[0].CityCode)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFindByIds(t *testing.T) {
	t.Run("empty IDs skip database", func(t *testing.T) {
		model, mock, _ := newMockFeedsModel(t)

		feeds, err := model.FindByIds(context.Background(), nil)

		require.NoError(t, err)
		require.Nil(t, feeds)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("queries all IDs at once", func(t *testing.T) {
		model, mock, _ := newMockFeedsModel(t)
		query := fmt.Sprintf("select %s from `feeds` where `status` = ? and `id` in (?,?,?)", feedsRows)
		mock.ExpectQuery(regexp.QuoteMeta(query)).
			WithArgs(feedStatusNormal, uint64(1001), uint64(1002), uint64(1003)).
			WillReturnRows(newFeedRows(1001, 42, "440300").AddRow(feedRowValues(1002, 43, "310000")...))

		feeds, err := model.FindByIds(context.Background(), []uint64{1001, 1002, 1003})

		require.NoError(t, err)
		require.Len(t, feeds, 2)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestSoftDeleteByUserId(t *testing.T) {
	model, mock, cacheStub := newMockFeedsModel(t)
	query := "update `feeds` set `status` = ? where `id` = ? and `user_id` = ? and `status` <> ?"
	mock.ExpectExec(regexp.QuoteMeta(query)).
		WithArgs(feedStatusDeleted, uint64(1001), uint64(42), feedStatusDeleted).
		WillReturnResult(sqlmock.NewResult(0, 1))

	deleted, err := model.SoftDeleteByUserId(context.Background(), 1001, 42)

	require.NoError(t, err)
	require.True(t, deleted)
	require.Equal(t, []string{"cache:feeds:id:1001"}, cacheStub.deletedKeys)
	require.NoError(t, mock.ExpectationsWereMet())
}

func newMockFeedsModel(t *testing.T) (*customFeedsModel, sqlmock.Sqlmock, *recordingCache) {
	t.Helper()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
	})

	cacheStub := &recordingCache{}
	conn := sqlx.NewSqlConnFromDB(db)
	return &customFeedsModel{
		defaultFeedsModel: &defaultFeedsModel{
			CachedConn: sqlc.NewConnWithCache(conn, cacheStub),
			table:      "`feeds`",
		},
	}, mock, cacheStub
}

func newFeedRows(id, userId uint64, cityCode string) *sqlmock.Rows {
	return sqlmock.NewRows(feedColumns).AddRow(feedRowValues(id, userId, cityCode)...)
}

func feedRowValues(id, userId uint64, cityCode string) []driver.Value {
	now := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	return []driver.Value{
		id, userId, int64(1), "title", "description", `["https://example.com/image.jpg"]`, "",
		cityCode, "深圳", "广东", feedStatusNormal, int64(0), uint64(10), uint64(2), uint64(3), now, now,
	}
}

type recordingCache struct {
	deletedKeys []string
}

func (c *recordingCache) Del(keys ...string) error {
	return c.DelCtx(context.Background(), keys...)
}

func (c *recordingCache) DelCtx(_ context.Context, keys ...string) error {
	c.deletedKeys = append(c.deletedKeys, keys...)
	return nil
}

func (c *recordingCache) Get(string, any) error {
	return sql.ErrNoRows
}

func (c *recordingCache) GetCtx(context.Context, string, any) error {
	return sql.ErrNoRows
}

func (c *recordingCache) IsNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

func (c *recordingCache) Set(string, any) error {
	return nil
}

func (c *recordingCache) SetCtx(context.Context, string, any) error {
	return nil
}

func (c *recordingCache) SetWithExpire(string, any, time.Duration) error {
	return nil
}

func (c *recordingCache) SetWithExpireCtx(context.Context, string, any, time.Duration) error {
	return nil
}

func (c *recordingCache) Take(val any, _ string, query func(any) error) error {
	return query(val)
}

func (c *recordingCache) TakeCtx(_ context.Context, val any, _ string, query func(any) error) error {
	return query(val)
}

func (c *recordingCache) TakeWithExpire(val any, _ string, query func(any, time.Duration) error) error {
	return query(val, time.Minute)
}

func (c *recordingCache) TakeWithExpireCtx(_ context.Context, val any, _ string, query func(any, time.Duration) error) error {
	return query(val, time.Minute)
}

var _ cache.Cache = (*recordingCache)(nil)
