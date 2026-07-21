package logic

import (
	"context"
	"sync"

	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	relationClient "github.com/sponge-dad/feed/app/relation/rpc/relationclient"
	userClient "github.com/sponge-dad/feed/app/user/rpc/userClient"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetUserLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetUserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserLogic {
	return &GetUserLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetUserLogic) GetUser(req *types.GetUserReq) (*types.UserDetail, error) {
	// 1. 获取用户基础信息
	userResp, err := l.svcCtx.UserRpc.GetUser(l.ctx, &userClient.GetUserReq{
		UserId: req.UserID,
	})
	if err != nil {
		return nil, err
	}

	detail := userInfoToDetail(userResp.User)
	if detail == nil {
		return nil, nil
	}

	// 2. 并行聚合 Relation 数据（关注数、粉丝数、是否已关注）。
	//    聚合失败不影响主结果，仅日志记录。
	meID := middleware.MustGetUserID(l.ctx)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		resp, err := l.svcCtx.RelationRpc.GetFollows(l.ctx, &relationClient.GetFollowsReq{
			UserId:   req.UserID,
			Page:     1,
			PageSize: 1,
		})
		if err != nil {
			l.Infof("GetFollows failed: %v", err)
			return
		}
		detail.FollowingCount = resp.Total
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		resp, err := l.svcCtx.RelationRpc.GetFans(l.ctx, &relationClient.GetFansReq{
			UserId:   req.UserID,
			Page:     1,
			PageSize: 1,
		})
		if err != nil {
			l.Infof("GetFans failed: %v", err)
			return
		}
		detail.FollowerCount = resp.Total
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if meID == 0 {
			return
		}
		resp, err := l.svcCtx.RelationRpc.IsFollow(l.ctx, &relationClient.IsFollowReq{
			FollowerId:  meID,
			FolloweeIds: []int64{req.UserID},
		})
		if err != nil {
			l.Infof("IsFollow failed: %v", err)
			return
		}
		detail.IsFollowing = resp.Results[req.UserID]
	}()

	wg.Wait()
	return detail, nil
}
