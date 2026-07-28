// createfeedlogic.go
//
// 职责：发布帖子。网关根据请求 IP 解析城市（发帖场景解析失败降级为空，不阻塞发布），
// 转发 Feed.CreateFeed。
package feed

import (
	"context"

	feedClient "github.com/sponge-dad/feed/app/feed/rpc/feedclient"
	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	"github.com/sponge-dad/feed/common/errorx"
	"github.com/sponge-dad/feed/common/ipx"

	"github.com/zeromicro/go-zero/core/logx"
)

type CreateFeedLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreateFeedLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateFeedLogic {
	return &CreateFeedLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// CreateFeed 发布帖子。
func (l *CreateFeedLogic) CreateFeed(req *types.CreateFeedReq) (*types.CreateFeedResp, error) {
	me := middleware.MustGetUserID(l.ctx)
	if me == 0 {
		return nil, errorx.New(errorx.Unauthorized)
	}
	if req.FeedType != 1 && req.FeedType != 2 {
		return nil, errorx.New(errorx.FeedBadType)
	}
	if len(req.MediaUrls) == 0 {
		return nil, errorx.New(errorx.FeedEmptyMedia)
	}

	// IP 属地解析：发帖场景失败不阻塞，降级为空并记录日志。
	var cityCode, cityName, ipLocation string
	if loc, err := l.svcCtx.IPResolver.Resolve(ipx.ClientIPFromContext(l.ctx)); err != nil {
		l.Infof("feed: resolve city for create feed degrade to empty: %v", err)
	} else {
		cityCode, cityName, ipLocation = loc.CityCode, loc.CityName, loc.Province
	}

	rpcResp, err := l.svcCtx.FeedRpc.CreateFeed(l.ctx, &feedClient.CreateFeedReq{
		AuthorId:    me,
		FeedType:    req.FeedType,
		Title:       req.Title,
		Description: req.Description,
		MediaUrls:   req.MediaUrls,
		CoverUrl:    req.CoverURL,
		CityCode:    cityCode,
		CityName:    cityName,
		IpLocation:  ipLocation,
	})
	if err != nil {
		return nil, err
	}

	f := rpcResp.Feed
	return &types.CreateFeedResp{
		Feed: types.FeedInfo{
			ID:          f.FeedId,
			UserID:      f.AuthorId,
			FeedType:    f.FeedType,
			Title:       f.Title,
			Description: f.Description,
			MediaUrls:   f.MediaUrls,
			CoverURL:    f.CoverUrl,
			CityName:    f.CityName,
			IPLocation:  f.IpLocation,
			CreatedAt:   f.CreatedAt,
		},
	}, nil
}
