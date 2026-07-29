// getFollowsLogic.go
//
// 职责：获取某用户的关注列表（分页）。
// 读取策略：优先读 Redis ZSet 缓存，缓存未命中或分页边界缺失时回源 MySQL，
// 并将结果回填到缓存。Redis 中按 created_at 倒序排，天然支持分页。
package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/relation/rpc/internal/svc"
	"github.com/sponge-dad/feed/app/relation/rpc/relation"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

// GetFollowsLogic 获取关注列表逻辑处理器。
type GetFollowsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

// NewGetFollowsLogic 构造 GetFollowsLogic 实例。
func NewGetFollowsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetFollowsLogic {
	return &GetFollowsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetFollows 获取用户关注列表。
func (l *GetFollowsLogic) GetFollows(in *relation.GetFollowsReq) (*relation.GetFollowsResp, error) {
	if in.UserId <= 0 {
		return nil, errorx.New(errorx.ParamError)
	}
	if in.Page <= 0 {
		in.Page = 1
	}
	if in.PageSize <= 0 {
		in.PageSize = 20
	}
	if in.PageSize > 100 {
		in.PageSize = 100
	}

	start := (in.Page - 1) * in.PageSize
	stop := start + in.PageSize - 1

	members, err := l.svcCtx.Redis.Zrevrange(followKey(in.UserId), int64(start), int64(stop))
	if err == nil && len(members) > 0 {
		// R-LS-02：Total 取缓存 ZSet 基数（全量计数），而非分页片段长度，
		// 保证网关展示的关注数与真实关注数一致。ZCARD 异常时回退为片段长度，避免错误放大。
		total := int64(len(members))
		if card, zerr := l.svcCtx.Redis.Zcard(followKey(in.UserId)); zerr == nil {
			total = int64(card)
		}
		return &relation.GetFollowsResp{
			FolloweeIds: parseIds(members),
			Total:       total,
		}, nil
	}

	records, err := l.svcCtx.RelationModel.FindByFollowerId(l.ctx, uint64(in.UserId), uint64(in.PageSize), uint64(start))
	if err != nil {
		l.Errorf("FindByFollowerId fail, userId=%d err=%v", in.UserId, err)
		return nil, err
	}

	ids := make([]int64, 0, len(records))
	for _, r := range records {
		ids = append(ids, int64(r.FolloweeId))
		if _, err := l.svcCtx.Redis.Zadd(followKey(in.UserId), r.CreatedAt, int64toa(int64(r.FolloweeId))); err != nil {
			l.Errorf("Zadd follow cache fail, userId=%d followee=%d err=%v", in.UserId, r.FolloweeId, err)
		}
	}

	return &relation.GetFollowsResp{
		FolloweeIds: ids,
		Total:       int64(len(ids)),
	}, nil
}

// parseIds 把 Redis 返回的字符串数组转成 int64 数组
func parseIds(members []string) []int64 {
	ids := make([]int64, 0, len(members))
	for _, m := range members {
		if v := parseInt64(m); v != 0 {
			ids = append(ids, v)
		}
	}
	return ids
}
