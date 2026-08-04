// internal/logic/keys.go
//
// 职责：Relation 服务业务级 Redis 缓存的 key 命名和常量定义。
// 所有 key 前缀集中在这里管理，避免各 logic 文件里硬编码不一致。
package logic

import (
	"fmt"
	"strconv"
)

// int64toa int64 转字符串（Redis member/key 用）
func int64toa(v int64) string {
	return strconv.FormatInt(v, 10)
}

const (
	// redisKeyFollow 关注列表缓存：ZSet，score=关注时间，member=被关注者ID
	redisKeyFollow = "user:follow:%d"
	// redisKeyFans 粉丝列表缓存：ZSet，score=关注时间，member=关注者ID
	redisKeyFans = "user:fans:%d"
	// redisKeyFansCount 粉丝数量缓存
	redisKeyFansCount = "user:fans_count:%d"
	// redisKeyVipUsers 大V用户集合：Set
	redisKeyVipUsers = "user:vip_users"
)

// followKey 返回某用户的关注列表 key
func followKey(userId int64) string {
	return fmt.Sprintf(redisKeyFollow, userId)
}

// fansKey 返回某用户的粉丝列表 key
func fansKey(userId int64) string {
	return fmt.Sprintf(redisKeyFans, userId)
}

// fansCountKey 返回某用户的粉丝数 key
func fansCountKey(userId int64) string {
	return fmt.Sprintf(redisKeyFansCount, userId)
}

// parseInt64 把字符串 member 解析成 int64，logic 层批量处理缓存 member 时用
func parseInt64(s string) int64 {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return v
}
