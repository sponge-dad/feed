# AGENTS.md —— AI 协作指南

> 本文件面向与 Feed 项目协作的 AI Agent（如 CodeBuddy、Copilot 等）。
> 在回答本项目相关问题或修改代码前，请先阅读本文件，并遵循其中指引。

---

## 1. 项目定位

Feed 是一个**类抖音/小红书的分布式 Feed 流系统**，基于 **go-zero** 微服务框架构建。

- 语言：Go 1.21+
- 通信：内部 gRPC + Protobuf，对外 HTTP REST（Gateway）
- 存储：MySQL 8.0（主从）+ Redis 7
- 消息：RocketMQ
- 部署：Docker Compose（本地/CVM）+ Kubernetes（生产）

---

## 2. 开始工作前必读

根据你要做的事情，优先阅读对应规范：

| 场景 | 必读文档 |
|---|---|
| 写任何 `.go` 代码 | [`docs/agent/dev-guidelines.md`](docs/agent/dev-guidelines.md) |
| 新增/修改 REST API 文档 | [`docs/agent/api-writing-guide.md`](docs/agent/api-writing-guide.md) |
| 新增/修改 `.proto` | [`docs/agent/proto-writing-guide.md`](docs/agent/proto-writing-guide.md) |
| 新增/修改 `.api` 网关文件 | [`docs/agent/go-zero-api-writing-guide.md`](docs/agent/go-zero-api-writing-guide.md) |
| 新增/修改 Gateway 服务代码 | [`docs/agent/gateway-standard.md`](docs/agent/gateway-standard.md) |
| 新增/修订 `docs/` 下任何文档 | [`docs/agent/doc-writing-guide.md`](docs/agent/doc-writing-guide.md) |
| 了解系统架构 | [`docs/design/architecture.md`](docs/design/architecture.md) |
| 了解服务拆分 | [`docs/design/service-design.md`](docs/design/service-design.md) |
| 了解数据模型 | [`docs/design/data-model.md`](docs/design/data-model.md) |

**原则**：当 AI 的默认写法与上述规范冲突时，以本仓库规范为准。

---

## 3. 项目结构速览

```
feed/
├── api/proto/              # 内部 gRPC 契约
├── app/
│   ├── user/rpc/           # 用户服务（9001）
│   ├── relation/rpc/       # 关系服务（9002）
│   ├── feed/rpc/           # Feed 服务（9003）
│   ├── comment/rpc/        # 评论服务（9004）
│   ├── interaction/rpc/    # 互动服务（9005）
│   └── gateway/            # HTTP 网关（8080，已运行，仅接入 user/relation 路由）
├── common/                 # 公共代码：errorx / idgen / jwtx / response 等
├── deploy/
│   ├── sql/                # 建表脚本
│   └── docker-compose.yaml # MySQL/Redis/etcd/RocketMQ
├── docs/
│   ├── agent/              # AI 协作规范（必读）
│   ├── design/             # 系统设计文档
│   └── *-test-plan.md      # 各服务测试方案
├── scripts/                # 初始化与压测脚本
└── Makefile
```

---

## 4. 编码规范（核心摘要）

### 4.1 文件与注释

- 每个手写 `.go` 文件必须有文件级注释，说明职责。
- 导出函数/方法/结构体必须注释，首句以名称开头。
- `goctl`/`protoc` 自动生成的文件（带 `DO NOT EDIT.` 标记）禁止手动修改。

### 4.2 分层约定（go-zero 标准结构）

```
app/<svc>/rpc/
├── <svc>.go              # 服务入口
├── internal/
│   ├── config/           # 配置结构
│   ├── logic/            # 业务逻辑（核心）
│   ├── server/           # gRPC 服务注册
│   ├── svc/              # ServiceContext
│   └── serverinterceptors/ # 自定义拦截器
├── etc/                  # 配置文件
└── <svc>/                # goctl 生成的 pb/stub
```

- **logic 层**：只放业务逻辑，不直接调外部 HTTP / 不拼 SQL。
- **model 层**：通过 `goctl model` 生成，复杂查询在 `customXXXModel` 中扩展。
- **common 包**：跨服务通用能力，禁止引入任何 `app/xxx` 业务包。

### 4.3 错误处理

- 业务错误统一使用 `common/errorx` 中的错误码。
- RPC 服务已注册 `serverinterceptors.ErrorInterceptor`，会将业务错误码转换为 gRPC status error。
- 禁止直接返回裸 `errors.New("xxx")` 给上游。

### 4.4 缓存与数据库

- 读多写少的数据优先走 Redis，并通过 cache-aside 维护一致性。
- 写操作遵循：**先写 DB，再删/更新缓存**；缓存失败不阻塞主流程，但需日志记录。
- 高并发写必须考虑幂等（如唯一索引冲突应识别为已存在）。
- **例外**：Interaction 等超高频写场景可采用「Redis 先行 + MQ 异步落库」的削峰策略，详见 `docs/design/data-model.md` 第 5 节。

### 4.5 ID 生成

- 业务实体 ID 使用 `common/idgen`（Snowflake），禁止用数据库自增主键作为业务 ID。
- 单机开发固定机器 ID 为 1；生产多实例需通过环境变量注入唯一机器 ID。

---

## 5. 测试规范

### 5.1 测试分层

| 类型 | 位置 | 说明 |
|---|---|---|
| 单元测试 | `app/<svc>/rpc/internal/logic/*_test.go` | 用 `miniredis` + model stub，不依赖真实存储 |
| 集成测试 | `app/<svc>/rpc/tests/*_test.go` | 启动真实服务 + MySQL/Redis，验证端到端 |
| 并发/缓存测试 | `app/<svc>/rpc/tests/*_test.go` | 多 goroutine / 缓存一致性 |
| 压力测试 | `scripts/benchmark-<svc>.sh` | 使用 `ghz` 或 `hey` |

### 5.2 测试环境隔离

- 集成测试使用独立配置文件：`<svc>-test.yaml`
- MySQL 使用独立测试库：`feed_<svc>_test`
- Redis 使用独立 DB（如 `select 1`）或不同 key 前缀

### 5.3 运行测试

```bash
# 全部测试
go test -race ./...

# Relation 服务测试
go test ./app/relation/rpc/...

# 压测（Relation）
bash scripts/benchmark-relation.sh
```

---

## 6. 安全底线

编写代码时必须默认考虑以下安全风险：

1. **SQL 注入**：所有数据库操作必须参数化，禁止字符串拼接 SQL。
2. **RCE**：避免使用 `os/exec` 执行外部命令；必须使用时严格校验参数。
3. **鉴权/授权**：写接口必须校验用户身份，禁止越权操作。
4. **XSS**：对外输出数据需转义，尤其用户生成内容。
5. **SSRF**：禁止请求内网地址（`10.*`、`172.16-31.*`、`192.168.*`、`127.*` 等）。
6. **反序列化**：不信任外部输入，使用安全反序列化方法。
7. **Secrets**：密码、密钥、Token 只能来自环境变量或配置中心，禁止硬编码。
8. **并发安全**：共享状态加锁或使用原子操作；缓存更新注意竞态条件。

---

## 7. 提交规范

- 提交信息格式：`<type>(<scope>): <subject>`
- 常用 type：`feat`、`fix`、`test`、`refactor`、`docs`、`chore`
- scope：服务名（`user`、`relation`、`feed`、`common` 等）或模块名
- 示例：
  ```
  test(relation): 补充并发关注取关一致性测试
  fix(user): 修复 bcrypt 比较时密码为空导致的 panic
  ```
- 提交前确保：
  1. `go build ./...` 通过
  2. `go test ./...` 通过
  3. `gofmt -w .` 已执行

---

## 8. 常见任务速查

### 8.1 启动本地基础设施

```bash
make up
```

### 8.2 启动某个 RPC 服务

```bash
cd app/relation/rpc && go run relation.go -f etc/relation.yaml
```

### 8.3 重新生成 proto 代码

```bash
make proto
```

### 8.4 新增服务测试

1. 复制现有服务的测试结构（参考 `app/relation/rpc/tests/`）。
2. 创建 `<svc>-test.yaml` 指向独立测试库。
3. 在 `app/<svc>/rpc/tests/integration_test.go` 中写 `TestMain` 启动服务。
4. 运行 `go test ./app/<svc>/rpc/...` 验证。

### 8.5 新增 RPC 方法

1. 修改 `api/proto/<svc>/<svc>.proto`。
2. 执行 `make proto`。
3. 在 `internal/logic/` 下新增 `xxxLogic.go`。
4. 在 `internal/server/` 下注册方法。
5. 补充单元测试 + 集成测试。

---

## 9. 已知陷阱

1. **etcd 端口冲突**：本地开发 etcd 使用 `127.0.0.1:2479`，不是默认 2379，避免与 K8s 自带 etcd 冲突。
2. **MySQL 自动建表只执行一次**：后续新增表需手动执行 `deploy/sql/*.sql`。
3. **Snowflake 机器 ID 冲突**：生产多实例必须注入不同机器 ID，否则 ID 会重复。
4. **goctl 生成文件会覆盖手写内容**：`model/*_gen.go` 和 `pb` 目录下文件不要手动改。
5. **缓存一致性窗口**：当前实现中 Redis 更新是异步 goroutine，存在短暂不一致，测试需考虑收敛时间。

---

## 10. 协作原则

1. **先读规范再写代码**：特别是 `docs/agent/dev-guidelines.md`。
2. **小步提交**：一个 commit 只解决一件事，便于 review 和回滚。
3. **测试先行**：新增逻辑必须配套测试，修复 bug 必须配套回归测试。
4. **不破坏现有行为**：修改公共包或服务接口时，确保现有测试通过。
5. **保持文档同步**：修改接口、配置、架构时同步更新对应文档。

---

如有疑问，优先查阅 `docs/` 目录；若仍未解决，再询问人类开发者。
