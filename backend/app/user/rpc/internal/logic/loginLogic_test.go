// loginLogic_test.go
//
// Login RPC Logic 单元测试。对应基线 U-LOG-01 ~ U-LOG-03。
// 注意：测试断言与日志不打印明文密码与完整 JWT Secret。
package logic

import (
	"context"
	"errors"
	"testing"
	"time"

	usermodel "github.com/sponge-dad/feed/app/user/model"
	"github.com/sponge-dad/feed/app/user/rpc/user"
	"github.com/sponge-dad/feed/common/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// mustHash 用最低成本生成 bcrypt 哈希（仅测试用，降低耗时）。
func mustHash(t *testing.T, pwd string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.MinCost)
	require.NoError(t, err)
	return string(h)
}

const loginTestPwd = "correct-horse"

func seedLoginUser(t *testing.T, m *stubUsersModel, status int64) *usermodel.Users {
	t.Helper()
	u := &usermodel.Users{
		Id:        10001,
		Username:  "alice",
		Password:  mustHash(t, loginTestPwd),
		Nickname:  "爱丽丝",
		Avatar:    "http://cdn.example.com/a.png",
		Bio:       "hi",
		CityCode:  "440300",
		CityName:  "深圳",
		Status:    status,
		CreatedAt: time.Unix(1700000000, 0),
	}
	m.add(u)
	return u
}

// Baseline: U-LOG-01
func TestLogin_CorrectPassword_ReturnsUserInfoAndParsableToken(t *testing.T) {
	m := newStubUsersModel()
	seedLoginUser(t, m, 1)
	svcCtx, _ := newTestServiceContext(t, m)
	l := NewLoginLogic(context.Background(), svcCtx)

	resp, err := l.Login(&user.LoginReq{Username: "alice", Password: loginTestPwd})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.User)

	assert.Equal(t, int64(10001), resp.User.Id)
	assert.Equal(t, "alice", resp.User.Username)
	assert.Equal(t, "爱丽丝", resp.User.Nickname)
	assert.EqualValues(t, 1, resp.User.Status)
	assert.Equal(t, int64(1700000000), resp.User.CreatedAt)

	// Token 可解析且 Claims 正确
	require.NotEmpty(t, resp.Token)
	claims, err := svcCtx.JwtManager.Parse(resp.Token)
	require.NoError(t, err)
	assert.Equal(t, int64(10001), claims.UserID)
	assert.Equal(t, "alice", claims.Username)
}

// Baseline: U-LOG-02（用户不存在与密码错误必须返回同一业务码 10002，防止用户名枚举）
func TestLogin_UserNotFoundOrWrongPassword_BothReturnUserPasswordWrong(t *testing.T) {
	cases := []struct {
		name     string
		username string
		password string
	}{
		{"用户不存在", "ghost", loginTestPwd},
		{"密码错误", "alice", "wrong-password"},
	}

	var messages []string
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newStubUsersModel()
			seedLoginUser(t, m, 1)
			svcCtx, _ := newTestServiceContext(t, m)
			l := NewLoginLogic(context.Background(), svcCtx)

			resp, err := l.Login(&user.LoginReq{Username: tc.username, Password: tc.password})
			require.Nil(t, resp)
			var codeErr *errorx.CodeError
			require.ErrorAs(t, err, &codeErr)
			assert.Equal(t, errorx.UserPasswordWrong, codeErr.Code) // 10002
			messages = append(messages, codeErr.Message)
		})
	}
	// 两种失败的对外消息必须完全一致
	require.Len(t, messages, 2)
	assert.Equal(t, messages[0], messages[1], "用户不存在与密码错误的错误消息必须一致")
}

// Baseline: U-LOG-03（禁用账号 → 10005，且不进行密码比对后的签发）
func TestLogin_DisabledUser_ReturnsUserDisabled(t *testing.T) {
	m := newStubUsersModel()
	seedLoginUser(t, m, 2) // 2:禁用
	svcCtx, _ := newTestServiceContext(t, m)
	l := NewLoginLogic(context.Background(), svcCtx)

	resp, err := l.Login(&user.LoginReq{Username: "alice", Password: loginTestPwd})
	require.Nil(t, resp)
	var codeErr *errorx.CodeError
	require.ErrorAs(t, err, &codeErr)
	assert.Equal(t, errorx.UserDisabled, codeErr.Code) // 10005
}

// Baseline: U-LOG-02 派生（数据库查询失败 → 透传系统错误，不得伪装成 10002）
func TestLogin_DatabaseQueryFails_PropagatesError(t *testing.T) {
	m := newStubUsersModel()
	dbErr := errors.New("mysql: connection refused")
	m.findOneByUsernameErr = dbErr
	svcCtx, _ := newTestServiceContext(t, m)
	l := NewLoginLogic(context.Background(), svcCtx)

	resp, err := l.Login(&user.LoginReq{Username: "alice", Password: loginTestPwd})
	assert.Nil(t, resp)
	require.ErrorIs(t, err, dbErr)
	var codeErr *errorx.CodeError
	assert.False(t, errors.As(err, &codeErr))
}

// Baseline: U-LOG-01 派生（上下文取消 → 查询阶段失败）
func TestLogin_ContextCancelled_ReturnsContextError(t *testing.T) {
	m := newStubUsersModel()
	seedLoginUser(t, m, 1)
	m.respectCtx = true
	svcCtx, _ := newTestServiceContext(t, m)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	l := NewLoginLogic(ctx, svcCtx)

	resp, err := l.Login(&user.LoginReq{Username: "alice", Password: loginTestPwd})
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, context.Canceled)
}
