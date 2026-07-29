// updateUserLogic_test.go
//
// UpdateUser RPC Logic 单元测试。对应基线 U-UPD-01、U-UPD-02，
// 以及 R-P1-3（user:brief 缓存不随资料更新失效）的单元级行为基线。
//
// 说明：基线任务中提到的"用户名冲突"场景在当前实现中不适用——
// UpdateUserReq 不包含 username 字段，用户名不可通过该接口修改（已记录到实施报告）。
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

func seedUpdateUser(m *stubUsersModel) *usermodel.Users {
	u := &usermodel.Users{
		Id: 10001, Username: "alice", Password: "hash",
		Nickname: "旧昵称", Avatar: "http://cdn/old.png", Bio: "旧简介",
		CityCode: "440300", CityName: "深圳", Status: 1,
		CreatedAt: time.Unix(1700000000, 0),
	}
	m.add(u)
	return u
}

// Baseline: U-UPD-01（全字段更新成功并落库）
func TestUpdateUser_AllFields_UpdatesRecordAndResponse(t *testing.T) {
	m := newStubUsersModel()
	seedUpdateUser(m)
	svcCtx, _ := newTestServiceContext(t, m)
	l := NewUpdateUserLogic(context.Background(), svcCtx)

	resp, err := l.UpdateUser(&user.UpdateUserReq{
		UserId:   10001,
		Nickname: "新昵称",
		Avatar:   "http://cdn/new.png",
		Bio:      "新简介",
		CityCode: "110100",
		CityName: "北京",
	})
	require.NoError(t, err)
	require.NotNil(t, resp.User)

	// 响应字段
	assert.Equal(t, "新昵称", resp.User.Nickname)
	assert.Equal(t, "http://cdn/new.png", resp.User.Avatar)
	assert.Equal(t, "新简介", resp.User.Bio)
	assert.Equal(t, "110100", resp.User.CityCode)
	assert.Equal(t, "北京", resp.User.CityName)
	assert.Equal(t, "alice", resp.User.Username, "用户名不可被更新")

	// 数据库副作用
	stored := m.byID[10001]
	assert.Equal(t, "新昵称", stored.Nickname)
	assert.Equal(t, "http://cdn/new.png", stored.Avatar)
	assert.Equal(t, "新简介", stored.Bio)
	assert.Equal(t, "110100", stored.CityCode)
	assert.Equal(t, 1, m.updateCalls)
}

// Baseline: U-UPD-01（部分字段更新：仅 nickname 变更，其余保持原值）
func TestUpdateUser_OnlyNickname_KeepsOtherFields(t *testing.T) {
	m := newStubUsersModel()
	seedUpdateUser(m)
	svcCtx, _ := newTestServiceContext(t, m)
	l := NewUpdateUserLogic(context.Background(), svcCtx)

	resp, err := l.UpdateUser(&user.UpdateUserReq{UserId: 10001, Nickname: "只改昵称"})
	require.NoError(t, err)

	stored := m.byID[10001]
	assert.Equal(t, "只改昵称", stored.Nickname)
	assert.Equal(t, "http://cdn/old.png", stored.Avatar, "未传字段应保持原值")
	assert.Equal(t, "旧简介", stored.Bio)
	assert.Equal(t, "440300", stored.CityCode)
	assert.Equal(t, "深圳", stored.CityName)
	assert.Equal(t, "只改昵称", resp.User.Nickname)
}

// Baseline: U-UPD-02（空字符串语义 = 不更新，而非清空）
func TestUpdateUser_EmptyStrings_MeanNoChangeNotClear(t *testing.T) {
	m := newStubUsersModel()
	seedUpdateUser(m)
	svcCtx, _ := newTestServiceContext(t, m)
	l := NewUpdateUserLogic(context.Background(), svcCtx)

	resp, err := l.UpdateUser(&user.UpdateUserReq{UserId: 10001}) // 全部空串
	require.NoError(t, err)

	stored := m.byID[10001]
	assert.Equal(t, "旧昵称", stored.Nickname, "空字符串表示不更新（当前实现无法清空字段）")
	assert.Equal(t, "旧简介", stored.Bio)
	assert.Equal(t, "旧昵称", resp.User.Nickname)
	// 当前实现即使无字段变化也会执行一次 Update（行为基线）
	assert.Equal(t, 1, m.updateCalls)
}

// Baseline: U-UPD-01 派生（用户不存在 → 10003，不执行 Update）
func TestUpdateUser_UserNotFound_ReturnsUserNotFound(t *testing.T) {
	m := newStubUsersModel()
	svcCtx, _ := newTestServiceContext(t, m)
	l := NewUpdateUserLogic(context.Background(), svcCtx)

	resp, err := l.UpdateUser(&user.UpdateUserReq{UserId: 404, Nickname: "x"})
	require.Nil(t, resp)
	var codeErr *errorx.CodeError
	require.ErrorAs(t, err, &codeErr)
	assert.Equal(t, errorx.UserNotFound, codeErr.Code)
	assert.Equal(t, 0, m.updateCalls)
}

// Baseline: U-UPD-01 派生（数据库更新失败 → 透传错误）
func TestUpdateUser_UpdateFails_PropagatesError(t *testing.T) {
	m := newStubUsersModel()
	seedUpdateUser(m)
	dbErr := errors.New("mysql: lock wait timeout")
	m.updateErr = dbErr
	svcCtx, _ := newTestServiceContext(t, m)
	l := NewUpdateUserLogic(context.Background(), svcCtx)

	resp, err := l.UpdateUser(&user.UpdateUserReq{UserId: 10001, Nickname: "x"})
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, dbErr)
}

// Risk baseline: R-P1-3
// 当前行为基线：UpdateUser 不会失效 BatchGetUsers 写入的 user:brief:{id} 业务缓存
// （goctl 行缓存由 model.Update 内部处理，但 brief 快照缓存无人清理），
// 更新资料后 600s 内 BatchGetUsers 仍返回旧昵称。
func TestUpdateUser_BriefCacheNotInvalidated_CurrentBehaviorBaseline(t *testing.T) {
	m := newStubUsersModel()
	seedUpdateUser(m)
	svcCtx, mr := newTestServiceContext(t, m)

	// 预热：模拟 BatchGetUsers 已写入 brief 缓存
	staleBrief := `{"id":10001,"nickname":"旧昵称","avatar":"http://cdn/old.png"}`
	require.NoError(t, mr.Set("user:brief:10001", staleBrief))

	l := NewUpdateUserLogic(context.Background(), svcCtx)
	_, err := l.UpdateUser(&user.UpdateUserReq{UserId: 10001, Nickname: "新昵称"})
	require.NoError(t, err)

	// 断言：brief 缓存仍是旧值（未被失效）——已知一致性风险 R-P1-3
	got, err := mr.Get("user:brief:10001")
	require.NoError(t, err)
	assert.Equal(t, staleBrief, got, "当前实现不清理 user:brief 缓存（行为基线，风险 R-P1-3）")

	// 且后续 BatchGetUsers 命中该脏缓存返回旧昵称
	bl := NewBatchGetUsersLogic(context.Background(), svcCtx)
	resp, err := bl.BatchGetUsers(&user.BatchGetUsersReq{UserIds: []int64{10001}})
	require.NoError(t, err)
	require.Len(t, resp.Users, 1)
	assert.Equal(t, "旧昵称", resp.Users[0].Nickname, "600s 窗口内返回陈旧昵称（行为基线）")
}
