package logic

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-sql-driver/mysql"
	"github.com/sponge-dad/feed/app/relation/model"
	"github.com/sponge-dad/feed/app/relation/rpc/internal/svc"
	"github.com/sponge-dad/feed/app/relation/rpc/relation"
	"github.com/sponge-dad/feed/common/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

// memoryRelationsModel 是 RelationsModel 的内存 stub，用于单元测试。
type memoryRelationsModel struct {
	mu        sync.Mutex
	records   []*model.Relations
	nextId    uint64
	insertErr error
}

func newMemoryRelationsModel() *memoryRelationsModel {
	return &memoryRelationsModel{nextId: 1}
}

func (m *memoryRelationsModel) Insert(_ context.Context, data *model.Relations) (sql.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.insertErr != nil {
		return nil, m.insertErr
	}
	for _, r := range m.records {
		if r.FollowerId == data.FollowerId && r.FolloweeId == data.FolloweeId {
			return nil, &mysql.MySQLError{Number: 1062, Message: "duplicate key"}
		}
	}
	if data.Id == 0 {
		data.Id = m.nextId
		m.nextId++
	}
	m.records = append(m.records, data)
	return &memoryResult{lastID: int64(data.Id)}, nil
}

func (m *memoryRelationsModel) FindOne(_ context.Context, id uint64) (*model.Relations, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.records {
		if r.Id == id {
			return r, nil
		}
	}
	return nil, model.ErrNotFound
}

func (m *memoryRelationsModel) FindOneByFollowerIdFolloweeId(_ context.Context, followerId, followeeId uint64) (*model.Relations, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.records {
		if r.FollowerId == followerId && r.FolloweeId == followeeId {
			return r, nil
		}
	}
	return nil, model.ErrNotFound
}

func (m *memoryRelationsModel) Update(_ context.Context, data *model.Relations) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.records {
		if r.Id == data.Id {
			m.records[i] = data
			return nil
		}
	}
	return model.ErrNotFound
}

func (m *memoryRelationsModel) Delete(_ context.Context, id uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.records {
		if r.Id == id {
			m.records = append(m.records[:i], m.records[i+1:]...)
			return nil
		}
	}
	return model.ErrNotFound
}

func (m *memoryRelationsModel) FindByFollowerId(_ context.Context, followerId uint64, limit, offset uint64) ([]*model.Relations, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res []*model.Relations
	for _, r := range m.records {
		if r.FollowerId == followerId {
			res = append(res, r)
		}
	}
	if offset >= uint64(len(res)) {
		return nil, nil
	}
	end := offset + limit
	if end > uint64(len(res)) {
		end = uint64(len(res))
	}
	return res[offset:end], nil
}

func (m *memoryRelationsModel) FindByFolloweeId(_ context.Context, followeeId uint64, limit, offset uint64) ([]*model.Relations, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res []*model.Relations
	for _, r := range m.records {
		if r.FolloweeId == followeeId {
			res = append(res, r)
		}
	}
	if offset >= uint64(len(res)) {
		return nil, nil
	}
	end := offset + limit
	if end > uint64(len(res)) {
		end = uint64(len(res))
	}
	return res[offset:end], nil
}

func (m *memoryRelationsModel) CountByFolloweeId(_ context.Context, followeeId uint64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var count int64
	for _, r := range m.records {
		if r.FolloweeId == followeeId {
			count++
		}
	}
	return count, nil
}

type memoryResult struct {
	lastID int64
}

func (r *memoryResult) LastInsertId() (int64, error) { return r.lastID, nil }
func (r *memoryResult) RowsAffected() (int64, error) { return 1, nil }

func newTestServiceContext(t *testing.T) (*svc.ServiceContext, func()) {
	s := miniredis.RunT(t)
	rds := redis.MustNewRedis(redis.RedisConf{
		Host: s.Addr(),
		Type: redis.NodeType,
	})
	return &svc.ServiceContext{
		RelationModel: newMemoryRelationsModel(),
		Redis:         rds,
		IdGen:         func() int64 { return 1 },
	}, func() { s.Close() }
}

func codeOf(err error) int {
	if codeErr, ok := errorx.TryParse(err); ok {
		return codeErr.Code
	}
	return -1
}

func TestFollowLogic_Follow_Self(t *testing.T) {
	ctx, closeFn := newTestServiceContext(t)
	defer closeFn()

	l := NewFollowLogic(context.Background(), ctx)
	_, err := l.Follow(&relation.FollowReq{FollowerId: 1, FolloweeId: 1})

	assert.Error(t, err)
	assert.Equal(t, errorx.RelationSelf, codeOf(err))
}

func TestFollowLogic_Follow_InvalidParam(t *testing.T) {
	ctx, closeFn := newTestServiceContext(t)
	defer closeFn()

	l := NewFollowLogic(context.Background(), ctx)
	_, err := l.Follow(&relation.FollowReq{FollowerId: -1, FolloweeId: 2})
	assert.Equal(t, errorx.ParamError, codeOf(err))

	_, err = l.Follow(&relation.FollowReq{FollowerId: 1, FolloweeId: 0})
	assert.Equal(t, errorx.ParamError, codeOf(err))
}

func TestFollowLogic_Follow_AlreadyFollow(t *testing.T) {
	ctx, closeFn := newTestServiceContext(t)
	defer closeFn()

	_, err := ctx.RelationModel.Insert(context.Background(), &model.Relations{
		Id:         1,
		FollowerId: 1,
		FolloweeId: 2,
		CreatedAt:  1,
	})
	assert.NoError(t, err)

	l := NewFollowLogic(context.Background(), ctx)
	resp, err := l.Follow(&relation.FollowReq{FollowerId: 1, FolloweeId: 2})

	assert.NoError(t, err)
	assert.True(t, resp.Success)

	count, err := ctx.RelationModel.CountByFolloweeId(context.Background(), 2)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), count)
}

func TestFollowLogic_Follow_DuplicateConcurrent(t *testing.T) {
	ctx, closeFn := newTestServiceContext(t)
	defer closeFn()

	l := NewFollowLogic(context.Background(), ctx)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := l.Follow(&relation.FollowReq{FollowerId: 1, FolloweeId: 2})
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	count, err := ctx.RelationModel.CountByFolloweeId(context.Background(), 2)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), count)
}

func TestFollowLogic_Follow_DuplicateKeyErrorIgnored(t *testing.T) {
	ctx, closeFn := newTestServiceContext(t)
	defer closeFn()

	// 模拟 model 返回 MySQL 1062 唯一键冲突，应被识别为已存在并返回成功。
	mm := ctx.RelationModel.(*memoryRelationsModel)
	mm.insertErr = &mysql.MySQLError{Number: 1062, Message: "duplicate key"}

	l := NewFollowLogic(context.Background(), ctx)
	resp, err := l.Follow(&relation.FollowReq{FollowerId: 1, FolloweeId: 2})

	assert.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestFollowLogic_Follow_OtherDBError(t *testing.T) {
	ctx, closeFn := newTestServiceContext(t)
	defer closeFn()

	mm := ctx.RelationModel.(*memoryRelationsModel)
	mm.insertErr = errors.New("connection refused")

	l := NewFollowLogic(context.Background(), ctx)
	_, err := l.Follow(&relation.FollowReq{FollowerId: 1, FolloweeId: 2})

	assert.Error(t, err)
}
