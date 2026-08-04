// getFansLogic.go
//
// 职责：获取某用户的粉丝列表（分页）。
// 逻辑和 GetFollows 对称，只是从 followee_id 查 follower_id。
package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/relation/rpc/internal/svc"
	"github.com/sponge-dad/feed/app/relation/rpc/relation"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

// GetFansLogic 获取粉丝列表逻辑处理器。
type GetFansLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

// NewGetFansLogic 构造 GetFansLogic 实例。
func NewGetFansLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetFansLogic {
	return &GetFansLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetFans 获取用户粉丝列表。
func (l *GetFansLogic) GetFans(in *relation.GetFansReq) (*relation.GetFansResp, error) {
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

	members, err := l.svcCtx.Redis.Zrevrange(fansKey(in.UserId), int64(start), int64(stop))
	if err == nil && len(members) > 0 {
		// R-LS-02：Total 取缓存 ZSet 基数（全量计数），而非分页片段长度，
		// 保证网关展示的粉丝数与真实粉丝数一致。ZCARD 异常时回退为片段长度。
		total := int64(len(members))
		if card, zerr := l.svcCtx.Redis.Zcard(fansKey(in.UserId)); zerr == nil {
			total = int64(card)
		}
		return &relation.GetFansResp{
			FollowerIds: parseIds(members),
			Total:       total,
		}, nil
	}

	records, err := l.svcCtx.RelationModel.FindByFolloweeId(l.ctx, uint64(in.UserId), uint64(in.PageSize), uint64(start))
	if err != nil {
		l.Errorf("FindByFolloweeId fail, userId=%d err=%v", in.UserId, err)
		return nil, err
	}

	ids := make([]int64, 0, len(records))
	for _, r := range records {
		ids = append(ids, int64(r.FollowerId))
		if _, err := l.svcCtx.Redis.Zadd(fansKey(in.UserId), r.CreatedAt, int64toa(int64(r.FollowerId))); err != nil {
			l.Errorf("Zadd fans cache fail, userId=%d follower=%d err=%v", in.UserId, r.FollowerId, err)
		}
	}

	return &relation.GetFansResp{
		FollowerIds: ids,
		Total:       int64(len(ids)),
	}, nil
}
