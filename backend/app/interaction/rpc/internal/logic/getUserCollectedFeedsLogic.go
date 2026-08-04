// getUserCollectedFeedsLogic.go
//
// 职责：查询用户收藏过的帖子列表，与点赞列表同构（基于 user:collects ZSet 游标分页），
// 详见 docs/design/interaction/05-user-list.md。
package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetUserCollectedFeedsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetUserCollectedFeedsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserCollectedFeedsLogic {
	return &GetUserCollectedFeedsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetUserCollectedFeeds 按收藏时间倒序分页返回帖子 ID；游标为 base64("score:feed_id")。
func (l *GetUserCollectedFeedsLogic) GetUserCollectedFeeds(in *interaction.GetUserCollectedFeedsReq) (*interaction.GetUserCollectedFeedsResp, error) {
	if in.GetUserId() <= 0 {
		return nil, errorx.New(errorx.ParamError)
	}
	res, err := newInteractHelper(l.ctx, l.svcCtx, kindCollect).page(in.GetUserId(), in.GetPageSize(), in.GetCursor())
	if err != nil {
		if _, ok := errorx.TryParse(err); ok {
			return nil, err
		}
		l.Errorf("GetUserCollectedFeeds: page failed user=%d err=%v", in.GetUserId(), err)
		return nil, errorx.New(errorx.ServerError)
	}
	return &interaction.GetUserCollectedFeedsResp{
		FeedIds:    res.feedIDs,
		NextCursor: res.nextCursor,
		Total:      res.total,
	}, nil
}
