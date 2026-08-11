// 用户兴趣画像快照自定义扩展：UpsertWithVersion（version 单调递增，防并发回退）。
package model

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ UserInterestProfilesModel = (*customUserInterestProfilesModel)(nil)

type (
	UserInterestProfilesModel interface {
		userInterestProfilesModel
		// UpsertWithVersion 按 user_id 幂等写入快照，version 单调递增：
		// 并发写入时旧快照（更小 version）不会回退覆盖新快照。
		UpsertWithVersion(ctx context.Context, data *UserInterestProfiles) error
	}

	customUserInterestProfilesModel struct {
		*defaultUserInterestProfilesModel
	}
)

func NewUserInterestProfilesModel(conn sqlx.SqlConn, c cache.CacheConf, opts ...cache.Option) UserInterestProfilesModel {
	return &customUserInterestProfilesModel{
		defaultUserInterestProfilesModel: newUserInterestProfilesModel(conn, c, opts...),
	}
}

// UpsertWithVersion 冲突时取「两者较大 version + 1」作为新 version：
// 即使两个实例并发写同一用户，也保证 version 单调递增，旧快照无法覆盖新数据。
func (m *customUserInterestProfilesModel) UpsertWithVersion(ctx context.Context, data *UserInterestProfiles) error {
	userInterestProfilesUserIdKey := fmt.Sprintf("%s%v", cacheUserInterestProfilesUserIdPrefix, data.UserId)
	query := fmt.Sprintf(`insert into %s
		(user_id, interest_json, version, calculated_at, created_at, updated_at)
		values (?, ?, ?, ?, now(), now())
		on duplicate key update
			interest_json = values(interest_json),
			version = if(values(version) > version, values(version), version + 1),
			calculated_at = values(calculated_at),
			updated_at = now()`, m.table)

	_, err := m.ExecCtx(ctx, func(ctx context.Context, conn sqlx.SqlConn) (sql.Result, error) {
		return conn.ExecCtx(ctx, query,
			data.UserId, data.InterestJson, data.Version, time.Now().Truncate(time.Millisecond))
	}, userInterestProfilesUserIdKey)
	return err
}
