// batchGetUserInteractionStatusLogic.go
//
// 职责：批量查询当前用户对多条帖子的互动状态（Feed 流卡片场景）。
// pipeline 批量 SISMEMBER；未命中的 key 直接回源 MySQL 批量查询，
// 不做单成员回填（避免制造部分 Set 毒化其他用户的查询，偏离 04-stats.md §4.1 的说明见
// internal/logic/interactionHelper.go 文件头）。
package logic

import (
	"context"

	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

type BatchGetUserInteractionStatusLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewBatchGetUserInteractionStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BatchGetUserInteractionStatusLogic {
	return &BatchGetUserInteractionStatusLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// BatchGetUserInteractionStatus 批量查询互动状态，返回顺序与请求一致；单次上限 100 个。
func (l *BatchGetUserInteractionStatusLogic) BatchGetUserInteractionStatus(in *interaction.BatchGetUserInteractionStatusReq) (*interaction.BatchGetUserInteractionStatusResp, error) {
	if in.GetUserId() <= 0 {
		return nil, errorx.New(errorx.ParamError)
	}
	feedIDs := in.GetFeedIds()
	if len(feedIDs) == 0 {
		return &interaction.BatchGetUserInteractionStatusResp{}, nil
	}
	if len(feedIDs) > maxBatchSize {
		return nil, errorx.NewWithMsg(errorx.ParamError, "单次批量查询不超过 100 个帖子")
	}
	for _, id := range feedIDs {
		if id <= 0 {
			return nil, errorx.New(errorx.ParamError)
		}
	}

	likedMap, err := newInteractHelper(l.ctx, l.svcCtx, kindLike).batchMember(in.GetUserId(), feedIDs)
	if err != nil {
		l.Errorf("BatchGetUserInteractionStatus: batch like status failed user=%d err=%v", in.GetUserId(), err)
		return nil, errorx.New(errorx.ServerError)
	}
	collectedMap, err := newInteractHelper(l.ctx, l.svcCtx, kindCollect).batchMember(in.GetUserId(), feedIDs)
	if err != nil {
		l.Errorf("BatchGetUserInteractionStatus: batch collect status failed user=%d err=%v", in.GetUserId(), err)
		return nil, errorx.New(errorx.ServerError)
	}

	list := make([]*interaction.UserInteractionStatus, 0, len(feedIDs))
	for _, feedID := range feedIDs {
		list = append(list, &interaction.UserInteractionStatus{
			FeedId:      feedID,
			IsLiked:     likedMap[feedID],
			IsCollected: collectedMap[feedID],
		})
	}
	return &interaction.BatchGetUserInteractionStatusResp{StatusList: list}, nil
}
