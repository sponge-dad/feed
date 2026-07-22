package model

import (
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ FeedsModel = (*customFeedsModel)(nil)

type (
	// FeedsModel is an interface to be customized, add more methods here,
	// and implement the added methods in customFeedsModel.
	FeedsModel interface {
		feedsModel
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
