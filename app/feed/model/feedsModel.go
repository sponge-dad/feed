package model

import (
	"context"
	"fmt"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ FeedsModel = (*customFeedsModel)(nil)

type (
	// FeedsModel is an interface to be customized, add more methods here,
	// and implement the added methods in customFeedsModel.
	FeedsModel interface {
		feedsModel
		FindByUserId(ctx context.Context, userId uint64) ([]*Feeds, error)
		FindByCityCode(ctx context.Context, cityCode string) ([]*Feeds, error)
	}

	customFeedsModel struct {
		*defaultFeedsModel
	}
)

// NewFeedsModel returns a model for the database table.
func NewFeedsModel(conn sqlx.SqlConn, c cache.CacheConf, opts ...cache.Option) FeedsModel {
	return &customFeedsModel{
		defaultFeedsModel: newFeedsModel(conn, c, opts...),
	}
}

func (m *customFeedsModel) FindByUserId(ctx context.Context, userId uint64) ([]*Feeds, error) {
	query := fmt.Sprintf("select %s from %s where user_id = ?", feedsRows, m.table)
	var feeds []*Feeds
	err := m.QueryRowsNoCacheCtx(ctx, &feeds, query, userId)
	return feeds, err
}

func (m *customFeedsModel) FindByCityCode(ctx context.Context, cityCode string) ([]*Feeds, error) {
	query := fmt.Sprintf("select %s from %s where city_code = ?", feedsRows, m.table)
	var feeds []*Feeds
	err := m.QueryRowsNoCacheCtx(ctx, &feeds, query, cityCode)
	return feeds, err
}
