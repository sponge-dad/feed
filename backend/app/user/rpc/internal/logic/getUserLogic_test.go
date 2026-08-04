// getUserLogic_test.go
//
// GetUser RPC Logic 单元测试。对应基线 U-GET-03（RPC 层用户不存在）与
// U-GET-04（非法 user_id 的 RPC 层行为基线）。
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
)

// Baseline: U-GET-01（RPC 层：用户存在返回全部字段）
func TestGetUser_UserExists_ReturnsAllFields(t *testing.T) {
	m := newStubUsersModel()
	m.add(&usermodel.Users{
		Id: 10001, Username: "alice", Nickname: "爱丽丝", Avatar: "http://cdn/a.png",
		Bio: "hello", CityCode: "440300", CityName: "深圳", Status: 1,
		CreatedAt: time.Unix(1700000000, 0),
	})
	svcCtx, _ := newTestServiceContext(t, m)
	l := NewGetUserLogic(context.Background(), svcCtx)

	resp, err := l.GetUser(&user.GetUserReq{UserId: 10001})
	require.NoError(t, err)
	require.NotNil(t, resp.User)
	assert.Equal(t, int64(10001), resp.User.Id)
	assert.Equal(t, "alice", resp.User.Username)
	assert.Equal(t, "爱丽丝", resp.User.Nickname)
	assert.Equal(t, "http://cdn/a.png", resp.User.Avatar)
	assert.Equal(t, "hello", resp.User.Bio)
	assert.Equal(t, "440300", resp.User.CityCode)
	assert.Equal(t, "深圳", resp.User.CityName)
	assert.EqualValues(t, 1, resp.User.Status)
	assert.Equal(t, int64(1700000000), resp.User.CreatedAt)
}

// Baseline: U-GET-03（用户不存在 → 10003）
func TestGetUser_UserNotFound_ReturnsUserNotFound(t *testing.T) {
	m := newStubUsersModel()
	svcCtx, _ := newTestServiceContext(t, m)
	l := NewGetUserLogic(context.Background(), svcCtx)

	resp, err := l.GetUser(&user.GetUserReq{UserId: 99999})
	require.Nil(t, resp)
	var codeErr *errorx.CodeError
	require.ErrorAs(t, err, &codeErr)
	assert.Equal(t, errorx.UserNotFound, codeErr.Code) // 10003
}

// Baseline: U-GET-01 派生（数据库错误 → 透传，不伪装为业务码）
func TestGetUser_DatabaseError_PropagatesError(t *testing.T) {
	m := newStubUsersModel()
	dbErr := errors.New("mysql: bad connection")
	m.findOneErr = dbErr
	svcCtx, _ := newTestServiceContext(t, m)
	l := NewGetUserLogic(context.Background(), svcCtx)

	resp, err := l.GetUser(&user.GetUserReq{UserId: 10001})
	assert.Nil(t, resp)
	require.ErrorIs(t, err, dbErr)
	var codeErr *errorx.CodeError
	assert.False(t, errors.As(err, &codeErr))
}

// Baseline: U-GET-04（行为基线）
// 当前 getUserLogic.go 未实现 user_id > 0 校验：0/负数直接透传给 FindOne，
// 查不到即返回 10003（而非参数错误 code=2）。本测试固化该行为。
func TestGetUser_InvalidUserID_CurrentlyReturnsUserNotFoundBaseline(t *testing.T) {
	cases := []struct {
		name   string
		userID int64
	}{
		{"user_id为0", 0},
		{"user_id为负数", -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newStubUsersModel()
			svcCtx, _ := newTestServiceContext(t, m)
			l := NewGetUserLogic(context.Background(), svcCtx)

			resp, err := l.GetUser(&user.GetUserReq{UserId: tc.userID})
			require.Nil(t, resp)
			var codeErr *errorx.CodeError
			require.ErrorAs(t, err, &codeErr)
			assert.Equal(t, errorx.UserNotFound, codeErr.Code,
				"当前实现无 user_id 合法性校验，非法 ID 走查询路径返回 10003（行为基线）")
		})
	}
}
