// registerLogic.go
//
// 职责：处理用户注册请求。
// 流程：查重用户名 -> bcrypt 加密密码 -> Snowflake 生成分布式ID -> 写 MySQL -> 签发 JWT。
package logic

import (
	"context"
	"database/sql"
	"errors"

	"time"

	usermodel "github.com/sponge-dad/feed/app/user/model"
	"github.com/sponge-dad/feed/app/user/rpc/internal/svc"
	"github.com/sponge-dad/feed/app/user/rpc/user"
	"github.com/sponge-dad/feed/common/errorx"
	"github.com/sponge-dad/feed/common/idgen"

	"github.com/zeromicro/go-zero/core/logx"
	"golang.org/x/crypto/bcrypt"
)

type RegisterLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewRegisterLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RegisterLogic {
	return &RegisterLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// Register 注册新用户。
func (l *RegisterLogic) Register(in *user.RegisterReq) (*user.RegisterResp, error) {
	// 1. 查重：用户名必须唯一，已存在则直接返回业务错误码，不当成系统异常处理。
	_, err := l.svcCtx.UserModel.FindOneByUsername(l.ctx, in.Username)
	if err == nil {
		return nil, errorx.New(errorx.UserExists)
	}
	if !errors.Is(err, usermodel.ErrNotFound) {
		// 不是"未找到"，说明数据库查询本身出了问题（连接失败等），向上透传系统错误。
		return nil, err
	}

	// 2. bcrypt 对密码加盐哈希，绝不存明文或弱哈希（如 MD5）。
	//    DefaultCost 是 bcrypt 推荐的计算成本，兼顾安全性与性能。
	hashed, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	// 3. 用 Snowflake 生成全局唯一 ID，而不是让 MySQL 自增
	//    （原因见 deploy/sql/user.sql 头部注释：为未来分库分表铺路）。
	newID := idgen.Next()

	// 4. 组装记录并写入 MySQL。email/phone 当前不参与注册，存 sql.NullString 的零值（NULL）。
	newUser := &usermodel.Users{
		Id:       newID,
		Username: in.Username,
		Password: string(hashed),
		Nickname: in.Nickname,
		CityCode: in.CityCode,
		CityName: in.CityName,
		Status:   1, // 1:正常
		Email:    sql.NullString{Valid: false},
		Phone:    sql.NullString{Valid: false},
	}
	if _, err := l.svcCtx.UserModel.Insert(l.ctx, newUser); err != nil {
		return nil, err
	}

	// 5. 注册成功即视为登录成功，直接签发 token，免去用户再手动登录一次。
	token, err := l.svcCtx.JwtManager.Generate(newID, in.Username)
	if err != nil {
		return nil, err
	}

	// 注：newUser.CreatedAt 此刻仍是 Go 零值（该字段由 MySQL DEFAULT CURRENT_TIMESTAMP
	// 在写入时自动生成，Insert 不会把生成值回填到结构体），这里用当前时间近似代替，
	// 避免为了拿到精确的库内时间再多发一次 FindOne 查询。
	return &user.RegisterResp{
		User: &user.UserInfo{
			Id:        newUser.Id,
			Username:  newUser.Username,
			Nickname:  newUser.Nickname,
			CityCode:  newUser.CityCode,
			CityName:  newUser.CityName,
			Status:    int32(newUser.Status),
			CreatedAt: time.Now().Unix(),
		},
		Token: token,
	}, nil
}
