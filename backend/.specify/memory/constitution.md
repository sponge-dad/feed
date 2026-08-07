<!--
Sync Impact Report:
  Version: 0.1.0 → 1.0.0
  All principles newly ratified based on AGENTS.md, docs/design/, and existing codebase.
  No previous version existed — template was blank.
-->

# Feed 后端项目宪法

> **Feed** 是一个类抖音/小红书的分布式 Feed 流系统，基于 go-zero 微服务框架构建。
> 本宪法定义项目不可妥协的核心原则、技术约束与治理规则。所有代码变更、架构决策、AI 协作必须遵循本宪法。

## 一、核心原则

### I. 微服务边界清晰 (Service Boundary Integrity)

- 每个微服务（user / relation / feed / comment / interaction）职责单一，通过 gRPC + Protobuf 通信，通过 Gateway 对外暴露 HTTP REST。
- **禁止跨服务直接访问数据库**：任何服务不得直接读写其他服务的 MySQL 表或 Redis key。
- 服务间调用必须走 gRPC 接口，不得通过消息队列绕过接口契约。
- 公共能力（errorx / idgen / jwtx / response）收敛在 `common/` 包，`common/` 禁止引入任何 `app/xxx` 业务包，避免循环依赖。

### II. 推拉结合 Feed 模型 (Push-Pull Hybrid)

- **普通用户发帖** → 推模式（写扩散）：异步 Worker 将帖子推送到所有粉丝的 Redis Inbox ZSet。
- **大 V 发帖**（粉丝 > 10 万）→ 拉模式（读扩散）：不推送到粉丝 Inbox，粉丝读取时从大 V Outbox 拉取并合并。
- 三种信息流策略不可混用：推荐流（全局池 + 随机打散 + 时间衰减）、关注流（inbox + 大 V outbox 合并）、同城流（城市池）。
- 任何对 Feed 分发策略的修改必须通过设计评审，并在 `docs/design/` 中同步更新。

### III. 数据安全优先 (Security First — NON-NEGOTIABLE)

以下红线不可妥协：
1. **SQL 注入**：所有数据库操作必须参数化，禁止字符串拼接 SQL。
2. **RCE**：避免 `os/exec`；必须使用时严格校验参数并记录日志。
3. **鉴权**：所有写接口必须校验 JWT 身份，禁止越权操作（如修改他人帖子）。
4. **XSS**：用户生成内容输出时必须转义。
5. **SSRF**：禁止请求内网地址（`10.*`、`172.16-31.*`、`192.168.*`、`127.*`）。
6. **Secrets**：密码、密钥、Token 只能来自环境变量或配置中心，**禁止硬编码**。
7. **并发安全**：共享状态必须加锁或原子操作；缓存更新注意竞态条件。
8. **幂等性**：高并发写操作必须考虑幂等（如唯一索引冲突应识别为已存在，而非报错）。

### IV. 数据一致性保障 (Data Consistency)

- 写操作遵循：**先写 MySQL，再删/更新 Redis 缓存**（cache-aside 模式）。
- 缓存更新失败不阻塞主流程，但必须记录错误日志。
- **例外**：Interaction（点赞/收藏）等超高频写场景采用「Redis 先行 + RocketMQ 异步落库」削峰策略，详见 `docs/design/data-model.md`。
- 所有涉及缓存的操作必须考虑缓存与 DB 的最终一致性窗口，测试需覆盖收敛时间。

### V. 测试分层与覆盖率 (Test Discipline)

| 层级 | 位置 | 要求 |
|------|------|------|
| 单元测试 | `internal/logic/*_test.go` | 使用 `miniredis` + model stub，不依赖真实存储 |
| 集成测试 | `tests/*_test.go` | 启动真实服务 + MySQL/Redis，端到端验证 |
| 并发/缓存测试 | `tests/*_test.go` | 多 goroutine / 缓存一致性场景 |
| 压力测试 | `scripts/benchmark-*.sh` | 使用 `ghz` 或 `hey` |

- **新增逻辑必须配套单元测试**。
- **修复 bug 必须配套回归测试**（先写能复现 bug 的测试，再修复）。
- 测试环境必须隔离：独立配置文件（`<svc>-test.yaml`）、独立 MySQL 库（`feed_<svc>_test`）、独立 Redis DB。

### VI. 编码规范与分层约定 (Code Standards)

- **分层严格**：logic 层放业务逻辑（不调外部 HTTP、不拼 SQL）；model 层通过 `goctl model` 生成，复杂查询在 `customXXXModel` 扩展；common 包仅放跨服务通用能力。
- **错误处理**：统一使用 `common/errorx` 错误码，禁止裸 `errors.New("xxx")` 返回给上游。gRPC 拦截器已自动转换业务错误码为 status error。
- **ID 生成**：业务实体使用 `common/idgen`（Snowflake），禁止数据库自增主键作为业务 ID。生产多实例必须注入唯一机器 ID。
- **注释**：每个手写 `.go` 文件必须有文件级注释；导出函数/方法/结构体必须注释，首句以名称开头。
- **代码生成文件**：`goctl` / `protoc` 自动生成的文件（带 `DO NOT EDIT.` 标记）禁止手动修改。
- 代码风格：提交前必须执行 `gofmt -w .`、确保 `go build ./...` 和 `go test ./...` 通过。

## 二、技术栈约束

| 类别 | 必须使用 | 禁止/限制 |
|------|---------|----------|
| 语言 | Go 1.21+ | 不引入其他后端语言 |
| 微服务框架 | go-zero v1.7.x | 不擅自升级主版本 |
| 服务通信 | gRPC + Protobuf (内部) / HTTP REST (对外) | 不在服务间使用 HTTP 直连 |
| 数据库 | MySQL 8.0 (主从) | 不引入其他关系型数据库 |
| 缓存 | Redis 7 (go-redis v9) | 不在 Redis 中存储持久化主数据 |
| 消息队列 | RocketMQ 5.1.4 | 不引入其他 MQ（Kafka 等） |
| 对象存储 | 腾讯云 COS (STS + 签名 URL) | 不在本地文件系统存持久化媒体 |
| 服务发现 | etcd (go-zero 内置) | 不在代码中硬编码服务地址 |
| 认证 | JWT (golang-jwt v5) | 不使用 session 或无状态 token |
| 部署 | Docker Compose (开发) / Kubernetes (生产) | 生产环境不直接 docker run |
| **工具链** | gofmt / go vet / go test -race | -- |

## 三、开发工作流

### 3.1 提交规范

- 格式：`<type>(<scope>): <subject>`
- type: `feat` / `fix` / `test` / `refactor` / `docs` / `chore`
- scope: 服务名（`user` / `relation` / `feed` / `comment` / `interaction` / `common` / `gateway`）
- 一个 commit 只解决一件事，便于 review 和回滚。

### 3.2 新增 RPC 方法

1. 修改 `api/proto/<svc>/<svc>.proto`
2. 执行 `make proto` 重新生成代码
3. 在 `internal/logic/` 新增 `xxxLogic.go`
4. 在 `internal/server/` 注册方法
5. 补充单元测试 + 集成测试
6. 同步更新 API 文档（`docs/design/api-spec/`）

### 3.3 文档同步

- 修改接口、配置、架构时必须同步更新 `docs/` 下对应文档。
- AI 协作者在编码前必须先阅读 `docs/agent/dev-guidelines.md`（编码规范）和 `docs/design/architecture.md`（系统架构）。

### 3.4 测试前自检

```bash
go build ./...    # 编译通过
go test ./...     # 测试通过
gofmt -w .        # 格式化
```

## 四、已知陷阱

1. **etcd 端口**：本地开发 etcd 使用 `127.0.0.1:2479`，非默认 2379（避免 K8s 冲突）。
2. **MySQL 自动建表**：仅首次启动执行，后续新增表需手动 `deploy/sql/*.sql`。
3. **Snowflake 机器 ID**：生产多实例必须不同，否则 ID 重复。
4. **goctl 生成文件**：`model/*_gen.go` 和 `pb/` 目录禁止手动修改。
5. **缓存一致性窗口**：Redis 异步更新存在短暂不一致，测试需考虑收敛时间。

## 五、治理

- 本宪法是项目最高技术准则，所有代码变更和架构决策必须符合本宪法。
- AGENTS.md、`docs/agent/` 下的编码规范是本宪法的实施细则，与本宪法冲突时以本宪法为准。
- 宪法修订需要：提出修订理由 → 在 `docs/` 中记录变更 → 更新版本号。
- 版本号遵循语义化版本：MAJOR（不兼容的原则变更）、MINOR（新增原则/约束）、PATCH（措辞修正）。

**Version**: 1.0.0 | **Ratified**: 2026-08-07 | **Last Amended**: 2026-08-07
