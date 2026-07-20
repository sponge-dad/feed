// vars.go
//
// 职责：model 包级别的公共变量/常量。这里把 sqlx.ErrNotFound 暴露给
// logic 层统一使用，和 User 服务保持一致。
package model

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var ErrNotFound = sqlx.ErrNotFound
