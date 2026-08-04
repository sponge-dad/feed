# Gateway 服务开发手册

> 本手册规定 Feed 项目 **API Gateway** 服务的开发标准。网关基于 go-zero `goctl` 生成，对外暴露 HTTP REST 接口，对内通过 gRPC 调用各业务服务。所有 Gateway 相关代码必须遵循本手册，以保证接口风格统一、可维护、可扩展。

---

## 1. 定位与职责

### 1.1 定位

API Gateway 是系统 **唯一对外入口**，所有客户端（Web、App、小程序）请求必须先经过网关。

### 1.2 核心职责

- **路由转发**：将 HTTP 请求路由到对应内部 gRPC 服务。
- **认证鉴权**：JWT Token 解析与登录态校验。
- **请求限流**：基于 go-zero 令牌桶 / 漏桶限流。
- **参数校验**：基础字段校验（必填、类型、长度）。
- **响应聚合**：跨服务请求聚合（如用户主页聚合 User + Relation + Feed 数据）。
- **统一错误**：对外返回统一的错误码与错误信息。
- **跨域处理**：CORS 配置。

### 1.3 禁止职责

- 不执行业务逻辑计算。
- 不直接访问数据库、Redis、MQ 等存储。
- 不在网关层保存业务状态。

---

## 2. 目录结构标准

Gateway 服务目录位于 `app/gateway/`，结构如下：

```
app/gateway/
├── api/                     # go-zero API 接口定义（*.api）
│   ├── gateway.api         # 总入口：import 各模块、注册路由
│   ├── user.api            # 用户模块 API 类型定义
│   ├── relation.api        # 关系模块 API 类型定义
│   ├── feed.api            # Feed 模块 API 类型定义
│   ├── comment.api         # 评论模块 API 类型定义
│   └── interaction.api     # 互动模块 API 类型定义
├── cmd/api/
│   └── main.go             # HTTP 服务入口
├── etc/
│   └── gateway.yaml        # 运行配置文件
└── internal/
    ├── config/
    │   └── config.go       # 配置结构体
    ├── handler/            # HTTP Handler（goctl 生成）
    ├── logic/              # 跨服务聚合逻辑（手写）
    ├── middleware/         # 自定义中间件
    ├── svc/
    │   └── service_context.go  # ServiceContext，注入 RPC Client 与配置
    └── types/
        └── types.go        # API 请求/响应类型（goctl 生成）
```

### 2.1 目录说明

| 目录 | 说明 | 是否可手写 |
|------|------|------------|
| `api/` | `*.api` 接口定义文件，**唯一允许手写的部分** | 是 |
| `cmd/api/` | 服务入口 `main.go`，`goctl` 生成后一般不再修改 | 否（生成） |
| `etc/` | YAML 配置文件 | 是 |
| `internal/config/` | 配置结构体，`goctl` 生成 | 否（生成） |
| `internal/handler/` | HTTP Handler，`goctl` 生成 | 否（生成） |
| `internal/middleware/` | 自定义中间件 | 是（按需） |
| `internal/svc/` | ServiceContext，`goctl` 生成后补全 RPC Client | 部分手写 |
| `internal/types/` | 请求/响应类型，`goctl` 生成 | 否（生成） |

**原则**：

- 所有类型定义必须在 `.api` 文件中完成，`goctl` 自动生成到 `internal/types/`。
- Handler 由 `goctl` 生成，业务逻辑只写在 `svc.ServiceContext` 提供的方法或 `logic` 中。
- 严禁绕过 `goctl` 直接修改 `types.go` 或 Handler 中的类型签名。

---

## 3. API 文件编写标准

### 3.1 文件组织

- 入口文件：`api/gateway.api`
  - 负责 `import` 各模块 API 文件。
  - 负责统一注册 HTTP 路由。
  - 按**鉴权维度**拆分 `@server` 块：公开接口、需登录接口。
- 模块文件：`api/{module}.api`
  - 按业务模块划分，如 `user.api`、`feed.api`、`relation.api`。
  - 只定义 `type`，不定义路由。

### 3.2 路由定义规范

```go
// 公开接口：注册、登录
@server (
	prefix: /api/v1
)
service gateway {
	@handler register
	post /users/register (RegisterReq) returns (RegisterResp)

	@handler login
	post /users/login (LoginReq) returns (LoginResp)
}

// 需登录接口
@server (
	jwt:    Auth
	prefix: /api/v1
)
service gateway {
	@handler getMe
	get /users/me returns (UserDetail)
}
```

| 项 | 规范 |
|----|------|
| service 名 | 固定为 `gateway`（通过多个 `@server` 块按鉴权维度拆分） |
| prefix | 统一为 `/api/v1`，多版本时可用 `/api/v2` |
| 路径 | 小写、kebab-case 或 snake_case 保持一致；REST 资源命名，如 `/users/:userId` |
| 路径参数 | `.api` 文件中使用 `:` 前缀（如 `:userId`），接口文档中使用 `{}`（如 `{userId}`） |
| 方法 | 严格遵循 HTTP 语义：`GET` 查询、`POST` 创建、`PUT/PATCH` 更新、`DELETE` 删除 |
| 鉴权 | 需登录接口加 `jwt: Auth`；公开接口不加 |
| handler 名 | 驼峰命名，与功能一致，如 `getUser`、`uploadToken` |

### 3.3 类型定义规范

```go
// 注册
type RegisterReq {
    Username string `json:"username" validate:"required"`
    Password string `json:"password" validate:"required"`
    Nickname string `json:"nickname" validate:"required"`
}

type RegisterResp {
    User  User   `json:"user"`
    Token string `json:"token"`
}
```

| 项 | 规范 |
|----|------|
| 字段命名 | Go 大驼峰，如 `Username`、`FollowingCount` |
| JSON tag | 小写 + snake_case，如 `city_name` |
| 必填校验 | 使用 `validate:"required"`，仅用于基础必填校验 |
| 可选字段 | 使用 `json:"nickname,optional"` |
| 路径参数 | 使用 `path:"userId"` |
| 表单参数 | 使用 `form:"file"` |
| 响应体 | 统一返回 JSON 对象，不允许返回裸数组或裸基本类型 |

### 3.4 分页规范

列表接口统一使用以下分页字段：

```go
type PageReq {
    Page     int64 `form:"page,default=1"`     // 页码，从 1 开始
    PageSize int64 `form:"page_size,default=20"` // 每页条数，默认 20，最大 100
}

type PageResp {
    List     []any `json:"list"`
    Total    int64 `json:"total"`
    Page     int64 `json:"page"`
    PageSize int64 `json:"page_size"`
    HasMore  bool  `json:"has_more"`
}
```

---

## 4. 配置文件标准

### 4.1 配置文件位置

`app/gateway/etc/gateway.yaml`

### 4.2 配置结构

```yaml
Name: gateway
Host: 0.0.0.0
Port: 8080

# gRPC 服务客户端配置（通过 etcd 服务发现）
UserRpc:
  Etcd:
    Hosts:
      - 127.0.0.1:2479
    Key: user.rpc

RelationRpc:
  Etcd:
    Hosts:
      - 127.0.0.1:2479
    Key: relation.rpc

# JWT 解析配置，必须与 User 服务保持一致
JwtAuth:
  AccessSecret: "feed-user-service-jwt-secret-CHANGE-ME"
  AccessExpireHour: 720
```

### 4.3 配置结构体

```go
type Config struct {
	rest.RestConf

	UserRpc        zrpc.RpcClientConf
	RelationRpc    zrpc.RpcClientConf
	FeedRpc        zrpc.RpcClientConf
	CommentRpc     zrpc.RpcClientConf
	InteractionRpc zrpc.RpcClientConf

	JwtAuth struct {
		AccessSecret     string
		AccessExpireHour int
	}

	// IP 定位解析器配置（同城流用），见 common/ipx
	IPLocation struct {
		DefaultCity string
	}
}
```

| 项 | 规范 |
|----|------|
| 基础配置 | 嵌入 `rest.RestConf`，包含 Name/Host/Port/Timeout 等 |
| RPC 配置 | 每个下游服务一个 `zrpc.RpcClientConf`，命名 `{Service}Rpc` |
| JWT 配置 | 字段名使用 `JwtAuth`，避免与 `rest.RestConf` 内嵌 `Auth` 撞名 |
| 密钥 | 必须来自环境变量或配置中心，禁止硬编码生产密钥 |

---

## 5. ServiceContext 标准

`ServiceContext` 是网关的核心依赖注入容器，负责持有所有 RPC Client 和全局配置。

### 5.1 标准结构

```go
type ServiceContext struct {
	Config         config.Config
	UserRpc        user.UserClient
	RelationRpc    relation.RelationClient
	FeedRpc        feed.FeedClient
	CommentRpc     comment.CommentClient
	InteractionRpc interaction.InteractionClient
	IPResolver     ipx.Resolver
}

func NewServiceContext(c config.Config) *ServiceContext {
	return &ServiceContext{
		Config:         c,
		UserRpc:        user.NewUserClient(zrpc.MustNewClient(c.UserRpc).Conn()),
		RelationRpc:    relation.NewRelationClient(zrpc.MustNewClient(c.RelationRpc).Conn()),
		FeedRpc:        feed.NewFeedClient(zrpc.MustNewClient(c.FeedRpc).Conn()),
		CommentRpc:     comment.NewCommentClient(zrpc.MustNewClient(c.CommentRpc).Conn()),
		InteractionRpc: interaction.NewInteractionClient(zrpc.MustNewClient(c.InteractionRpc).Conn()),
		IPResolver:     ipx.NewStaticResolver(c.IPLocation.DefaultCity),
	}
}
```

### 5.2 使用规范

- `ServiceContext` 必须在 `main.go` 中初始化，并注入到每个 Handler。
- Handler 通过 `svc.UserRpc.Login(...)` 调用下游服务。
- 禁止在 Handler 中直接 `new` RPC Client。
- 跨服务聚合逻辑可封装在 `ServiceContext` 的方法中，避免 Handler 膨胀。

---

## 6. Handler 编写标准

### 6.1 Handler 生成

Handler 由 `goctl api go` 生成，不要手写。

### 6.2 Handler 代码规范

```go
func GetUserHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.GetUserReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.Error(w, err)
			return
		}

		l := user.NewGetUserLogic(r.Context(), svcCtx)
		resp, err := l.GetUser(&req)
		if err != nil {
			httpx.Error(w, err)
		} else {
			httpx.OkJson(w, resp)
		}
	}
}
```

### 6.3 编写原则

- Handler 只负责：解析请求、调用 Logic、返回响应。
- 禁止在 Handler 中写业务逻辑。
- 禁止在 Handler 中直接访问数据库或外部 HTTP 接口。
- 请求上下文统一使用 `r.Context()`，确保链路追踪、超时传递。

---

## 7. Logic 编写标准

> 注：go-zero 生成 Gateway 时默认不生成 Logic 目录，建议 Gateway 项目手动创建 `internal/logic/` 目录，用于承载跨服务聚合逻辑。

### 7.1 目录结构

```
internal/logic/
├── user/
│   ├── get_user_logic.go
│   └── login_logic.go
└── feed/
    └── feed_list_logic.go
```

### 7.2 Logic 模板

```go
package user

type GetUserLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetUserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserLogic {
	return &GetUserLogic{ctx: ctx, svcCtx: svcCtx}
}

func (l *GetUserLogic) GetUser(req *types.GetUserReq) (*types.UserDetail, error) {
	userResp, err := l.svcCtx.UserRpc.GetUser(l.ctx, &user.GetUserReq{
		UserId: req.UserID,
	})
	if err != nil {
		return nil, err
	}

	return &types.UserDetail{
		ID:       userResp.Id,
		Username: userResp.Username,
		Nickname: userResp.Nickname,
	}, nil
}
```

### 7.3 聚合逻辑

当接口需要调用多个服务时，建议：

- 串行调用：服务间有依赖关系时使用。
- 并行调用：服务间无依赖时使用 `errgroup` 或 `sync.WaitGroup` 并发调用，注意超时控制。

```go
import "golang.org/x/sync/errgroup"

g, ctx := errgroup.WithContext(l.ctx)

var userResp *user.GetUserResp
var relationResp *relation.GetFollowCountResp

g.Go(func() error {
	var err error
	userResp, err = l.svcCtx.UserRpc.GetUser(ctx, &user.GetUserReq{UserId: req.UserID})
	return err
})

g.Go(func() error {
	var err error
	relationResp, err = l.svcCtx.RelationRpc.GetFollowCount(ctx, &relation.GetFollowCountReq{UserId: req.UserID})
	return err
})

if err := g.Wait(); err != nil {
	return nil, err
}
```

---

## 8. 中间件标准

### 8.1 内置中间件

go-zero 提供：

- JWT 鉴中间件（`jwt: Auth`）
- 限流中间件（`maxBytes`、`timeout` 等）
- 熔断、降载等（`rest.RestConf` 中配置）

### 8.2 自定义中间件

自定义中间件放在 `internal/middleware/`，必须实现 `http.Handler` 包装器。

```go
package middleware

import "net/http"

type ContextMiddleware struct {
	next http.Handler
}

func NewContextMiddleware(next http.Handler) *ContextMiddleware {
	return &ContextMiddleware{next: next}
}

func (m *ContextMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 前置逻辑
	m.next.ServeHTTP(w, r)
	// 后置逻辑
}
```

注册方式：

```go
server := rest.MustNewServer(c.RestConf, rest.WithMiddlewares(
	[]rest.Middleware{
		middleware.NewContextMiddleware,
	},
))
```

### 8.3 常见中间件

| 中间件 | 用途 | 优先级 |
|--------|------|--------|
| JWT 鉴权 | 登录态校验 | 高 |
| CORS | 跨域处理 | 高 |
| 限流 | 令牌桶/漏桶 | 高 |
| 请求日志 | 记录请求耗时、状态码 | 中 |
| 统一响应 | 统一错误码包装 | 中 |
| 灰度 | 按用户/设备灰度 | 低 |

---

## 9. 认证与授权标准

### 9.1 JWT 认证流程

```
Client --(Header: Authorization: Bearer <token>)--> Gateway
Gateway 解析 JWT，获取 userId
Gateway 将 userId 写入 Context
下游服务无需再次解析 Token
```

### 9.2 JWT 配置一致性

- 网关的 `JwtAuth.AccessSecret` 必须与 User 服务签发 Token 使用的密钥一致。
- 网关的 `JwtAuth.AccessExpireHour` 必须与 User 服务一致。
- 密钥修改需同步更新所有服务配置。

### 9.3 用户 ID 获取

在 Logic 中从 Context 获取当前登录用户 ID：

```go
userId, ok := l.ctx.Value("userId").(int64)
if !ok {
    return nil, errors.New("unauthorized")
}
```

> 实际取值 key 以 go-zero JWT 中间件注入的 key 为准，通常为 `jwtPayload` 或自定义 key。

### 9.4 权限校验

- 网关只做**登录态校验**。
- 业务权限校验（如是否是自己的资源）由下游服务负责，或网关层做简单 owner 校验。

---

## 10. 错误处理与响应标准

### 10.1 统一错误响应

```json
{
  "code": 10001,
  "message": "用户名或密码错误"
}
```

### 10.2 错误码规范

业务错误码统一在 `common/errorx/errorx.go` 中维护，分段规则如下：

| 错误码段 | 含义 |
|----------|------|
| 0 | 成功 |
| 1 ~ 999 | 系统级错误（参数错误、未登录、无权限、网关层错误等） |
| 10000 ~ 10999 | User 服务错误 |
| 11000 ~ 11999 | Relation 服务错误 |
| 12000 ~ 12999 | Feed 服务错误 |
| 13000 ~ 13999 | Comment 服务错误 |
| 14000 ~ 14999 | Interaction 服务错误 |

> 详细码段定义与错误码列表见 `docs/design/api-spec/README.md` 和 `common/errorx/errorx.go`。

### 10.3 错误处理原则

- 网关层错误（如参数解析失败、超时）使用 1 ~ 999 段错误码。
- 下游 RPC 错误透传时保留原业务错误码，由网关统一包装为 HTTP JSON。
- 禁止将内部错误详情（如 SQL 错误、堆栈）直接返回给客户端。

---

## 11. 安全规范

### 11.1 输入安全

- 所有请求参数必须在 `.api` 中声明校验规则。
- 字符串字段必须限制长度，防止超长请求攻击。
- 文件上传类接口必须校验文件类型、大小、扩展名。
- 对客户端输入做 XSS 过滤，禁止将原始输入直接反射到响应中。

### 11.2 认证安全

- JWT 密钥使用强随机字符串，长度不少于 32 字节。
- 生产环境密钥必须通过环境变量或配置中心注入，禁止硬编码。
- Token 过期时间合理设置（如 30 天）。
- 敏感接口必须开启 HTTPS。

### 11.3 传输安全

- 对外接口强制 HTTPS。
- 内部 RPC 调用启用 TLS/mTLS（生产环境）。
- 禁止在 URL 中传递密码、Token 等敏感信息。

### 11.4 反攻击

- 登录、注册、验证码接口必须配置限流，防止暴力破解。
- 文件上传接口限制文件大小和频率。
- 对内部 IP 段（`10.*`、`192.168.*`、`172.16-31.*`）访问做严格限制，禁止 SSRF。

---

## 12. 代码生成流程

### 12.1 编写/修改 API 文件

在 `app/gateway/api/` 中修改或新增 `.api` 文件。

### 12.2 生成代码

```bash
cd app/gateway
# 方式一：指定 api 文件
goctl api go -api api/gateway.api -dir .

# 方式二：使用 Makefile 中的命令（推荐）
make gateway
```

### 12.3 生成后补全

- 在 `internal/svc/service_context.go` 中注册新的 RPC Client。
- 在 `internal/config/config.go` 中增加新的配置字段（生成后通常保留）。
- 在 `internal/logic/` 中编写业务聚合逻辑。

### 12.4 禁止操作

- 禁止直接修改 `internal/types/types.go` 中的类型，应该修改 `.api` 文件重新生成。
- 禁止直接修改 `internal/handler/*.go` 中的请求解析和响应逻辑，业务逻辑应放在 Logic 中。

---

## 13. 日志与监控

### 13.1 日志规范

- 使用 go-zero 内置 `logx`。
- 日志级别：`Debug` 用于调试，`Info` 用于正常请求，`Error` 用于错误，`Slow` 用于慢请求。
- 关键字段必须打印：`trace_id`、`user_id`、`path`、`cost`、`status`、`error`。
- 禁止在日志中打印密码、Token、手机号等敏感信息。

### 13.2 监控指标

- QPS、请求耗时 P99/P95/P50
- 各下游 RPC 调用成功率、耗时
- 错误码分布
- 限流触发次数

### 13.3 链路追踪

- 所有请求必须传递 `trace_id`。
- 网关生成的 `trace_id` 需注入到 RPC Context 中，供下游服务使用。

---

## 14. 测试与部署

### 14.1 本地测试

```bash
cd app/gateway
go run cmd/api/main.go -f etc/gateway.yaml
```

### 14.2 单元测试

- 对 Logic 层编写单元测试。
- 使用 `gomock` 或 `mockery` 模拟 RPC Client。
- 测试覆盖核心路径：成功、参数错误、RPC 失败、超时。

### 14.3 部署

- 使用容器化部署，Dockerfile 位于项目根目录或 `deploy/`。
- 配置通过环境变量或配置中心注入，禁止将生产密钥打包到镜像。
- 多实例部署时前置 Nginx / Ingress 做负载均衡。

---

## 15. 常见问题

### 15.1 为什么网关不直接访问数据库？

网关是薄层，直接访问数据库会导致：

- 业务逻辑泄漏到网关，难以维护。
- 数据库连接池管理复杂。
- 安全边界模糊。

### 15.2 如何新增一个下游服务？

1. 在 `api/proto/` 中定义 proto 文件。
2. 生成 gRPC client 代码。
3. 在 `gateway.yaml` 中增加 RPC 配置。
4. 在 `config.go` 中增加 `zrpc.RpcClientConf` 字段。
5. 在 `service_context.go` 中初始化 Client。
6. 在 `.api` 文件中新增接口，重新生成 Gateway 代码。

### 15.3 网关如何获取当前登录用户？

通过 go-zero JWT 中间件解析 Token 后，用户 ID 会注入到 Context 中。在 Logic 中从 Context 取出即可。

### 15.4 一个接口需要聚合多个服务数据怎么办？

在 `internal/logic/` 中编写聚合逻辑，优先使用 `errgroup` 做无依赖并行调用，注意设置超时。

---

## 16. 检查清单（Code Review）

在提交 Gateway 代码前，请确认：

- [ ] 新的接口已在 `.api` 文件中定义，并重新生成代码。
- [ ] 路由路径符合 REST 规范，方法使用正确。
- [ ] 请求/响应类型字段有正确的 JSON tag 和校验规则。
- [ ] 敏感接口已配置 `jwt: Auth`。
- [ ] 新的下游服务已在 `config.go` 和 `service_context.go` 中注册。
- [ ] 配置中未硬编码生产密钥。
- [ ] 错误处理完善，不泄露内部错误详情。
- [ ] 敏感输入已做校验和过滤。
- [ ] 日志中未打印敏感信息。
- [ ] 已补充单元测试或接口测试。

---

## 附录：参考文件

| 文件 | 说明 |
|------|------|
| `app/gateway/api/gateway.api` | 网关总入口 API 文件（import 各模块、注册路由） |
| `app/gateway/api/user.api` | 用户模块 API 类型定义 |
| `app/gateway/api/relation.api` | 关系模块 API 类型定义 |
| `app/gateway/api/feed.api` | Feed 模块 API 类型定义 |
| `app/gateway/api/comment.api` | 评论模块 API 类型定义 |
| `app/gateway/api/interaction.api` | 互动模块 API 类型定义 |
| `app/gateway/etc/gateway.yaml` | 网关运行配置 |
| `app/gateway/internal/config/config.go` | 配置结构体 |
| `app/gateway/internal/svc/service_context.go` | 依赖注入容器 |
| `docs/design/service-design.md` | 服务拆分与调用关系 |
| `docs/design/api-spec/` | 对外 REST API 契约 |
