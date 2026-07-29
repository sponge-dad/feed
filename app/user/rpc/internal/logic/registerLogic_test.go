// registerLogic_test.go
//
// Register RPC Logic 单元测试。对应基线 U-REG-01 ~ U-REG-03、U-REG-05（单元版）、
// 风险基线 R-P0-4（并发同名注册 1062 未兜底转 10001）。
package logic

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	usermodel "github.com/sponge-dad/feed/app/user/model"
	"github.com/sponge-dad/feed/app/user/rpc/user"
	"github.com/sponge-dad/feed/common/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// Baseline: U-REG-01
func TestRegister_ValidInput_CreatesUserWithBcryptHashAndValidToken(t *testing.T) {
	m := newStubUsersModel()
	svcCtx, _ := newTestServiceContext(t, m)
	l := NewRegisterLogic(context.Background(), svcCtx)

	const plainPwd = "s3cret-pass"
	before := time.Now()
	resp, err := l.Register(&user.RegisterReq{
		Username: "alice",
		Password: plainPwd,
		Nickname: "爱丽丝",
		CityCode: "440300",
		CityName: "深圳",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.User)

	// 响应：user_id > 0，用户名与昵称正确，token 非空
	assert.Greater(t, resp.User.Id, int64(0))
	assert.Equal(t, "alice", resp.User.Username)
	assert.Equal(t, "爱丽丝", resp.User.Nickname)
	assert.Equal(t, "440300", resp.User.CityCode)
	assert.Equal(t, "深圳", resp.User.CityName)
	assert.EqualValues(t, 1, resp.User.Status)
	require.NotEmpty(t, resp.Token)

	// 数据库副作用：写入 1 条记录，字段正确
	stored, ok := m.byUsername["alice"]
	require.True(t, ok, "users 表应新增 alice 记录")
	assert.Equal(t, resp.User.Id, stored.Id)
	assert.EqualValues(t, 1, stored.Status)
	assert.False(t, stored.Email.Valid, "注册时 email 应为 NULL")
	assert.False(t, stored.Phone.Valid, "注册时 phone 应为 NULL")
	assert.Equal(t, 1, m.insertCalls)

	// 密码必须是 bcrypt 哈希，绝不能是明文
	assert.NotEqual(t, plainPwd, stored.Password)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(stored.Password), []byte(plainPwd)))

	// Token 可被项目 JWT Manager 解析，Claims 与当前实现一致
	claims, err := svcCtx.JwtManager.Parse(resp.Token)
	require.NoError(t, err)
	assert.Equal(t, resp.User.Id, claims.UserID)
	assert.Equal(t, "alice", claims.Username)
	assert.WithinDuration(t, before.Add(24*time.Hour), claims.ExpiresAt.Time, 5*time.Second)
}

// Baseline: U-REG-02（用户名已存在 → code=10001，且不写库）
func TestRegister_UsernameExists_ReturnsUserExistsWithoutInsert(t *testing.T) {
	m := newStubUsersModel()
	m.add(&usermodel.Users{Id: 1, Username: "alice", Password: "hash", Status: 1})
	svcCtx, _ := newTestServiceContext(t, m)
	l := NewRegisterLogic(context.Background(), svcCtx)

	resp, err := l.Register(&user.RegisterReq{Username: "alice", Password: "pwd123"})
	require.Nil(t, resp)

	var codeErr *errorx.CodeError
	require.ErrorAs(t, err, &codeErr)
	assert.Equal(t, errorx.UserExists, codeErr.Code) // 10001
	assert.Equal(t, 0, m.insertCalls, "重名注册不得触发 Insert")
}

// Baseline: U-REG-03（记录当前行为基线）
// 注意：基线文档预期 RPC 层做长度校验返回 code=2，但当前 registerLogic.go 没有任何
// 参数校验（校验位于 Gateway 层 .api validate 之外同样缺失）。本测试固化 RPC 层
// 当前行为：空用户名/空密码均注册成功。差异已记录到 docs/test-implementation-report.md。
func TestRegister_EmptyUsernameOrPassword_CurrentlySucceedsWithoutValidation(t *testing.T) {
	cases := []struct {
		name     string
		username string
		password string
	}{
		{"空用户名当前可注册成功（无RPC层校验）", "", "pwd123"},
		{"空密码当前可注册成功（无RPC层校验）", "bob", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newStubUsersModel()
			svcCtx, _ := newTestServiceContext(t, m)
			l := NewRegisterLogic(context.Background(), svcCtx)

			resp, err := l.Register(&user.RegisterReq{Username: tc.username, Password: tc.password})
			require.NoError(t, err, "当前实现无参数校验，应注册成功（行为基线）")
			require.NotNil(t, resp)
			assert.Equal(t, tc.username, resp.User.Username)
			assert.Equal(t, 1, m.insertCalls)
		})
	}
}

// Baseline: U-REG-01 派生（查重查询失败 → 透传系统错误，不写库）
func TestRegister_FindByUsernameFails_PropagatesErrorWithoutInsert(t *testing.T) {
	m := newStubUsersModel()
	dbErr := errors.New("mysql: connection refused")
	m.findOneByUsernameErr = dbErr
	svcCtx, _ := newTestServiceContext(t, m)
	l := NewRegisterLogic(context.Background(), svcCtx)

	resp, err := l.Register(&user.RegisterReq{Username: "alice", Password: "pwd123"})
	assert.Nil(t, resp)
	require.ErrorIs(t, err, dbErr)
	var codeErr *errorx.CodeError
	assert.False(t, errors.As(err, &codeErr), "系统错误不应被包装为业务错误码")
	assert.Equal(t, 0, m.insertCalls)
}

// Baseline: U-REG-01 派生（bcrypt 加密失败：>72 字节密码触发 ErrPasswordTooLong，不写库）
func TestRegister_PasswordHashFails_ReturnsErrorWithoutInsert(t *testing.T) {
	m := newStubUsersModel()
	svcCtx, _ := newTestServiceContext(t, m)
	l := NewRegisterLogic(context.Background(), svcCtx)

	longPwd := make([]byte, 73) // bcrypt 上限 72 字节
	for i := range longPwd {
		longPwd[i] = 'a'
	}
	resp, err := l.Register(&user.RegisterReq{Username: "alice", Password: string(longPwd)})
	assert.Nil(t, resp)
	require.Error(t, err)
	assert.ErrorIs(t, err, bcrypt.ErrPasswordTooLong)
	assert.Equal(t, 0, m.insertCalls)
}

// Baseline: U-REG-01 派生（上下文取消 → 查重阶段即失败，不产生任何副作用）
func TestRegister_ContextCancelled_ReturnsErrorWithoutSideEffects(t *testing.T) {
	m := newStubUsersModel()
	m.respectCtx = true
	svcCtx, _ := newTestServiceContext(t, m)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	l := NewRegisterLogic(ctx, svcCtx)

	resp, err := l.Register(&user.RegisterReq{Username: "alice", Password: "pwd123"})
	assert.Nil(t, resp)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 0, m.insertCalls)
}

// Baseline: U-REG-01 派生（数据库插入失败 → 透传错误）
func TestRegister_InsertFails_PropagatesError(t *testing.T) {
	m := newStubUsersModel()
	dbErr := errors.New("mysql: table is full")
	m.insertErr = dbErr
	svcCtx, _ := newTestServiceContext(t, m)
	l := NewRegisterLogic(context.Background(), svcCtx)

	resp, err := l.Register(&user.RegisterReq{Username: "alice", Password: "pwd123"})
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, dbErr)
	assert.Empty(t, m.byUsername, "插入失败后不应存在用户记录")
}

// Baseline: U-REG-05（单元版并发同名注册）
// 最小修复后行为：并发同名注册时，仅一个请求成功；后到请求命中唯一索引 1062，
// 已被 registerLogic 识别并转换为 UsernameExists(10001)，不再以原始 *mysql.MySQLError 透传。
func TestRegister_ConcurrentSameUsername_OnlyOneSucceedsOthersGetUserExists(t *testing.T) {
	m := newStubUsersModel()
	svcCtx, _ := newTestServiceContext(t, m)

	const workers = 8
	var wg sync.WaitGroup
	errs := make([]error, workers)
	var start sync.WaitGroup
	start.Add(1)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			start.Wait()
			l := NewRegisterLogic(context.Background(), svcCtx)
			_, err := l.Register(&user.RegisterReq{Username: "same-name", Password: "pwd123"})
			errs[idx] = err
		}(i)
	}
	start.Done()
	wg.Wait()

	var successCount, existsCount int
	for _, err := range errs {
		switch {
		case err == nil:
			successCount++
		default:
			var codeErr *errorx.CodeError
			require.ErrorAs(t, err, &codeErr, "并发撞唯一键必须被兜底为 UserExists(10001)")
			assert.Equal(t, errorx.UserExists, codeErr.Code,
				"1062 应转为 UsernameExists，不应透传原始 *mysql.MySQLError")
			existsCount++
		}
	}

	assert.Equal(t, 1, successCount, "同一用户名最终只能注册成功一次")
	assert.Equal(t, workers, successCount+existsCount)
	// 数据库最终仅一条记录
	assert.Len(t, m.byUsername, 1)
	assert.NotNil(t, m.byUsername["same-name"])
}
