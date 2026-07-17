// batchGetUsersLogic.go
//
// 职责：批量获取用户简要信息，专供 Feed/Comment 等服务在网关聚合时调用，
// 避免下游服务展示"帖子作者信息"时对 User 服务发起 N 次单条查询（N+1 问题）。
//
// 缓存策略（区别于 UserModel 内置的单条缓存）：
//  1. 先用 Redis MGET 批量查一次业务级缓存 user:brief:{id}
//  2. 未命中的 ID 收集起来，一次 SQL（IN 查询）批量查 MySQL，绝不循环单查
//  3. 查到的结果批量回写缓存
//  4. 合并两部分结果返回
package logic

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/sponge-dad/feed/app/user/rpc/internal/svc"
	"github.com/sponge-dad/feed/app/user/rpc/user"

	"github.com/zeromicro/go-zero/core/logx"
)

// briefCacheTTLSeconds 批量简要信息缓存的过期时间，短一些即可，
// 因为昵称/头像变化频率低，即便缓存稍微不一致，展示上也不敏感。
const briefCacheTTLSeconds = 600 // 10分钟

type BatchGetUsersLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewBatchGetUsersLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BatchGetUsersLogic {
	return &BatchGetUsersLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// briefCacheKey 生成单个用户简要信息的缓存 key。
func briefCacheKey(id int64) string {
	return "user:brief:" + strconv.FormatInt(id, 10)
}

// BatchGetUsers 批量获取用户简要信息（id/nickname/avatar），供跨服务聚合展示作者用。
func (l *BatchGetUsersLogic) BatchGetUsers(in *user.BatchGetUsersReq) (*user.BatchGetUsersResp, error) {
	if len(in.UserIds) == 0 {
		return &user.BatchGetUsersResp{}, nil
	}

	// 1. 批量查缓存：一次 MGET 而不是循环 N 次 GET。
	keys := make([]string, len(in.UserIds))
	for i, id := range in.UserIds {
		keys[i] = briefCacheKey(id)
	}
	cachedVals, err := l.svcCtx.Redis.MgetCtx(l.ctx, keys...)
	if err != nil {
		// Redis 查询失败不阻断主流程，退化为全部走数据库查询。
		l.Logger.Errorf("batch get users mget cache failed: %v", err)
		cachedVals = make([]string, len(in.UserIds))
	}

	result := make(map[int64]*user.UserBrief, len(in.UserIds))
	var missIDs []int64
	for i, id := range in.UserIds {
		if i < len(cachedVals) && cachedVals[i] != "" {
			var brief user.UserBrief
			if jsonErr := json.Unmarshal([]byte(cachedVals[i]), &brief); jsonErr == nil {
				result[id] = &brief
				continue
			}
		}
		missIDs = append(missIDs, id)
	}

	// 2. 未命中的部分一次性批量查 MySQL（IN 查询），不写 for 循环单条查询。
	if len(missIDs) > 0 {
		users, err := l.svcCtx.UserModel.FindByIds(l.ctx, missIDs)
		if err != nil {
			return nil, err
		}

		// 3. 批量回写缓存。逐个 Setex 是操作 Redis（内存），
		//    和"循环查数据库"性质不同，不会造成数据库压力问题。
		for _, u := range users {
			brief := &user.UserBrief{
				Id:       u.Id,
				Nickname: u.Nickname,
				Avatar:   u.Avatar,
			}
			result[u.Id] = brief

			if data, jsonErr := json.Marshal(brief); jsonErr == nil {
				if setErr := l.svcCtx.Redis.SetexCtx(l.ctx, briefCacheKey(u.Id), string(data), briefCacheTTLSeconds); setErr != nil {
					l.Logger.Errorf("batch get users setex cache failed: %v", setErr)
				}
			}
		}
	}

	// 4. 按传入顺序组装返回，查不到的 ID（已注销/不存在）直接跳过。
	resp := &user.BatchGetUsersResp{
		Users: make([]*user.UserBrief, 0, len(in.UserIds)),
	}
	for _, id := range in.UserIds {
		if brief, ok := result[id]; ok {
			resp.Users = append(resp.Users, brief)
		}
	}
	return resp, nil
}
