# User 服务完整测试方案

> 本方案覆盖 User 服务（含其对外 Gateway REST 接口）的多维度测试，不仅验证请求/响应，还包括单元、集成、性能、并发、安全、容错、一致性等测试类型。测试前请先通过 `make up` 启动基础依赖（MySQL/Redis/etcd）。

---

## 1. 测试目标

| 维度 | 目标 |
|------|------|
| 功能正确性 | 注册/登录/获取/更新/批量获取等接口按预期工作 |
| 接口契约 | gRPC proto 与 REST API 字段、类型、错误码符合设计 |
| 数据一致性 | 数据库、缓存、RPC 响应状态一致 |
| 并发安全 | 多 goroutine/多客户端下无数据竞态、无重复注册 |
| 性能 | 明确 QPS、P99 延迟基线 |
| 安全 | 防止越权、注入、密码泄露、JWT 伪造、暴力破解 |
| 容错 | 依赖故障（MySQL/Redis/下游服务）时服务行为可控 |
| 可观测性 | 日志、指标、链路追踪完整可用 |

---

## 2. 测试环境准备

### 2.1 本地依赖

```bash
# 启动 MySQL / Redis / etcd / RocketMQ
make up

# 可选：安装 k6 / hey / wrk 等压测工具
# 安装 hey（HTTP 压测）
go install github.com/rakyll/hey@latest
```

### 2.2 启动被测服务

```bash
# 启动 User RPC 服务
go run app/user/rpc/user.go -f app/user/rpc/etc/user.yaml

# 启动 Gateway HTTP 服务
go run app/gateway/cmd/api/gateway.go -f app/gateway/etc/gateway.yaml
```

### 2.3 测试数据隔离

- 单元测试：使用 `sqlmock` + `miniredis` 或内存 stub，不依赖真实存储。
- 集成测试：使用独立测试数据库（如 `feed_test`）+ 测试 namespace。
- 每次集成测试前后清理数据：`TRUNCATE users;` 或回滚事务。

---

## 3. 测试类型与用例

### 3.1 单元测试（Unit Test）

目标：不依赖外部服务，验证每个函数/逻辑独立正确。

#### 3.1.1 必测文件

| 文件 | 测试重点 |
|------|----------|
| `app/user/rpc/internal/logic/registerLogic.go` | 用户名唯一、密码加密、token 签发、错误码 |
| `app/user/rpc/internal/logic/loginLogic.go` | 密码校验、账号状态、错误信息不泄露 |
| `app/user/rpc/internal/logic/getUserLogic.go` | 缓存命中/未命中、DB 回源 |
| `app/user/rpc/internal/logic/updateUserLogic.go` | 字段选择性更新、缓存失效 |
| `app/gateway/internal/logic/*.go` | 请求转换、下游 RPC 失败透传、聚合逻辑 |
| `common/jwtx/jwtx.go` | token 生成/解析/过期/非法签名 |
| `common/errorx/errorx.go` | 错误码包装、类型断言 |

#### 3.1.2 推荐工具

```go
// 使用 gomock 模拟 UserModel
mockCtrl := gomock.NewController(t)
mockUserModel := mock.NewMockUsersModel(mockCtrl)

// 使用 sqlmock 拦截 SQL
mockDB, mock, _ := sqlmock.New()
```

#### 3.1.3 关键用例示例

```go
// 注册：密码已加密存储，不能明文落库
func TestRegisterLogic_PasswordHashed(t *testing.T) {
    // 调用 RegisterLogic
    // 断言数据库中 password 字段不等于输入明文
    // 断言 bcrypt 校验通过
}

// 登录：用户名不存在和密码错误返回同一错误码（防枚举）
func TestLoginLogic_SameErrorForWrongPasswordAndNotFound(t *testing.T) {
    // 两种情况均返回 errorx.UserPasswordWrong
}

// JWT：过期 token 解析失败
func TestJWTParse_ExpiredToken(t *testing.T) {
    // 生成过期 token
    // 断言 Parse 返回 error
}
```

#### 3.1.4 运行命令

```bash
go test ./app/user/rpc/internal/logic/... -v -race
go test ./app/gateway/internal/logic/... -v -race
go test ./common/jwtx/... -v
```

---

### 3.2 集成测试（Integration Test）

目标：在真实 MySQL/Redis/etcd 环境下，验证完整用户生命周期。

#### 3.2.1 测试流程

```
注册 -> 登录 -> 获取当前用户 -> 更新用户 -> 获取他人信息 -> 批量获取用户 -> 退出/登录
```

#### 3.2.2 必测场景

| 场景 | 验证点 |
|------|--------|
| 注册成功 | 数据库存在记录、密码已哈希、返回 token |
| 重复注册 | 返回 `UserExists` 错误码，数据库不重复 |
| 登录成功 | 返回新 token，可解析出正确 user_id |
| 登录失败 | 用户名错误/密码错误返回同一错误码 |
| 更新用户 | 数据库字段更新、缓存失效或更新、返回最新数据 |
| 获取用户 | Gateway 聚合 Relation 关注数/粉丝数/是否关注 |
| 批量获取 | 返回用户列表与请求 ID 顺序一致 |
| 禁用用户 | 禁用后登录返回 `UserDisabled` |

#### 3.2.3 测试入口建议

在 `app/user/rpc` 与 `app/gateway` 下分别创建 `integration_test` 目录或文件，使用 `//go:build integration` 标签，避免默认执行。

```go
//go:build integration

package logic_test

func TestUserLifecycle(t *testing.T) {
    // 1. 注册
    // 2. 登录
    // 3. 使用 token 调 Gateway /users/me
    // 4. 更新用户信息
    // 5. 查询更新结果
    // 6. 清理测试数据
}
```

#### 3.2.4 运行命令

```bash
go test -tags=integration ./app/user/rpc/... ./app/gateway/... -v
```

---

### 3.3 接口契约测试（Contract Test）

目标：验证 gRPC proto 与 REST API 字段、类型、错误码不偏离设计。

#### 3.3.1 gRPC 契约测试

- 使用 `buf` 或 `prototool` 检查 proto 是否向后兼容。
- 对比 `api/proto/user/user.proto` 与实现方法是否一致。
- 使用 gRPC 反射（`grpcurl`）调用：

```bash
grpcurl -plaintext -d '{"username":"test","password":"123456","nickname":"Tester"}' \
  localhost:9001 user.User/Register
```

#### 3.3.2 REST 契约测试

验证 Gateway 对外接口的：

- 路径、方法、Content-Type
- 必填字段缺失时的错误
- 字段类型非法（如 userId 传字符串）
- 成功响应结构
- 错误响应结构（`code/message/data/request_id`）

```bash
# 必填字段缺失
curl -X POST http://localhost:8080/api/v1/users/register \
  -H "Content-Type: application/json" \
  -d '{"username":"test"}'
# 期望：code = 2, message = 参数错误

# 成功注册
curl -X POST http://localhost:8080/api/v1/users/register \
  -H "Content-Type: application/json" \
  -d '{"username":"test","password":"123456","nickname":"Tester"}'
# 期望：code = 0, data.user.id > 0, data.token != ""
```

---

### 3.4 性能测试（Performance Test）

目标：获取 User 服务与 Gateway 的 QPS、P99/P95 延迟、错误率、资源占用基线。

#### 3.4.1 测试指标

| 指标 | 关注原因 |
|------|----------|
| QPS | 并发处理能力 |
| P99/P95 延迟 | 长尾体验 |
| 错误率 | 超时、限流、资源耗尽 |
| CPU / 内存 | 是否存在泄漏 |
| MySQL 连接数 | 连接池是否打满 |
| Redis 命中率 | 缓存效果 |

#### 3.4.2 压测用例

| 接口 | 场景 | 目标 |
|------|------|------|
| POST /api/v1/users/register | 1000 并发，持续 60s | 观察用户名冲突、密码哈希 CPU 占用 |
| POST /api/v1/users/login | 1000 并发，持续 60s | 观察 bcrypt 校验耗时、数据库压力 |
| GET /api/v1/users/:id | 5000 并发，持续 60s | 观察缓存命中率、缓存击穿 |
| GET /api/v1/users/me | 1000 并发，持续 60s | 观察 JWT 解析开销、聚合 RPC 耗时 |
| User RPC GetUser | 5000 并发，直接压 gRPC | 观察 MySQL/Redis 是否瓶颈 |

#### 3.4.3 工具示例（hey）

```bash
# 压测 GetUser（公开接口）
hey -z 60s -c 1000 -q 500 \
  http://localhost:8080/api/v1/users/10086

# 压测 登录（需准备一批账号）
hey -z 60s -c 500 -m POST -T "application/json" \
  -D login_payloads.json \
  http://localhost:8080/api/v1/users/login
```

#### 3.4.4 压测后检查

- MySQL 慢查询日志
- Redis 监控 `INFO stats`
- 服务日志是否有大量慢请求或错误
- 内存是否持续增长（leak）

---

### 3.5 并发与竞态测试（Concurrency & Race Test）

目标：暴露多 goroutine 下的数据竞态、死锁、重复写入。

#### 3.5.1 关键场景

| 场景 | 方法 | 预期 |
|------|------|------|
| 同一个用户名并发注册 | 100 goroutine 同时注册同一用户名 | 只有一个成功，其余返回 `UserExists`，数据库无重复 |
| 并发登录同一账号 | 100 goroutine 同时登录 | 均成功，返回不同 token |
| 并发更新同一用户 | 多个 goroutine 同时 PATCH /users/me | 最终数据一致，无 panic |
| 并发 GetUser 缓存击穿 | 大量并发查询不存在的用户 | 数据库只查询一次（或有限次数），其余走缓存或返回错误 |
| 并发批量获取 | 多个 goroutine 批量请求相同 ID 列表 | 结果正确，无 panic |

#### 3.5.2 运行命令

```bash
# 开启 Go 竞态检测器
go test ./app/user/rpc/internal/logic/... -race -count=10
```

#### 3.5.3 自定义并发测试示例

```go
func TestConcurrentRegister(t *testing.T) {
    var wg sync.WaitGroup
    success := int32(0)
    exists := int32(0)

    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            _, err := logic.Register(...) // 同一用户名
            if err == nil { atomic.AddInt32(&success, 1) }
            if errors.IsCode(err, errorx.UserExists) { atomic.AddInt32(&exists, 1) }
        }()
    }
    wg.Wait()

    assert.Equal(t, int32(1), success)
    assert.Equal(t, int32(99), exists)
}
```

---

### 3.6 安全测试（Security Test）

目标：防止常见安全漏洞与越权行为。

#### 3.6.1 测试清单

| 测试项 | 方法 | 预期 |
|--------|------|------|
| SQL 注入 | 用户名/密码传入 `' OR '1'='1` | 注册/登录失败，无异常 panic |
| XSS | 昵称传入 `<script>alert(1)</script>` | 响应原样返回但不触发脚本（前端会转义，或后端过滤） |
| 密码明文存储 | 查询数据库 users.password | 字段值为 bcrypt 哈希，非明文 |
| JWT 签名伪造 | 修改 token payload 但不重新签名 | Gateway 返回 401 |
| JWT 过期 | 使用过期 token 访问 /users/me | Gateway 返回 401 |
| 越权更新 | 用户 A 用 token 尝试更新用户 B | 只能更新自己（/me） |
| 越权获取 | 未登录访问 /users/me | 返回 401 |
| 暴力破解 | 连续 1000 次错误密码登录 | 触发限流或 CAPTCHA/账号锁定（如已接入） |
| 敏感信息泄露 | 抓包 / 日志 | 日志中无密码、token、手机号等明文 |
| 请求体过大 | 发送超大 JSON | 返回 413 或参数错误，服务不崩溃 |

#### 3.6.2 越权测试示例

```bash
# 用户 A 注册登录拿到 token_A
token_A=$(curl -s ... | jq -r .data.token)

# 用户 B 注册
curl -X POST ... -d '{"username":"b","password":"123","nickname":"B"}'

# 用户 A 不能更新用户 B（系统没有 /users/:id PATCH，只有 /users/me）
# 重点验证 /users/me 的 user_id 来自 token，不可被 body 篡改
```

---

### 3.7 容错与混沌测试（Fault Tolerance & Chaos Test）

目标：验证依赖故障时服务不崩溃、错误可控。

#### 3.7.1 故障场景

| 故障 | 注入方式 | 预期 |
|------|----------|------|
| MySQL 断开 | `docker compose stop mysql` | 注册/登录/更新返回 `ServerError`，服务不 panic |
| Redis 断开 | `docker compose stop redis` | 缓存失效，DB 回源，功能仍可用（延迟升高） |
| etcd 断开 | `docker compose stop etcd` | RPC 服务发现失败，Gateway 返回超时错误 |
| User RPC 超时 | 在 Gateway 配置中调小 timeout | Gateway 返回超时错误，不 hang 住 |
| 下游 Relation 不可用 | 停止 relation-rpc | `GET /users/:id` 仍返回基础用户信息，聚合字段为 0 |
| 网络抖动 | 使用 tc/netem 模拟丢包/延迟 | 重试/超时行为符合预期 |
| 服务重启 | kill 并重启 user-rpc | Gateway 通过 etcd 重新发现，请求恢复 |

#### 3.7.2 混沌测试命令示例

```bash
# 模拟 MySQL 故障 30s
docker compose -f deploy/docker-compose.yaml stop mysql && sleep 30 && docker compose start mysql

# 同时持续调用登录接口，观察错误率与恢复时间
```

---

### 3.8 数据一致性测试（Consistency Test）

目标：验证 DB、缓存、RPC 响应三者一致。

#### 3.8.1 测试场景

| 场景 | 验证点 |
|------|--------|
| 注册后立查 | DB 有记录、缓存有记录、RPC 返回一致 |
| 更新昵称后 | DB 字段更新、缓存更新或失效、Gateway /users/me 返回最新值 |
| 更新密码后 | 旧 token 仍能解析（JWT 本身未失效），但新密码登录生效 |
| 用户名唯一 | 并发注册下 DB 唯一索引不冲突 |
| 批量获取 | 返回顺序与请求 ID 一致，无缺失 |

#### 3.8.2 缓存一致性检查

```bash
# 更新后立刻读
redis-cli GET "user:{id}"
# 应为空（失效）或最新值
```

---

## 4. 测试工具栈

| 用途 | 工具 |
|------|------|
| 单元测试 | Go testing + testify + gomock + sqlmock |
| 竞态检测 | `go test -race` |
| 集成测试 | `go test -tags=integration` + docker-compose |
| gRPC 测试 | grpcurl / grpcui / 自定义 Go client |
| HTTP 测试 | curl / hey / k6 / Postman |
| 压测 | hey / k6 / wrk |
| 混沌测试 | docker compose / tc-netem / chaosblade |
| 可观测性 | Prometheus + Grafana + Jaeger + go-zero logx |

---

## 5. 持续集成（CI）建议

在 CI 流水线中分阶段执行：

```yaml
stages:
  - lint
  - unit-test
  - integration-test
  - contract-test
  - race-test
  - build
  - performance-test  # 可选， nightly
```

### 5.1 每次提交必跑

```bash
make fmt
make test
```

### 5.2 每晚或发布前必跑

```bash
go test -tags=integration ./...
go test -race ./...
make build
# 性能基准脚本
bash scripts/benchmark-user.sh
```

---

## 6. 测试通过标准

| 检查项 | 通过标准 |
|--------|----------|
| 单元测试 | 覆盖率 ≥ 70%，核心逻辑 ≥ 90% |
| 集成测试 | 全部通过，无数据残留 |
| 竞态测试 | `go test -race` 无竞态报告 |
| 接口契约 | REST 错误码与响应结构 100% 符合设计 |
| 性能基线 | 单实例 GetUser P99 < 50ms，登录 P99 < 100ms |
| 安全测试 | 无高危漏洞，无敏感信息泄露 |
| 容错测试 | 依赖故障时服务不崩溃，错误码可控 |
| 数据一致性 | 更新后 1s 内缓存/DB/RPC 一致 |

---

## 7. 附录：快速测试脚本

### 7.1 启动并初始化

```bash
make up
make build
./bin/user-rpc -f app/user/rpc/etc/user.yaml &
./bin/gateway -f app/gateway/etc/gateway.yaml &
```

### 7.2 一键接口冒烟测试

```bash
bash scripts/smoke-test-user.sh
```

脚本内容示例：

```bash
#!/bin/bash
set -e
BASE=http://localhost:8080/api/v1

# 注册
TOKEN=$(curl -s -X POST "$BASE/users/register" \
  -H "Content-Type: application/json" \
  -d '{"username":"smoke","password":"123456","nickname":"smoke"}' | jq -r '.data.token')

# 获取 me
curl -s "$BASE/users/me" -H "Authorization: Bearer $TOKEN" | jq

# 更新 me
curl -s -X PATCH "$BASE/users/me" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"nickname":"smoke-new"}' | jq
```

---

## 8. 测试记录模板

每次测试后填写：

| 测试日期 | 版本 | 测试类型 | 用例数 | 通过数 | 失败数 | 阻塞问题 | 负责人 |
|----------|------|----------|--------|--------|--------|----------|--------|
| 2026-07-20 | v0.1 | 集成+性能 | 50 | 48 | 2 | 缓存击穿未处理 | xxx |

