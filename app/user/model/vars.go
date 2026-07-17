// vars.go
//
// 职责：model 层公共变量/错误定义。
// ErrNotFound 用于区分"查询未找到"和"数据库真的出错了"两种情况，
// logic 层判断错误时必须用 errors.Is(err, model.ErrNotFound)，
// 未找到应转换成业务错误码（如 errorx.UserNotFound），
// 而不是把它当成服务器内部错误直接向上抛。
package model

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var ErrNotFound = sqlx.ErrNotFound
