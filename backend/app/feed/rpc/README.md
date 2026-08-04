# Feed RPC 服务实现教学流程

> 本文档是 Feed 模块的**实现教学路线图**，面向第一次在本仓库开发新服务的同学。
> 建议配合以下文档阅读：
>
> - `../../../docs/design/service-design.md`（服务职责）
> - `../../../docs/design/data-model.md`（数据模型）
> - `../../../docs/agent/dev-guidelines.md`（编码规范）
> - `../../../docs/agent/proto-writing-guide.md`（proto 规范）
>
> 同时参考已落地的 `app/user/rpc/` 和 `app/relation/rpc/` 作为代码样板。

---

## 目标

根据项目现有规范和 `docs/` 文档，从零实现一个符合 go-zero 工程结构的 Feed 服务，并提交到新分支。

## 前置要求

- 已阅读 `AGENTS.md`、`docs/design/service-design.md`、`docs/design/data-model.md`
- 已安装 `goctl` 工具
- 已能本地启动 MySQL / Redis / etcd（`make up`）

---

## 第一阶段：工程准备

### 步骤 1：创建功能分支

```bash
git checkout -b feature/feed-module
```

### 步骤 2：确认现有服务结构

- 查看 `app/user/rpc/` 和 `app/relation/rpc/` 的目录组织。
- 理解 `internal/config`、`internal/svc`、`internal/logic`、`internal/server` 的分层职责。
- 理解 model 目录 `app/<svc>/model/` 与 rpc 目录的关系。

---

## 第二阶段：协议与接口定义

### 步骤 3：编写 `api/proto/feed/feed.proto`

- 定义 `FeedType`、`FeedStatus`、`FeedTab` 枚举。
- 定义 `FeedInfo` 领域消息。
- 定义请求/响应：
  - `CreateFeedReq/Resp`
  - `GetFeedReq/Resp`
  - `BatchGetFeedsReq/Resp`
  - `ListFeedsReq/Resp`
  - `GetUserFeedsReq/Resp`
  - `DeleteFeedReq/Resp`
- 定义 `PageInfo` 分页信息。
- 定义 `service Feed`。

### 步骤 4：生成 RPC 骨架

```bash
goctl rpc protoc api/proto/feed/feed.proto \
  --go_out=app/feed/rpc \
  --go-grpc_out=app/feed/rpc \
  --zrpc_out=app/feed/rpc --style=goZero
```

### 步骤 5：查看生成产物

- `feed.go`：服务入口。
- `etc/feed.yaml`：配置文件。
- `internal/config/config.go`：配置结构。
- `internal/svc/servicecontext.go`：依赖注入容器。
- `internal/server/feedserver.go`：gRPC 服务注册。
- `feed/feed.pb.go`、`feed/feed_grpc.pb.go`：协议代码。
- `feedclient/feed.go`：客户端封装。

**注意**：凡是带 `DO NOT EDIT` 标记的生成文件都不要手动修改。

---

## 第三阶段：数据层

### 步骤 6：编写 `deploy/sql/feed.sql`

- 创建 `feed_feed` 数据库。
- 创建 `feeds` 表，包含帖子类型、媒体、城市、状态、计数、时间等字段。
- 添加索引：
  - `idx_user_created`
  - `idx_city_created`
  - `idx_status_created`
  - `idx_created_at`

### 步骤 7：执行建表

```bash
mysql -h 127.0.0.1 -P 3306 -u root -p feed_feed < deploy/sql/feed.sql
```

### 步骤 8：生成 Model 代码

```bash
goctl model mysql ddl \
  --src deploy/sql/feed.sql \
  --dir app/feed/model \
  --cache true \
  --style goZero
```

### 步骤 9：扩展 Model 自定义方法

在 `app/feed/model/feedsmodel.go` 中补充：

- `FindByUserId`：按用户查主页帖子。
- `FindByCityCode`：按城市查同城帖子。
- 如有需要，补充软删除更新方法。

---

## 第四阶段：服务基础设施

### 步骤 10：完善 `internal/config/config.go`

- 内嵌 `zrpc.RpcServerConf`。
- 添加 `Mysql.DataSource`。
- 添加 `CacheRedis`。
- 添加 `UserRpc`、`RelationRpc` 客户端配置。

### 步骤 11：完善 `etc/feed.yaml`

- 端口设为 `9003`。
- etcd 注册到 `127.0.0.1:2479`。
- MySQL 指向 `feed_feed`。
- Redis 复用现有配置。
- 配置 `UserRpc` 和 `RelationRpc`。

### 步骤 12：完善 `internal/svc/servicecontext.go`

- 初始化 `FeedsModel`。
- 初始化 Redis 客户端。
- 初始化 `idgen.Next`。
- 初始化 `UserRpc`、`RelationRpc` 客户端。

### 步骤 13：注册错误拦截器

参考 `app/relation/rpc/internal/serverinterceptors/error.go`，在 Feed 服务相同位置创建拦截器，并在 `feed.go` 中注册。

---

## 第五阶段：业务逻辑实现

### 步骤 14：创建 `internal/logic/keys.go`

集中定义 Redis Key：

| Key | 类型 | 说明 |
|---|---|---|
| `feed:{feed_id}` | Hash | 帖子详情缓存 |
| `feed:outbox:{user_id}` | ZSet | 发件箱 |
| `feed:inbox:{user_id}` | ZSet | 收件箱 |
| `feed:recommend` | ZSet | 推荐池 |
| `feed:city:{city_code}` | ZSet | 同城池 |
| `timeline:{user_id}:{tab}` | String | 前2页缓存 |

### 步骤 15：实现 `createFeedLogic.go`

- 参数校验（作者、内容、类型）。
- 生成 Snowflake ID。
- 写入 MySQL `feeds` 表。
- 异步写入自己的 `outbox`。
- 异步加入推荐池、同城池。
- 异步清除自己的 timeline 缓存。

### 步骤 16：实现 `getFeedLogic.go`

- 参数校验。
- 先查 Redis `feed:{feed_id}`。
- 未命中回源 MySQL，回填缓存。
- 软删除帖子返回 `FeedNotFound`。

### 步骤 17：实现 `batchGetFeedsLogic.go`

- 批量查询帖子详情。
- 优先走 Redis，未命中批量回源 MySQL。
- 返回 `[]FeedInfo`。

### 步骤 18：实现 `listFeedsLogic.go`（核心）

- 支持三种 `tab`：关注流、推荐流、同城流。
- **关注流**：
  - 拉取 `inbox:{user_id}`。
  - 调用 Relation 服务获取关注列表。
  - 对关注的大 V 拉取 `outbox:{vip_id}`。
  - 按时间戳合并排序、分页。
- **推荐流**：从 `feed:recommend` ZSet 按 score 倒序取。
- **同城流**：从 `feed:city:{city_code}` 按时间倒序取。
- 前 2 页缓存到 `timeline:{user_id}:{tab}`，TTL 60 秒。

### 步骤 19：实现 `getUserFeedsLogic.go`

- 调用 model 的 `FindByUserId`。
- 转换为 `FeedInfo` 列表。
- 返回分页信息。

### 步骤 20：实现 `deleteFeedLogic.go`

- 校验帖子存在。
- 校验操作者是否为作者。
- MySQL 软删除（status = 2）。
- 删除 Redis `feed:{feed_id}`。
- 从 `outbox`、`recommend`、`city` 中移除。
- 清除相关 timeline 缓存。

---

## 第六阶段：工具函数与组装

### 步骤 21：创建 `internal/logic/convert.go`

- `buildFeedInfo(model *model.Feeds) *feed.FeedInfo`：model → proto。
- `parseIds([]string) []int64`：Redis member 解析。
- `feedIdsToMembers([]int64) []string`：ID 转字符串。

### 步骤 22：实现 `internal/server/feedserver.go`

确认 gRPC 方法已正确注册到生成的 server，必要时手动调整。

### 步骤 23：调整 `feed.go` 入口

- 调用 `idgen.Init(1)`。
- 加载配置。
- 创建 ServiceContext。
- 注册 FeedServer 和错误拦截器。
- 启动服务。

---

## 第七阶段：验证与提交

### 步骤 24：格式化与编译

```bash
gofmt -w .
go build ./app/feed/rpc/...
```

### 步骤 25：启动依赖服务

```bash
make up
# 启动 user、relation 服务
cd app/user/rpc && go run user.go -f etc/user.yaml
cd app/relation/rpc && go run relation.go -f etc/relation.yaml
```

### 步骤 26：启动 Feed 服务

```bash
cd app/feed/rpc
go run feed.go -f etc/feed.yaml
```

### 步骤 27：接口自测

- 使用 `grpcurl` 或 Go 测试客户端调用 `CreateFeed`。
- 调用 `GetFeed` 验证帖子详情。
- 调用 `ListFeeds` 验证三种流。
- 调用 `DeleteFeed` 验证软删除。

### 步骤 28：补充单元测试

- `createFeedLogic_test.go`
- `getFeedLogic_test.go`
- `listFeedsLogic_test.go`

### 步骤 29：提交代码

```bash
git add .
git commit -m "feat(feed): 实现 Feed 服务核心接口"
```

---

## 每个步骤的验收标准

| 步骤 | 验收标准 |
|---|---|
| Proto | `make proto` 能成功生成 Feed 代码 |
| Model | `go build ./app/feed/model/...` 通过 |
| Config | `etc/feed.yaml` 与 `config.go` 字段一一对应 |
| CreateFeed | 发帖后数据库有记录，outbox 有数据 |
| GetFeed | 能命中 Redis 缓存 |
| ListFeeds | 关注流、推荐流、同城流都能返回结果 |
| DeleteFeed | 删除后 GetFeed 返回帖子不存在 |
| 整体 | `go build ./...` 通过 |

---

## 后续可扩展

- 接入 RocketMQ，发帖后异步推送到粉丝 inbox。
- 大 V 判定与拉模式合并。
- 接入 Interaction 服务获取实时点赞/收藏数。
- 图片/视频上传与 MinIO 集成。
