# API Gateway

Feed 系统的对外 HTTP 网关（go-zero REST），端口 8080。承担鉴权、限流、BFF 聚合与统一错误响应。

## 职责

- **路由转发**：对外暴露 `/api/v1` 下的 REST 接口，对内通过 gRPC 调用 User / Relation / Feed / Comment / Interaction 服务。
- **认证鉴权**：JWT 中间件解析 Token，userId 注入 `context`。
- **BFF 聚合**：信息流 / 详情 / 个人页 / 我的赞·收藏等接口用 `errgroup` 并行批量调用下游（禁 N+1）。
- **统一错误**：业务码 11000~14999 透传，统一 `{code,message,data,request_id}` 结构。

## 目录结构

```
app/gateway/
├── api/                 # *.api 接口定义（唯一手写）
│   ├── gateway.api      # 总入口：import 各模块、注册 22 个路由
│   ├── relation.api feed.api comment.api interaction.api  # 模块类型定义
├── cmd/api/main.go      # 服务入口
├── etc/gateway.yaml     # 运行配置（etcd 127.0.0.1:2479）
└── internal/
    ├── config/          # 配置结构体
    ├── handler/         # goctl 生成，定制 response.Success/ErrorFrom 输出
    ├── logic/           # BFF 聚合逻辑（手写，按模块分包）
    ├── middleware/      # ClientIP 中间件（同城流定位用）
    ├── svc/             # ServiceContext（注入 5 个 RPC Client + IPResolver）
    └── types/           # goctl 生成
```

## 路由一览（/api/v1）

| 模块 | 接口 | 鉴权 |
|------|------|------|
| relation | POST /relations/:userId/follow、DELETE /relations/:userId/follow、GET /relations/following、GET /relations/followers、GET /relations/:userId/is-following | jwt |
| feed | POST /feeds、DELETE /feeds/:feedId、GET /feeds/:feedId、GET /feeds/timeline、GET /users/:userId/feeds | jwt |
| comment | POST /feeds/:feedId/comments、GET /feeds/:feedId/comments、GET /comments/:commentId/replies、DELETE /comments/:commentId、POST /comments/:commentId/like、DELETE /comments/:commentId/like | jwt |
| interaction | POST /feeds/:feedId/like|collect、DELETE /feeds/:feedId/like|collect、GET /users/me/likes、GET /users/me/collects | jwt |

> 评论点赞（like/unlike comment）路由已暴露，下游 RPC 暂未就绪，logic 占位返回业务错误，待补齐。

## 本地运行

```bash
# 先启动基础设施：make up
cd app/gateway
go run cmd/api/main.go -f etc/gateway.yaml
```

## 重新生成代码

接口变更后修改 `api/*.api`，再执行：

```bash
make api   # = goctl api go --style=goZero + 清理重复生成的 gateway.go/servicecontext.go
```

生成后 handler 已定制为统一响应（`response.Success` / `response.ErrorFrom`），**不要手改 types.go 与 handler 签名**。

## 测试

```bash
go test -race ./app/gateway/...
```

聚合 / 分页 / 权限 / 超时 / 降级场景由 `internal/mocks`（gomock 桩）覆盖。
