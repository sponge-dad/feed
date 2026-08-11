package model

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var ErrNotFound = sqlx.ErrNotFound

// 用户兴趣画像快照缓存键前缀（goctl model 生成约定）。
const (
	cacheUserInterestProfilesIdPrefix     = "cache:userInterestProfiles:id:"
	cacheUserInterestProfilesUserIdPrefix = "cache:userInterestProfiles:userId:"
)
