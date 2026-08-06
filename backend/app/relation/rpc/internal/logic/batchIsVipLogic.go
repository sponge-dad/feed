// batchIsVipLogic.go
//
// 职责：批量判断一组用户是否是大V（粉丝数 >= 阈值）。
// 与单个 IsVip 的判定口径完全一致（Redis Set → fans_count 缓存 → MySQL 回源），
// 区别在于每一层都是批量操作：
//  1. 一次 pipeline 批量 SISMEMBER 查 `user:vip_users`
//  2. 未命中的一次 MGET 查 `user:fans_count:{id}`
//  3. 仍未命中的一次 SQL group by 回源，并回填缓存
//
// 提供该接口的原因：Feed 关注流需要在关注列表（最多数千人）里挑出大V，
// 逐个调 IsVip 会产生 N 次 RPC + N 次 Redis 往返（N+1 问题），
// 改为一次 BatchIsVip 后固定为 1 次 RPC + 常数次 Redis/DB 往返。
package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/relation/rpc/internal/svc"
	"github.com/sponge-dad/feed/app/relation/rpc/relation"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

// batchIsVipMaxSize 单次批量判定的用户数上限（去重后）。
// 与 Feed 侧关注列表拉取上限（5000）对齐，防止调用方传入超大列表打爆 Redis/DB。
const batchIsVipMaxSize = 5000

// BatchIsVipLogic 批量大V判定逻辑处理器。
type BatchIsVipLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

// NewBatchIsVipLogic 构造 BatchIsVipLogic 实例。
func NewBatchIsVipLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BatchIsVipLogic {
	return &BatchIsVipLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// BatchIsVip 批量判断用户是否是大V。
// 入参中的每个合法 ID 都会出现在返回的 map 里；重复 ID 只判定一次。
func (l *BatchIsVipLogic) BatchIsVip(in *relation.BatchIsVipReq) (*relation.BatchIsVipResp, error) {
	ids, err := normalizeUserIds(in.UserIds)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return &relation.BatchIsVipResp{Results: map[int64]bool{}}, nil
	}

	results := make(map[int64]bool, len(ids))

	// 第一层：批量查大V集合，命中即为大V。
	missed := l.lookupVipSet(ids, results)
	if len(missed) == 0 {
		return &relation.BatchIsVipResp{Results: results}, nil
	}

	// 第二层：未命中的批量查粉丝数缓存。
	missed = l.lookupFansCountCache(missed, results)
	if len(missed) == 0 {
		return &relation.BatchIsVipResp{Results: results}, nil
	}

	// 第三层：缓存完全缺失的回源 MySQL，一次 SQL 统计并回填缓存。
	if err := l.rebuildFromDB(missed, results); err != nil {
		l.Errorf("BatchIsVip rebuildFromDB failed, size=%d err=%v", len(missed), err)
		return nil, err
	}

	return &relation.BatchIsVipResp{Results: results}, nil
}

// normalizeUserIds 校验并去重用户ID，保持入参顺序。
func normalizeUserIds(userIds []int64) ([]int64, error) {
	if len(userIds) > batchIsVipMaxSize {
		return nil, errorx.New(errorx.ParamError)
	}

	ids := make([]int64, 0, len(userIds))
	seen := make(map[int64]struct{}, len(userIds))
	for _, id := range userIds {
		if id <= 0 {
			return nil, errorx.New(errorx.ParamError)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
}

// lookupVipSet 用一次 pipeline 批量 SISMEMBER 判定大V集合，命中的写入 results，
// 返回未命中需要继续下探的 ID。Redis 异常时不阻断，全部下探到下一层。
func (l *BatchIsVipLogic) lookupVipSet(ids []int64, results map[int64]bool) []int64 {
	// 只依赖 Result() 方法，避免 logic 层直接依赖 go-redis 的命令类型。
	cmds := make([]interface{ Result() (bool, error) }, 0, len(ids))
	err := l.svcCtx.Redis.PipelinedCtx(l.ctx, func(pipe redis.Pipeliner) error {
		for _, id := range ids {
			cmds = append(cmds, pipe.SIsMember(l.ctx, redisKeyVipUsers, int64toa(id)))
		}
		return nil
	})
	if err != nil {
		l.Errorf("BatchIsVip pipeline sismember fail, size=%d err=%v", len(ids), err)
		return ids
	}

	missed := make([]int64, 0, len(ids))
	for i, id := range ids {
		isMember, cmdErr := cmds[i].Result()
		if cmdErr == nil && isMember {
			results[id] = true
			continue
		}
		missed = append(missed, id)
	}
	return missed
}

// lookupFansCountCache 用一次 MGET 批量读粉丝数缓存并按阈值判定，
// 判定为大V的补写回 `user:vip_users`；返回缓存缺失需要回源的 ID。
func (l *BatchIsVipLogic) lookupFansCountCache(ids []int64, results map[int64]bool) []int64 {
	keys := make([]string, 0, len(ids))
	for _, id := range ids {
		keys = append(keys, fansCountKey(id))
	}

	values, err := l.svcCtx.Redis.MgetCtx(l.ctx, keys...)
	if err != nil || len(values) != len(ids) {
		if err != nil {
			l.Errorf("BatchIsVip mget fans count fail, size=%d err=%v", len(ids), err)
		}
		return ids
	}

	missed := make([]int64, 0, len(ids))
	promoted := make([]any, 0)
	for i, id := range ids {
		if values[i] == "" {
			missed = append(missed, id)
			continue
		}
		isVip := parseInt64(values[i]) >= l.svcCtx.Config.Vip.FansThreshold
		results[id] = isVip
		if isVip {
			promoted = append(promoted, int64toa(id))
		}
	}
	l.promoteVips(promoted)
	return missed
}

// rebuildFromDB 一次 SQL 批量统计粉丝数，回填 fans_count 缓存与大V集合。
func (l *BatchIsVipLogic) rebuildFromDB(ids []int64, results map[int64]bool) error {
	followeeIds := make([]uint64, 0, len(ids))
	for _, id := range ids {
		followeeIds = append(followeeIds, uint64(id))
	}

	counts, err := l.svcCtx.RelationModel.CountByFolloweeIds(l.ctx, followeeIds)
	if err != nil {
		return err
	}

	promoted := make([]any, 0)
	for _, id := range ids {
		// map 中不存在表示该用户没有任何粉丝记录，粉丝数按 0 处理。
		count := counts[uint64(id)]
		if err := l.svcCtx.Redis.SetCtx(l.ctx, fansCountKey(id), int64toa(count)); err != nil {
			l.Errorf("BatchIsVip set fans count cache fail, userId=%d err=%v", id, err)
		}

		isVip := count >= l.svcCtx.Config.Vip.FansThreshold
		results[id] = isVip
		if isVip {
			promoted = append(promoted, int64toa(id))
		}
	}
	l.promoteVips(promoted)
	return nil
}

// promoteVips 把新判定出的大V一次性补写进 `user:vip_users`，写失败只记日志不影响判定结果。
func (l *BatchIsVipLogic) promoteVips(members []any) {
	if len(members) == 0 {
		return
	}
	if _, err := l.svcCtx.Redis.SaddCtx(l.ctx, redisKeyVipUsers, members...); err != nil {
		l.Errorf("BatchIsVip sadd vip set fail, size=%d err=%v", len(members), err)
	}
}
