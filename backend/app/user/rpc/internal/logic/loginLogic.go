// loginLogic.go
//
// 职责：处理用户登录请求。
// 流程：按用户名查库 -> bcrypt 校验密码 -> 校验通过则签发新 JWT。
package logic

import (
	"context"
	"errors"

	usermodel "github.com/sponge-dad/feed/app/user/model"
	"github.com/sponge-dad/feed/app/user/rpc/internal/svc"
	"github.com/sponge-dad/feed/app/user/rpc/user"
	"github.com/sponge-dad/feed/common/errorx"

	"github.com/zeromicro/go-zero/core/logx"
)

type LoginLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewLoginLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LoginLogic {
	return &LoginLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// Login 校验用户名密码，成功后签发登录 token。
func (l *LoginLogic) Login(in *user.LoginReq) (*user.LoginResp, error) {
	// 1. 按用户名查库。这里直接查 MySQL/UserModel 内置缓存，
	//    登录是低频操作（相对于 GetUser），不需要额外的业务级缓存。
	u, err := l.svcCtx.UserModel.FindOneByUsername(l.ctx, in.Username)
	if errors.Is(err, usermodel.ErrNotFound) {
		// 出于安全考虑，用户名不存在和密码错误返回同一个错误码，
		// 避免攻击者通过错误信息差异枚举出哪些用户名已注册。
		return nil, errorx.New(errorx.UserPasswordWrong)
	}
	if err != nil {
		return nil, err
	}

	// 2. 账号已被禁用，直接拒绝登录。
	if u.Status != 1 {
		return nil, errorx.New(errorx.UserDisabled)
	}

	// 3. bcrypt 校验密码：将输入密码和库中哈希比较，bcrypt 内部处理了加盐逻辑。
	//    通过 BcryptPool 执行，避免无限制并发把 CPU 打满。
	if err := l.svcCtx.BcryptPool.Compare([]byte(u.Password), []byte(in.Password)); err != nil {
		return nil, errorx.New(errorx.UserPasswordWrong)
	}

	// 4. 校验通过，签发新 token（每次登录都换发新 token，不复用旧的）。
	token, err := l.svcCtx.JwtManager.Generate(u.Id, u.Username)
	if err != nil {
		return nil, err
	}

	return &user.LoginResp{
		User: &user.UserInfo{
			Id:        u.Id,
			Username:  u.Username,
			Nickname:  u.Nickname,
			Avatar:    u.Avatar,
			Bio:       u.Bio,
			CityCode:  u.CityCode,
			CityName:  u.CityName,
			Status:    int32(u.Status),
			CreatedAt: u.CreatedAt.Unix(),
		},
		Token: token,
	}, nil
}
