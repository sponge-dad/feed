// test_helpers_test.go
//
// User RPC Logic 单元测试公共设施：
//   - stubUsersModel：model.UsersModel 的内存桩，支持错误注入、调用计数与并发安全的唯一索引模拟；
//   - newTestServiceContext：组装 miniredis + stub model + 测试专用 JWT Manager 的 ServiceContext。
//
// 单元测试不连接真实 MySQL/Redis/etcd/RocketMQ。
package logic

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	mysqldriver "github.com/go-sql-driver/mysql"
	usermodel "github.com/sponge-dad/feed/app/user/model"
	"github.com/sponge-dad/feed/app/user/rpc/internal/pkg/bcryptx"
	"github.com/sponge-dad/feed/app/user/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/jwtx"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

// testJwtSecret 测试专用 JWT 密钥，与任何环境配置无关。
const testJwtSecret = "user-logic-unit-test-secret"

// stubResult 实现 sql.Result。
type stubResult struct{ lastID int64 }

func (r stubResult) LastInsertId() (int64, error) { return r.lastID, nil }
func (r stubResult) RowsAffected() (int64, error) { return 1, nil }

// stubUsersModel model.UsersModel 的内存实现。
// 通过 mu 保证并发安全；byUsername 模拟 uk_username 唯一索引。
type stubUsersModel struct {
	mu         sync.Mutex
	byID       map[int64]*usermodel.Users
	byUsername map[string]*usermodel.Users

	// 错误注入
	findOneByUsernameErr error
	findOneErr           error
	insertErr            error
	updateErr            error
	findByIdsErr         error

	// respectCtx 为 true 时，各方法优先返回 ctx.Err()（模拟真实驱动对已取消上下文的行为）。
	respectCtx bool

	// 调用计数
	insertCalls    int
	updateCalls    int
	findByIdsCalls int
	findByIdsArgs  [][]int64
}

func newStubUsersModel() *stubUsersModel {
	return &stubUsersModel{
		byID:       make(map[int64]*usermodel.Users),
		byUsername: make(map[string]*usermodel.Users),
	}
}

func (s *stubUsersModel) add(u *usermodel.Users) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[u.Id] = u
	s.byUsername[u.Username] = u
}

func (s *stubUsersModel) ctxErr(ctx context.Context) error {
	if s.respectCtx && ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

func (s *stubUsersModel) Insert(ctx context.Context, data *usermodel.Users) (sql.Result, error) {
	if err := s.ctxErr(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.insertCalls++
	if s.insertErr != nil {
		return nil, s.insertErr
	}
	// 模拟 uk_username 唯一索引：重复用户名返回 MySQL 1062。
	if _, exists := s.byUsername[data.Username]; exists {
		return nil, &mysqldriver.MySQLError{Number: 1062, Message: "Duplicate entry '" + data.Username + "' for key 'uk_username'"}
	}
	cp := *data
	s.byID[cp.Id] = &cp
	s.byUsername[cp.Username] = &cp
	return stubResult{lastID: cp.Id}, nil
}

func (s *stubUsersModel) FindOne(ctx context.Context, id int64) (*usermodel.Users, error) {
	if err := s.ctxErr(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.findOneErr != nil {
		return nil, s.findOneErr
	}
	if u, ok := s.byID[id]; ok {
		cp := *u
		return &cp, nil
	}
	return nil, usermodel.ErrNotFound
}

func (s *stubUsersModel) FindOneByEmail(ctx context.Context, email sql.NullString) (*usermodel.Users, error) {
	return nil, usermodel.ErrNotFound
}

func (s *stubUsersModel) FindOneByPhone(ctx context.Context, phone sql.NullString) (*usermodel.Users, error) {
	return nil, usermodel.ErrNotFound
}

func (s *stubUsersModel) FindOneByUsername(ctx context.Context, username string) (*usermodel.Users, error) {
	if err := s.ctxErr(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.findOneByUsernameErr != nil {
		return nil, s.findOneByUsernameErr
	}
	if u, ok := s.byUsername[username]; ok {
		cp := *u
		return &cp, nil
	}
	return nil, usermodel.ErrNotFound
}

func (s *stubUsersModel) Update(ctx context.Context, data *usermodel.Users) error {
	if err := s.ctxErr(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updateCalls++
	if s.updateErr != nil {
		return s.updateErr
	}
	cp := *data
	s.byID[cp.Id] = &cp
	s.byUsername[cp.Username] = &cp
	return nil
}

func (s *stubUsersModel) Delete(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.byID[id]; ok {
		delete(s.byUsername, u.Username)
		delete(s.byID, id)
	}
	return nil
}

func (s *stubUsersModel) FindByIds(ctx context.Context, ids []int64) ([]*usermodel.Users, error) {
	if err := s.ctxErr(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.findByIdsCalls++
	s.findByIdsArgs = append(s.findByIdsArgs, append([]int64(nil), ids...))
	if s.findByIdsErr != nil {
		return nil, s.findByIdsErr
	}
	// 模拟 SQL IN 查询：去重返回，查不到的 ID 跳过。
	seen := make(map[int64]bool, len(ids))
	var out []*usermodel.Users
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		if u, ok := s.byID[id]; ok {
			cp := *u
			out = append(out, &cp)
		}
	}
	return out, nil
}

// newTestServiceContext 构造单元测试用 ServiceContext。
// 返回的 miniredis 实例由调用方通过 t.Cleanup 自动关闭。
func newTestServiceContext(t *testing.T, m usermodel.UsersModel) (*svc.ServiceContext, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rds, err := redis.NewRedis(redis.RedisConf{Host: mr.Addr(), Type: "node"})
	require.NoError(t, err)
	return &svc.ServiceContext{
		UserModel:  m,
		Redis:      rds,
		JwtManager: jwtx.NewManager(testJwtSecret, 24),
		BcryptPool: bcryptx.NewPool(4),
	}, mr
}
