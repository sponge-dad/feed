// getUserHandler.go
//
// 职责：获取用户信息（他人主页或当前登录用户主页）。
// 流程：调用 User.GetUser -> 并行调用 Relation 接口聚合 following_count / follower_count / is_following -> 返回。
package handler

import (
	"net/http"
	"sync"

	"github.com/sponge-dad/feed/app/gateway/internal/middleware"
	"github.com/sponge-dad/feed/app/gateway/internal/svc"
	"github.com/sponge-dad/feed/app/gateway/internal/types"
	relationClient "github.com/sponge-dad/feed/app/relation/rpc/relationclient"
	userClient "github.com/sponge-dad/feed/app/user/rpc/userClient"
	"github.com/sponge-dad/feed/common/errorx"
	"github.com/sponge-dad/feed/common/response"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// GetUserHandler 处理 GET /api/v1/users/{userId}。
func GetUserHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.GetUserReq
		if err := httpx.Parse(r, &req); err != nil {
			response.Error(r.Context(), w, errorx.ParamError, "请求参数解析失败")
			return
		}

		meID := middleware.UserIDFromContext(r.Context())

		// 1. 获取用户基础信息。
		userResp, err := svcCtx.UserClient.GetUser(r.Context(), &userClient.GetUserReq{
			UserId: req.UserID,
		})
		if err != nil {
			writeError(r.Context(), w, err)
			return
		}

		// 2. 并行聚合 Relation 数据。
		var followingCount, followerCount int64
		var isFollowing bool
		var wg sync.WaitGroup

		wg.Add(3)
		go func() {
			defer wg.Done()
			resp, err := svcCtx.RelationClient.GetFollows(r.Context(), &relationClient.GetFollowsReq{
				UserId:   req.UserID,
				Page:     1,
				PageSize: 1, // 只拿总数，不关心列表内容
			})
			if err != nil {
				logx.WithContext(r.Context()).Infof("GetFollows failed: %v", err)
				return
			}
			followingCount = resp.Total
		}()

		go func() {
			defer wg.Done()
			resp, err := svcCtx.RelationClient.GetFans(r.Context(), &relationClient.GetFansReq{
				UserId:   req.UserID,
				Page:     1,
				PageSize: 1,
			})
			if err != nil {
				logx.WithContext(r.Context()).Infof("GetFans failed: %v", err)
				return
			}
			followerCount = resp.Total
		}()

		go func() {
			defer wg.Done()
			// 未登录时跳过 is_following 查询。
			if meID == 0 {
				return
			}
			resp, err := svcCtx.RelationClient.IsFollow(r.Context(), &relationClient.IsFollowReq{
				FollowerId:  meID,
				FolloweeIds: []int64{req.UserID},
			})
			if err != nil {
				logx.WithContext(r.Context()).Infof("IsFollow failed: %v", err)
				return
			}
			isFollowing = resp.Results[req.UserID]
		}()

		wg.Wait()

		response.Success(r.Context(), w, &types.UserDetail{
			ID:             userResp.User.Id,
			Username:       userResp.User.Username,
			Nickname:       userResp.User.Nickname,
			Avatar:         userResp.User.Avatar,
			Bio:            userResp.User.Bio,
			CityName:       userResp.User.CityName,
			FollowingCount: followingCount,
			FollowerCount:  followerCount,
			FeedCount:      0, // Feed 服务尚未实现，暂固定为 0
			IsFollowing:    isFollowing,
		})
	}
}

// GetMeHandler 处理 GET /api/v1/users/me。
func GetMeHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		meID := middleware.UserIDFromContext(r.Context())
		if meID == 0 {
			response.HTTPError(r.Context(), w, http.StatusUnauthorized, errorx.Unauthorized, "未登录")
			return
		}

		userResp, err := svcCtx.UserClient.GetUser(r.Context(), &userClient.GetUserReq{
			UserId: meID,
		})
		if err != nil {
			writeError(r.Context(), w, err)
			return
		}

		response.Success(r.Context(), w, &types.UserDetail{
			ID:             userResp.User.Id,
			Username:       userResp.User.Username,
			Nickname:       userResp.User.Nickname,
			Avatar:         userResp.User.Avatar,
			Bio:            userResp.User.Bio,
			CityName:       userResp.User.CityName,
			FollowingCount: 0,
			FollowerCount:  0,
			FeedCount:      0,
			IsFollowing:    false,
		})
	}
}
