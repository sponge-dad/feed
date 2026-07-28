// Package logic 的单元测试共享脚手架：miniredis + model/relation 桩。
// 不依赖真实 MySQL / RocketMQ / Relation 服务，专注于业务逻辑分支覆盖。
package logic

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/sponge-dad/feed/app/feed/model"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/svc"
	"github.com/sponge-dad/feed/app/relation/rpc/relation"
	"github.com/sponge-dad/feed/app/relation/rpc/relationclient"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

// stubFeedsModel 是 model.FeedsModel 的内存桩实现，用于单元测试。
type stubFeedsModel struct {
	byID          map[uint64]*model.Feeds
	byUser        map[uint64][]*model.Feeds
	byCity        map[string][]*model.Feeds
	findOneCalls  int
	findByIDsCall int
}

func newStubFeedsModel() *stubFeedsModel {
	return &stubFeedsModel{
		byID:   make(map[uint64]*model.Feeds),
		byUser: make(map[uint64][]*model.Feeds),
		byCity: make(map[string][]*model.Feeds),
	}
}

func (s *stubFeedsModel) Insert(_ context.Context, data *model.Feeds) (sql.Result, error) {
	s.byID[data.Id] = data
	return nil, nil
}

func (s *stubFeedsModel) FindOne(_ context.Context, id uint64) (*model.Feeds, error) {
	s.findOneCalls++
	if f, ok := s.byID[id]; ok {
		return f, nil
	}
	return nil, model.ErrNotFound
}

func (s *stubFeedsModel) Update(_ context.Context, data *model.Feeds) error {
	s.byID[data.Id] = data
	return nil
}

func (s *stubFeedsModel) Delete(_ context.Context, id uint64) error {
	delete(s.byID, id)
	return nil
}

func (s *stubFeedsModel) FindByUserId(_ context.Context, userID, limit, offset uint64) ([]*model.Feeds, error) {
	list := s.byUser[userID]
	return slicePage(list, limit, offset), nil
}

func (s *stubFeedsModel) FindByCityCode(_ context.Context, cityCode string, limit, offset uint64) ([]*model.Feeds, error) {
	list := s.byCity[cityCode]
	return slicePage(list, limit, offset), nil
}

func (s *stubFeedsModel) FindByIds(_ context.Context, ids []uint64) ([]*model.Feeds, error) {
	s.findByIDsCall++
	out := make([]*model.Feeds, 0, len(ids))
	for _, id := range ids {
		if f, ok := s.byID[id]; ok {
			out = append(out, f)
		}
	}
	return out, nil
}

func (s *stubFeedsModel) SoftDeleteByUserId(_ context.Context, feedID, _ uint64) (bool, error) {
	delete(s.byID, feedID)
	return true, nil
}

// slicePage 按 offset/limit 截取（与 model 的 offset 分页语义一致）。
func slicePage(list []*model.Feeds, limit, offset uint64) []*model.Feeds {
	if offset >= uint64(len(list)) {
		return nil
	}
	end := offset + limit
	if end > uint64(len(list)) {
		end = uint64(len(list))
	}
	return list[offset:end]
}

// stubRelation 是 relationclient.Relation 的桩，仅实现关注流所需的 GetFollows / IsVip。
type stubRelation struct {
	relationclient.Relation
	followees []int64
	vips      map[int64]bool
}

func (s *stubRelation) GetFollows(_ context.Context, _ *relation.GetFollowsReq, _ ...grpc.CallOption) (*relation.GetFollowsResp, error) {
	return &relation.GetFollowsResp{FolloweeIds: s.followees}, nil
}

func (s *stubRelation) IsVip(_ context.Context, in *relation.IsVipReq, _ ...grpc.CallOption) (*relation.IsVipResp, error) {
	return &relation.IsVipResp{IsVip: s.vips[in.UserId]}, nil
}

// newTestSvc 构造带 miniredis 与桩依赖的 ServiceContext。
func newTestSvc(t *testing.T, m model.FeedsModel, r relationclient.Relation) *svc.ServiceContext {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	rdb := redis.MustNewRedis(redis.RedisConf{Type: "node", Host: mr.Addr()})
	return &svc.ServiceContext{
		Redis:       rdb,
		FeedModel:   m,
		RelationRpc: r,
	}
}

// mkFeed 构造一个用于测试的帖子实体。
func mkFeed(id, user uint64, created time.Time) *model.Feeds {
	return &model.Feeds{
		Id:           id,
		UserId:       user,
		FeedType:     1,
		Title:        fmt.Sprintf("t-%d", id),
		CityCode:     "440300",
		CityName:     "深圳",
		IsVipFeed:    0,
		Status:       1,
		LikeCount:    0,
		CommentCount: 0,
		CollectCount: 0,
		CreatedAt:    created,
		UpdatedAt:    created,
		MediaUrls:    sql.NullString{},
	}
}

// zadd 向 miniredis 写入 ZSet 成员（score 为秒级时间戳，member 为帖子 ID 字符串）。
func zadd(t *testing.T, rdb *redis.Redis, key string, score int64, member int64) {
	t.Helper()
	_, err := rdb.Zadd(key, score, fmt.Sprintf("%d", member))
	require.NoError(t, err)
}
