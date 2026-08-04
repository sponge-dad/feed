package logic

import (
	"context"
	"encoding/json"

	"github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"
	feedEvent "github.com/sponge-dad/feed/common/event/feed"

	"github.com/zeromicro/go-zero/core/logx"
)

const feedStatusDeleted = int64(2)

type DeleteFeedLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDeleteFeedLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteFeedLogic {
	return &DeleteFeedLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// DeleteFeed 删除帖子（软删除）
func (l *DeleteFeedLogic) DeleteFeed(in *feed.DeleteFeedReq) (*feed.DeleteFeedResp, error) {
	// 参数校验
	if in.FeedId <= 0 || in.UserId <= 0 {
		return nil, errorx.New(errorx.ParamError)
	}
	// 查询帖子存在
	existing, err := l.svcCtx.FeedModel.FindOne(l.ctx, uint64(in.FeedId))
	if err != nil {
		return nil, err
	}
	// 权限校验
	if existing.UserId != uint64(in.UserId) {
		return nil, errorx.New(errorx.FeedNoPermission)
	}
	// 幂等：已删除则直接返回成功，避免重发事件
	if existing.Status == feedStatusDeleted {
		return &feed.DeleteFeedResp{Success: true}, nil
	}
	// 软删除
	existing.Status = feedStatusDeleted
	res, err := l.svcCtx.FeedModel.SoftDeleteByUserId(l.ctx, existing.Id, existing.UserId)
	if err != nil {
		l.Errorf("soft delete feed failed feedId=%d userId=%d err=%v", in.FeedId, in.UserId, err)
		return nil, err
	}
	if !res {
		return &feed.DeleteFeedResp{Success: true}, nil
	}
	// 发送消息
	event := feedEvent.NewEventFeedDeleted(in.FeedId, in.UserId, existing.IsVipFeed == 1, existing.CityCode)
	body, err := json.Marshal(event)
	if err != nil {
		l.Errorf("marshal feed.deleted event failed feedId=%d err=%v", in.FeedId, err)
		return nil, err
	} else if err = l.svcCtx.Producer.SendSync(feedEvent.TopicFeedDeleted, body); err != nil {
		l.Errorf("send feed.deleted to MQ failed feedId=%d err=%v", in.FeedId, err)
	}
	return &feed.DeleteFeedResp{Success: true}, nil
}
