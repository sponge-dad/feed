// config.go 负责读取现有服务配置与解析目标用户。
// 所有配置均复用仓库既有文件，避免在工具中硬编码任何连接串或密钥。
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/zeromicro/go-zero/core/conf"
)

// cosConf 是网关 COS 配置的子集。
type cosConf struct {
	Bucket    string
	Region    string
	SecretID  string
	SecretKey string
	Env       string
	BaseURL   string
}

// gatewayConf 只取工具需要的字段，YAML 中的其余键会被忽略。
type gatewayConf struct {
	Auth struct {
		AccessSecret string
		AccessExpire int64
	}
	Cos cosConf
}

// userConf 只取 User 服务的 MySQL 连接串。
type userConf struct {
	Mysql struct {
		DataSource string
	}
}

// loadGatewayConf 加载网关配置，并把 ${ENV} 占位符按环境变量展开。
func loadGatewayConf(path string) (*gatewayConf, error) {
	var c gatewayConf
	if err := conf.LoadConfig(path, &c); err != nil {
		return nil, fmt.Errorf("加载网关配置失败 %s: %w", path, err)
	}
	c.Cos.SecretID = os.ExpandEnv(c.Cos.SecretID)
	c.Cos.SecretKey = os.ExpandEnv(c.Cos.SecretKey)
	if c.Auth.AccessSecret == "" {
		return nil, fmt.Errorf("网关配置缺少 Auth.AccessSecret")
	}
	if c.Cos.SecretID == "" || c.Cos.SecretKey == "" {
		return nil, fmt.Errorf("COS 凭证为空，请先执行: source scripts/cos-env.sh")
	}
	if c.Cos.BaseURL == "" || c.Cos.Env == "" {
		return nil, fmt.Errorf("网关配置缺少 Cos.BaseURL 或 Cos.Env")
	}
	return &c, nil
}

// loadUserDSN 从 User 服务配置读取 MySQL 连接串。
func loadUserDSN(path string) (string, error) {
	var c userConf
	if err := conf.LoadConfig(path, &c); err != nil {
		return "", fmt.Errorf("加载 User 配置失败 %s: %w", path, err)
	}
	if strings.TrimSpace(c.Mysql.DataSource) == "" {
		return "", fmt.Errorf("User 配置缺少 Mysql.DataSource")
	}
	return c.Mysql.DataSource, nil
}

// targetUser 是注入目标用户的基本信息。
type targetUser struct {
	ID       int64
	Username string
	Nickname string
}

// resolveUser 按 username 或 nickname 精确定位用户。
// 使用参数化查询防止 SQL 注入；命中多条时报错，避免误注入到非预期账号。
func resolveUser(ctx context.Context, dsn, name string) (*targetUser, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("目标用户不能为空")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("连接 MySQL 失败: %w", err)
	}
	defer db.Close()

	qctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	rows, err := db.QueryContext(qctx,
		"SELECT id, username, nickname FROM users WHERE username = ? OR nickname = ? LIMIT 5", name, name)
	if err != nil {
		return nil, fmt.Errorf("查询用户失败: %w", err)
	}
	defer rows.Close()

	var found []targetUser
	for rows.Next() {
		var u targetUser
		if err := rows.Scan(&u.ID, &u.Username, &u.Nickname); err != nil {
			return nil, fmt.Errorf("解析用户记录失败: %w", err)
		}
		found = append(found, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取用户记录失败: %w", err)
	}

	switch len(found) {
	case 0:
		return nil, fmt.Errorf("未找到用户: %s", name)
	case 1:
		return &found[0], nil
	default:
		var sb strings.Builder
		for _, u := range found {
			fmt.Fprintf(&sb, "\n  id=%d username=%s nickname=%s", u.ID, u.Username, u.Nickname)
		}
		return nil, fmt.Errorf("用户 %s 命中 %d 条记录，请改用唯一的 username：%s", name, len(found), sb.String())
	}
}
