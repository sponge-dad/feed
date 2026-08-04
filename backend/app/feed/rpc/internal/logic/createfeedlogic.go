package logic

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/sponge-dad/feed/app/feed/model"
	"github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/svc"
	"github.com/sponge-dad/feed/app/relation/rpc/relation"
	"github.com/sponge-dad/feed/common/errorx"
	feedEvent "github.com/sponge-dad/feed/common/event/feed"
	"github.com/sponge-dad/feed/common/requestid"

	"github.com/zeromicro/go-zero/core/logx"
)

type CreateFeedLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateFeedLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateFeedLogic {
	return &CreateFeedLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 创建帖子
func (l *CreateFeedLogic) CreateFeed(in *feed.CreateFeedReq) (*feed.CreateFeedResp, error) {
	// 参数校验
	if in.AuthorId <= 0 {
		return nil, errorx.New(errorx.ParamError)
	}
	if in.FeedType != int32(feed.FeedType_FEED_TYPE_IMAGE) && in.FeedType != int32(feed.FeedType_FEED_TYPE_VIDEO) {
		return nil, errorx.New(errorx.ParamError)
	}
	if in.Description == "" {
		return nil, errorx.New(errorx.FeedEmptyContent)
	}
	if len(in.MediaUrls) < 1 {
		return nil, errorx.New(errorx.FeedEmptyMedia)
	}
	// 封面图仅视频类型必填（见 feed.proto cover_url 注释）
	if in.FeedType == int32(feed.FeedType_FEED_TYPE_VIDEO) && in.CoverUrl == "" {
		return nil, errorx.New(errorx.FeedEmptyMedia)
	}
	// 入库
	mediaUrlsJSON, err := json.Marshal(in.MediaUrls)
	if err != nil {
		return nil, err
	}

	vip, err := l.svcCtx.RelationRpc.IsVip(l.ctx, &relation.IsVipReq{UserId: in.AuthorId})
	if err != nil {
		l.Errorf("rpc call failed: service=relation.rpc method=IsVip userId=%d err=%v", in.AuthorId, err)
		return nil, err
	}
	var isVip int64 = 0 // 默认普通人
	if vip.IsVip {
		isVip = 1
	}
	now := time.Now()
	feedID := l.svcCtx.IdGen()
	_, err = l.svcCtx.FeedModel.Insert(l.ctx, &model.Feeds{
		Id:          uint64(feedID),
		UserId:      uint64(in.AuthorId),
		FeedType:    int64(in.FeedType),
		Title:       in.Title,
		Description: in.Description,
		MediaUrls: sql.NullString{
			String: string(mediaUrlsJSON),
			Valid:  true,
		},
		CoverUrl: in.CoverUrl,
		// TODO city ip
		CityCode:     in.CityCode,
		CityName:     in.CityName,
		IpLocation:   in.IpLocation,
		Status:       1,
		IsVipFeed:    isVip,
		LikeCount:    0,
		CommentCount: 0,
		CollectCount: 0,
		CreatedAt:    now,
	})
	// TODO 高并发下唯一索引冲突视为已存在，返回成功以保证幂等。
	// 这里直接返回错误，等出现问题在修改
	if err != nil {
		return nil, err
	}
	// 发送event -> MQ
	event := feedEvent.NewEventFeedCreated(feedID, in.AuthorId, vip.IsVip, in.CityCode, now.UnixMilli(), requestid.FromContext(l.ctx))
	body, err := json.Marshal(event)
	if err != nil {
		l.Errorf("marshal feed.created event failed feedId=%d err=%v", feedID, err)
		return nil, err
	} else if err = l.svcCtx.Producer.SendSync(feedEvent.TopicFeedCreated, body); err != nil {
		// MQ 失败不阻塞主流程：记录日志，由 Worker 重试/本地消息表兜底。
		l.Errorf("send feed.created to MQ failed feedId=%d err=%v", feedID, err)
	}

	return &feed.CreateFeedResp{
		Feed: &feed.FeedInfo{
			FeedId:       feedID,
			AuthorId:     in.AuthorId,
			FeedType:     in.FeedType,
			Title:        in.Title,
			Description:  in.Description,
			MediaUrls:    in.MediaUrls,
			CoverUrl:     in.CoverUrl,
			CityCode:     in.CityCode,
			CityName:     in.CityName,
			IpLocation:   in.IpLocation,
			Status:       1,
			IsVipFeed:    vip.IsVip,
			LikeCount:    0,
			CommentCount: 0,
			CollectCount: 0,
			CreatedAt:    now.UnixMilli(),
			UpdatedAt:    now.UnixMilli(),
		},
	}, nil
}
