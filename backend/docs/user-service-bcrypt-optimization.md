# User 服务 bcrypt 性能优化与压测问题排查记录

## 1. 背景

Feed 项目 User 服务的注册、登录接口使用 `golang.org/x/crypto/bcrypt` 对用户密码进行哈希和校验。bcrypt 是故意设计为慢速的 CPU 密集型算法，可抵御暴力破解，但在高并发场景下容易把服务 CPU 占满，导致 RPC 调用超时。

本次优化过程中还连带发现并修复了以下问题：

- Gateway 无法透传 user-rpc 的业务错误码，导致"用户已存在"被显示成"服务器内部错误"。
- JWT claim 中 `user_id` 用 number 存储，64 位 Snowflake ID 经 go-zero `jwt.MapClaims` 解析为 `float64` 后精度丢失。
- 压测脚本误用 `hey -D`（把整个文件作为单个 body），导致 register/login 压测数据失真。
- go-zero 默认的 breaker / adaptive shedding 会把 bcrypt 的正常耗时误判为系统过载，产生大量 503。

## 2. 压测现象

使用 `scripts/benchmark-user.sh` 对 Gateway 暴露的 REST 接口进行压测，并发 100、持续 60s：

| 接口 | QPS | 成功率 | P99 | 关键现象 |
|------|-----|--------|-----|----------|
| `GET /users/:id` / `GET /users/me` | 2069 | 100% 200 | 918ms | 正常 |
| `POST /users/login` | 1433（名义） | 6.4% 200 / 93.6% 503 | 548ms | 大量 `context deadline exceeded` |

`login` 接口的失败率高达 93% 以上，错误集中在 `503 Service Unavailable` 与客户端超时。

## 3. 根因分析

1. **bcrypt 是 CPU 密集型计算**：`bcrypt.CompareHashAndPassword` 单线程长时间占用 CPU，cost=10 时单核一次约 60~100ms。
2. **无并发限制**：go-zero 默认给每个 gRPC 请求分配一个 goroutine。100 个并发请求同时进入 user-rpc，会启动 100 个 goroutine 同时计算 bcrypt。
3. **CPU 被占满导致排队**：当并发数远超 CPU 核心数时，goroutine 大量切换、排队，单个请求处理时间从几十毫秒涨到数秒。
4. **Gateway / user-rpc 自我保护**：默认 breaker 与 adaptive shedding 在高负载/慢响应时打开，直接拒绝请求，表现为 503。
5. **Gateway RPC 超时触发**：堆积超过 RPC 超时阈值后 Gateway 直接返回 503 / context deadline exceeded。

> 第一个接口（查询类）不依赖 bcrypt，且命中缓存，因此表现正常；这进一步说明瓶颈就在 login 的密码校验环节。

## 4. 优化方案

### 4.1 引入 bcrypt 并发池

核心思路：用带缓冲的 channel 作为信号量，把同时执行 bcrypt 的 goroutine 数限制在 CPU 核心数附近。超过容量的请求会排队，而不是无限制争抢 CPU。

新建 `app/user/rpc/internal/pkg/bcryptx/pool.go`：

```19:47:app/user/rpc/internal/pkg/bcryptx/pool.go
// Pool 限制 bcrypt 计算的并发度。
type Pool struct {
	sem chan struct{}
}

// NewPool 创建一个 bcrypt 计算池。
// workers <= 0 时默认使用 runtime.NumCPU()，与 CPU 核心数保持一致。
func NewPool(workers int) *Pool {
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	return &Pool{
		sem: make(chan struct{}, workers),
	}
}

// Hash 在池内执行 bcrypt.GenerateFromPassword，并发数受池容量限制。
func (p *Pool) Hash(password []byte, cost int) ([]byte, error) {
	p.sem <- struct{}{}
	defer func() { <-p.sem }()
	return bcrypt.GenerateFromPassword(password, cost)
}

// Compare 在池内执行 bcrypt.CompareHashAndPassword，并发数受池容量限制。
func (p *Pool) Compare(hashedPassword, password []byte) error {
	p.sem <- struct{}{}
	defer func() { <-p.sem }()
	return bcrypt.CompareHashAndPassword(hashedPassword, password)
}
```

### 4.2 调整 RPC 超时与连接控制

- **user-rpc 服务端**：增大处理超时，避免 bcrypt 排队时服务端提前丢弃请求。
- **gateway 客户端**：增大调用 user-rpc 的超时，给 bcrypt 排队 + 计算留出足够时间。

### 4.3 修复业务错误码跨服务透传

user-rpc 返回的 `*errorx.CodeError` 经过 gRPC 传输后，Gateway 收到的是普通 gRPC status error，`handler/helper.go` 无法识别，统一当作服务器内部错误返回。

新增 `common/errorx/grpc.go`：

```26:56:common/errorx/grpc.go
// ToGRPCError 将业务 CodeError 转换为 gRPC status error。
func ToGRPCError(err *CodeError) error {
	payload, _ := json.Marshal(map[string]interface{}{
		"code": err.Code,
		"msg":  err.Message,
	})
	return status.Error(codes.Unknown, grpcErrorPrefix+string(payload))
}

// FromGRPCError 尝试从 gRPC error 中还原业务 CodeError。
func FromGRPCError(err error) (*CodeError, bool) {
	st, ok := status.FromError(err)
	if !ok {
		return nil, false
	}

	msg := st.Message()
	if !strings.HasPrefix(msg, grpcErrorPrefix) {
		return nil, false
	}

	var payload struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if jsonErr := json.Unmarshal([]byte(msg[len(grpcErrorPrefix):]), &payload); jsonErr != nil {
		return nil, false
	}

	return NewWithMsg(payload.Code, payload.Msg), true
}
```

并在 user-rpc 注册服务端拦截器 `app/user/rpc/internal/serverinterceptors/error.go`，将 logic 层业务错误统一转换；Gateway `handler/helper.go` 改用 `errorx.TryParse` 解析。

### 4.4 修复 JWT `user_id` 精度丢失

`common/jwtx/jwtx.go` 中 `UserID` 改用字符串序列化：

```15:18:common/jwtx/jwtx.go
type Claims struct {
	// UserID 在 JSON 中以字符串形式存储，避免 64 位整数经过 float64 时精度丢失。
	UserID   int64  `json:"user_id,string"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}
```

`app/gateway/internal/middleware/auth.go` 兼容字符串形式的 claim：

```22:34:app/gateway/internal/middleware/auth.go
if v := ctx.Value("user_id"); v != nil {
	switch uid := v.(type) {
	case string:
		if id, err := strconv.ParseInt(uid, 10, 64); err == nil {
			return id
		}
	case float64:
		return int64(uid)
	case int64:
		return uid
	case int:
		return int64(uid)
	}
}
```

### 4.5 压测脚本修复

`hey` 的 `-D` 会把整个文件作为**单个 body**，而不是按行随机选择。原脚本用 `-D login_payloads.txt` 发送了 100 行 JSON 组成的非法 body，导致压测数据失真。

修复后：

- `login` 压测改用固定 body 的 `-d` 参数，重点测试 bcrypt compare。
- `register` 压测改用 shell 并发 worker 持续生成随机用户名，避免用户名冲突。
- 脚本自动处理"用户已存在"情况，重复运行也能正常获取 token。

## 5. 代码改动

### 5.1 ServiceContext 注册 BcryptPool

`app/user/rpc/internal/svc/serviceContext.go`：

```40:42:app/user/rpc/internal/svc/serviceContext.go
// BcryptPool 限制 bcrypt 计算的并发度，防止 CPU 密集型操作把服务打满。
// Register/Login 通过它执行密码哈希和校验。
BcryptPool *bcryptx.Pool
```

```55:60:app/user/rpc/internal/svc/serviceContext.go
return &ServiceContext{
	Config:     c,
	UserModel:  model.NewUsersModel(conn, c.CacheRedis),
	Redis:      rds,
	JwtManager: jwtx.NewManager(c.JwtAuth.AccessSecret, c.JwtAuth.AccessExpireHour),
	BcryptPool: bcryptx.NewPool(c.BcryptWorkers),
}
```

### 5.2 Login / Register 使用并发池

`app/user/rpc/internal/logic/loginLogic.go`：

```52:56:app/user/rpc/internal/logic/loginLogic.go
// 3. bcrypt 校验密码：将输入密码和库中哈希比较，bcrypt 内部处理了加盐逻辑。
//    通过 BcryptPool 执行，避免无限制并发把 CPU 打满。
if err := l.svcCtx.BcryptPool.Compare([]byte(u.Password), []byte(in.Password)); err != nil {
	return nil, errorx.New(errorx.UserPasswordWrong)
}
```

`app/user/rpc/internal/logic/registerLogic.go`：

```50:56:app/user/rpc/internal/logic/registerLogic.go
// 2. bcrypt 对密码加盐哈希，绝不存明文或弱哈希（如 MD5）。
//    DefaultCost 是 bcrypt 推荐的计算成本，兼顾安全性与性能。
//    通过 BcryptPool 执行，避免无限制并发把 CPU 打满。
hashed, err := l.svcCtx.BcryptPool.Hash([]byte(in.Password), bcrypt.DefaultCost)
if err != nil {
	return nil, err
}
```

### 5.3 Config 增加 BcryptWorkers

`app/user/rpc/internal/config/config.go`：

```45:49:app/user/rpc/internal/config/config.go
// BcryptWorkers bcrypt 并发计算的 worker 数量。
// 注册/登录都依赖 bcrypt，属于 CPU 密集型操作。该值控制同时执行 bcrypt 的
// 最大 goroutine 数，避免并发请求过多时把 CPU 占满导致 RPC 超时。<=0 时默认
// 使用 runtime.NumCPU()。
BcryptWorkers int
```

### 5.4 user-rpc 注册错误码拦截器

`app/user/rpc/user.go`：

```28:36:app/user/rpc/user.go
s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
	user.RegisterUserServer(grpcServer, server.NewUserServer(ctx))

	if c.Mode == service.DevMode || c.Mode == service.TestMode {
		reflection.Register(grpcServer)
	}
})

// 注册服务端拦截器：将业务错误码转换为 gRPC status error，供调用方还原。
s.AddUnaryInterceptors(serverinterceptors.ErrorInterceptor)
```

## 6. 配置改动

### 6.1 user-rpc 服务端配置

`app/user/rpc/etc/user.yaml`：

```yaml
# RPC 服务端全局超时与连接控制。
# Timeout 单位为毫秒，bcrypt 计算是 CPU 密集型，压测时需要给足处理时间。
Timeout: 10000
MaxConns: 1000
# 压测时临时设为 999（上限）关闭 adaptive shedding；生产建议 900~950。
CpuThreshold: 999

...

# bcrypt 计算并发 worker 数。
# <=0 时使用 runtime.NumCPU()。该值应根据实际 CPU 核心数和线上负载调整：
# 过大导致 CPU 占满、RPC 超时；过小会导致请求排队、吞吐下降。
BcryptWorkers: 0
```

### 6.2 Gateway 客户端配置

`app/gateway/etc/gateway.yaml`：

```yaml
UserRpc:
  Etcd:
    Hosts:
      - 127.0.0.1:2479
    Key: user.rpc
  Timeout: 10000     # Gateway 调用 user.rpc 超时 10s

...

# 压测时临时关闭 breaker / shedding；生产建议开启。
Middlewares:
  Breaker: false
  Shedding: false
```

## 7. 验证方式

```bash
# 1. 编译
make build BUILD_DIR=/tmp/feed-bench-build

# 2. 启动 user-rpc、relation-rpc、gateway
./tmp/feed-bench-build/user-rpc -f app/user/rpc/etc/user.yaml &
./tmp/feed-bench-build/relation-rpc -f app/relation/rpc/etc/relation.yaml &
./tmp/feed-bench-build/gateway -f app/gateway/etc/gateway.yaml &

# 3. 执行压测
bash scripts/benchmark-user.sh
```

## 8. 优化后压测结果

测试环境：16 核 CPU，100 并发，持续 60s，关闭 breaker/shedding 以观察 bcrypt 池的真实表现。

| 接口 | 成功数 | 失败数 | QPS | 平均耗时 | P99 |
|------|--------|--------|-----|----------|-----|
| `POST /users/register` | 8248 | 0 | - | - | - |
| `POST /users/login` | 12260 | 0 | 202 | 491ms | 556ms |
| `GET /users/:id` | 814109 | 0 | 13567 | 7.4ms | 17.6ms |
| `GET /users/me` | 1000000 | 0 | 22030 | 6.0ms | 9.8ms |

### 结果解读

- `login` 吞吐约 **200 req/s**，与 16 核 × bcrypt 约 80ms 的理论值（16 / 0.08 = 200）完全吻合。
- 100 并发下 `login` 平均延迟约 500ms，符合排队模型：`((并发 - worker) / worker) × bcrypt 耗时 ≈ ((100 - 16) / 16) × 100ms = 525ms`。
- 查询类接口不受 bcrypt 影响，成功率 100%，延迟稳定在 10ms 左右。

## 9. 性能预期与生产建议

1. **绝对吞吐受 CPU 核心数限制**：单实例 login 极限吞吐 ≈ `NumCPU / bcrypt耗时`。16 核约 200 req/s，4 核约 50 req/s。
2. **BcryptWorkers 调优**：默认值 `runtime.NumCPU()` 是起点。线上可适当提高到 `NumCPU * 2`，但过高会失去限流意义。
3. **水平扩容 user-rpc**：多实例可线性提升总吞吐，配合 Gateway 负载均衡。
4. **生产环境必须开启保护机制**：压测时关闭 breaker/shedding 是为了观察真实吞吐；生产环境应保留，并将 `CpuThreshold` 调回 900~950，避免雪崩。
5. **login 限流/防刷**：高并发 login 也可能是暴力破解攻击，建议配合 IP 限流、图形验证码、账户锁定策略。
6. **降低 bcrypt cost 仅用于测试环境**：生产环境不建议降低，cost 低于 10 会显著削弱密码安全性。
7. **缓存成功 token**：在允许的场景下，对同一用户的合法登录结果做短时缓存，可减少重复 bcrypt 计算。

## 10. 相关文件

- `app/user/rpc/internal/pkg/bcryptx/pool.go`
- `app/user/rpc/internal/svc/serviceContext.go`
- `app/user/rpc/internal/logic/loginLogic.go`
- `app/user/rpc/internal/logic/registerLogic.go`
- `app/user/rpc/internal/config/config.go`
- `app/user/rpc/internal/serverinterceptors/error.go`
- `app/user/rpc/user.go`
- `app/user/rpc/etc/user.yaml`
- `app/gateway/internal/handler/helper.go`
- `app/gateway/internal/middleware/auth.go`
- `app/gateway/etc/gateway.yaml`
- `common/jwtx/jwtx.go`
- `common/errorx/grpc.go`
- `scripts/benchmark-user.sh`
