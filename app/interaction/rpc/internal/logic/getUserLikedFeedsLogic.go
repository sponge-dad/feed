// getUserLikedFeedsLogic.go
//
// 职责：查询用户点赞过的帖子列表（个人主页场景）。
// 基于 user:likes ZSet 游标分页，未命中时回源 MySQL 重建（最多 1000 条），
// 详见 docs/design/interaction/05-user-list.md。
package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetUserLikedFeedsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetUserLikedFeedsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserLikedFeedsLogic {
	return &GetUserLikedFeedsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetUserLikedFeeds 按点赞时间倒序分页返回帖子 ID；游标为 base64("score:feed_id")。
func (l *GetUserLikedFeedsLogic) GetUserLikedFeeds(in *interaction.GetUserLikedFeedsReq) (*interaction.GetUserLikedFeedsResp, error) {
	if in.GetUserId() <= 0 {
		return nil, errorx.New(errorx.ParamError)
	}
	res, err := newInteractHelper(l.ctx, l.svcCtx, kindLike).page(in.GetUserId(), in.GetPageSize(), in.GetCursor())
	if err != nil {
		if _, ok := errorx.TryParse(err); ok {
			return nil, err // 业务错误（如非法游标）原样上抛
		}
		l.Errorf("GetUserLikedFeeds: page failed user=%d err=%v", in.GetUserId(), err)
		return nil, errorx.New(errorx.ServerError)
	}
	return &interaction.GetUserLikedFeedsResp{
		FeedIds:    res.feedIDs,
		NextCursor: res.nextCursor,
		Total:      res.total,
	}, nil
}
