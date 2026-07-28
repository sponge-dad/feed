// Package logic 的单元测试共享脚手架：miniredis + model/user/feed 桩。
// 不依赖真实 MySQL / RocketMQ / User / Feed 服务，专注业务逻辑分支覆盖。
package logic

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/sponge-dad/feed/app/comment/model"
	"github.com/sponge-dad/feed/app/comment/rpc/internal/svc"
	"github.com/sponge-dad/feed/app/feed/rpc/feedclient"
	"github.com/sponge-dad/feed/app/user/rpc/user"
	"github.com/sponge-dad/feed/app/user/rpc/userClient"
	"github.com/sponge-dad/feed/common/errorx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

// stubCommentsModel 是 model.CommentsModel 的内存桩实现，模拟软删/计数联动语义。
type stubCommentsModel struct {
	mu               sync.Mutex
	byID             map[uint64]*model.Comments
	countByFeedCalls int
}

func newStubCommentsModel() *stubCommentsModel {
	return &stubCommentsModel{byID: make(map[uint64]*model.Comments)}
}

func (s *stubCommentsModel) Insert(_ context.Context, data *model.Comments) (sql.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[data.Id] = data
	return nil, nil
}

func (s *stubCommentsModel) FindOne(_ context.Context, id uint64) (*model.Comments, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.byID[id]; ok {
		cp := *c
		return &cp, nil
	}
	return nil, model.ErrNotFound
}

func (s *stubCommentsModel) Update(_ context.Context, data *model.Comments) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[data.Id] = data
	return nil
}

func (s *stubCommentsModel) Delete(_ context.Context, id uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byID, id)
	return nil
}

// InsertComment 模拟事务写入：子回复联动根评论 reply_count+1，根不可见返回 ErrRootUnavailable。
func (s *stubCommentsModel) InsertComment(_ context.Context, data *model.Comments) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if data.RootId != 0 {
		root, ok := s.byID[data.RootId]
		if !ok || root.Status != model.CommentStatusNormal {
			return model.ErrRootUnavailable
		}
		root.ReplyCount++
	}
	cp := *data
	s.byID[data.Id] = &cp
	return nil
}

// SoftDelete 模拟事务软删：幂等 + 计数减量语义与真实实现一致。
func (s *stubCommentsModel) SoftDelete(_ context.Context, comment *model.Comments) (bool, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.byID[comment.Id]
	if !ok || stored.Status != model.CommentStatusNormal {
		return false, 0, nil
	}

	var visibleReplies int64
	if stored.RootId == 0 {
		for _, c := range s.byID {
			if c.RootId == stored.Id && c.Status == model.CommentStatusNormal {
				visibleReplies++
			}
		}
	}

	stored.Status = model.CommentStatusDeleted
	if stored.RootId == 0 {
		return true, 1 + visibleReplies, nil
	}
	if root, ok := s.byID[stored.RootId]; ok && root.Status == model.CommentStatusNormal && root.ReplyCount > 0 {
		root.ReplyCount--
	}
	return true, 1, nil
}

func (s *stubCommentsModel) FindRootsByFeedId(_ context.Context, feedId, limit, offset uint64) ([]*model.Comments, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var roots []*model.Comments
	for _, c := range s.byID {
		if c.FeedId == feedId && c.RootId == 0 && c.Status == model.CommentStatusNormal {
			cp := *c
			roots = append(roots, &cp)
		}
	}
	// 时间倒序，id 倒序兜底
	sort.Slice(roots, func(i, j int) bool {
		if !roots[i].CreatedAt.Equal(roots[j].CreatedAt) {
			return roots[i].CreatedAt.After(roots[j].CreatedAt)
		}
		return roots[i].Id > roots[j].Id
	})
	if offset >= uint64(len(roots)) {
		return nil, nil
	}
	end := offset + limit
	if end > uint64(len(roots)) {
		end = uint64(len(roots))
	}
	return roots[offset:end], nil
}

func (s *stubCommentsModel) FindPreviewsByRootIds(_ context.Context, rootIds []uint64, previewCount uint64) ([]*model.Comments, error) {
	var out []*model.Comments
	for _, rootID := range rootIds {
		replies := s.visibleRepliesAsc(rootID)
		if uint64(len(replies)) > previewCount {
			replies = replies[:previewCount]
		}
		out = append(out, replies...)
	}
	return out, nil
}

func (s *stubCommentsModel) FindRepliesByCursor(_ context.Context, rootId uint64, cursorCreatedAt time.Time, cursorId, limit uint64) ([]*model.Comments, error) {
	replies := s.visibleRepliesAsc(rootId)
	var out []*model.Comments
	for _, r := range replies {
		if r.CreatedAt.After(cursorCreatedAt) || (r.CreatedAt.Equal(cursorCreatedAt) && r.Id > cursorId) {
			out = append(out, r)
		}
		if uint64(len(out)) >= limit {
			break
		}
	}
	return out, nil
}

func (s *stubCommentsModel) CountByFeedId(_ context.Context, feedId uint64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.countByFeedCalls++
	var count int64
	for _, c := range s.byID {
		if c.FeedId == feedId && s.visibleLocked(c) {
			count++
		}
	}
	return count, nil
}

func (s *stubCommentsModel) CountByFeedIds(_ context.Context, feedIds []uint64) (map[uint64]int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	counts := make(map[uint64]int64)
	for _, c := range s.byID {
		if !s.visibleLocked(c) {
			continue
		}
		for _, feedID := range feedIds {
			if c.FeedId == feedID {
				counts[feedID]++
			}
		}
	}
	return counts, nil
}

// visibleLocked 与真实计数口径一致：自身可见，且（一级评论 或 根评论仍可见）。
// 调用方需已持有 s.mu。
func (s *stubCommentsModel) visibleLocked(c *model.Comments) bool {
	if c.Status != model.CommentStatusNormal {
		return false
	}
	if c.RootId == 0 {
		return true
	}
	root, ok := s.byID[c.RootId]
	return ok && root.Status == model.CommentStatusNormal
}

func (s *stubCommentsModel) CountRepliesByRootId(_ context.Context, rootId uint64) (int64, error) {
	return int64(len(s.visibleRepliesAsc(rootId))), nil
}

func (s *stubCommentsModel) FindTopRootsByLike(_ context.Context, feedId, limit uint64) ([]*model.Comments, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var roots []*model.Comments
	for _, c := range s.byID {
		if c.FeedId == feedId && c.RootId == 0 && c.Status == model.CommentStatusNormal {
			cp := *c
			roots = append(roots, &cp)
		}
	}
	sort.Slice(roots, func(i, j int) bool {
		if roots[i].LikeCount != roots[j].LikeCount {
			return roots[i].LikeCount > roots[j].LikeCount
		}
		return roots[i].Id > roots[j].Id
	})
	if uint64(len(roots)) > limit {
		roots = roots[:limit]
	}
	return roots, nil
}

func (s *stubCommentsModel) FindByIds(_ context.Context, ids []uint64) ([]*model.Comments, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*model.Comments
	for _, id := range ids {
		if c, ok := s.byID[id]; ok && c.Status == model.CommentStatusNormal {
			cp := *c
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (s *stubCommentsModel) UpdateLikeCount(_ context.Context, id, likeCount uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.byID[id]; ok {
		c.LikeCount = likeCount
	}
	return nil
}

// visibleRepliesAsc 返回某楼可见子回复，时间正序、id 正序兜底。
func (s *stubCommentsModel) visibleRepliesAsc(rootID uint64) []*model.Comments {
	s.mu.Lock()
	defer s.mu.Unlock()
	var replies []*model.Comments
	for _, c := range s.byID {
		if c.RootId == rootID && c.Status == model.CommentStatusNormal {
			cp := *c
			replies = append(replies, &cp)
		}
	}
	sort.Slice(replies, func(i, j int) bool {
		if !replies[i].CreatedAt.Equal(replies[j].CreatedAt) {
			return replies[i].CreatedAt.Before(replies[j].CreatedAt)
		}
		return replies[i].Id < replies[j].Id
	})
	return replies
}

// stubUserRpc 是 userClient.User 的桩，仅实现 BatchGetUsers，并统计调用次数以断言无 N+1。
type stubUserRpc struct {
	userClient.User
	users     map[int64]*user.UserBrief
	calls     int
	returnErr error
}

func (s *stubUserRpc) BatchGetUsers(_ context.Context, in *user.BatchGetUsersReq, _ ...grpc.CallOption) (*user.BatchGetUsersResp, error) {
	s.calls++
	if s.returnErr != nil {
		return nil, s.returnErr
	}
	resp := &user.BatchGetUsersResp{}
	for _, id := range in.UserIds {
		if u, ok := s.users[id]; ok {
			resp.Users = append(resp.Users, u)
		}
	}
	return resp, nil
}

// stubFeedRpc 是 feedclient.Feed 的桩，仅实现帖子存在性校验所需的 GetFeed。
type stubFeedRpc struct {
	feedclient.Feed
	existing map[int64]bool
}

func (s *stubFeedRpc) GetFeed(_ context.Context, in *feedclient.GetFeedReq, _ ...grpc.CallOption) (*feedclient.GetFeedResp, error) {
	if s.existing[in.FeedId] {
		return &feedclient.GetFeedResp{}, nil
	}
	return nil, errorx.New(errorx.FeedNotFound)
}

// newTestSvc 构造带 miniredis 与桩依赖的 ServiceContext；Producer 留空（发送逻辑 nil 安全）。
func newTestSvc(t *testing.T, m model.CommentsModel, u userClient.User, f feedclient.Feed) *svc.ServiceContext {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	rdb := redis.MustNewRedis(redis.RedisConf{Type: "node", Host: mr.Addr()})

	var nextID int64 = 1000
	return &svc.ServiceContext{
		Redis:        rdb,
		CommentModel: m,
		UserRpc:      u,
		FeedRpc:      f,
		IdGen: func() int64 {
			nextID++
			return nextID
		},
	}
}

// mkComment 构造一条测试评论记录。
func mkComment(id, feedID, userID, rootID, parentID uint64, created time.Time) *model.Comments {
	return &model.Comments{
		Id:        id,
		FeedId:    feedID,
		UserId:    userID,
		Content:   fmt.Sprintf("c-%d", id),
		RootId:    rootID,
		ParentId:  parentID,
		Status:    model.CommentStatusNormal,
		CreatedAt: created,
	}
}

// requireBizCode 断言错误为指定业务错误码。
func requireBizCode(t *testing.T, err error, code int) {
	t.Helper()
	require.Error(t, err)
	codeErr, ok := errorx.TryParse(err)
	require.True(t, ok, "expect errorx.CodeError, got %v", err)
	require.Equal(t, code, codeErr.Code)
}
