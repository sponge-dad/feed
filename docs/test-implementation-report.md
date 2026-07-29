# P0 测试实施报告

> 记录基于 `docs/api-test-baseline.md`「测试实施优先级」P0 清单落地的自动化测试：实施范围、文件清单、基线编号覆盖、执行结果、发现的生产代码问题、基线与代码不一致项及后续 P1 建议。

---

## 1. 实施范围

本次仅实施 P0 级别测试（基线文档「测试实施优先级」第 1、2、3、5、6 项），未批量实施 P1/P2。

| P0 项 | 内容 | 状态 |
|---|---|---|
| P0-1 | User 服务 Logic 单测全套（register/login/getUser/updateUser/batchGetUsers） | 已完成（stub + miniredis） |
| P0-1 附 | `app/user/rpc/tests/integration_test.go` 集成版并发注册（U-REG-05） | 未实施（不在指定文件清单；并发同名注册已在 Logic 单测以 stub 1062 竞态覆盖，固化 R-P0-4 行为） |
| P0-2 | Feed 写路径单测（createfeed / deletefeed） | 已完成 |
| P0-3 | JWT/认证回归（Gateway httptest + jwtx 单测） | 已完成 |
| P0-4 | RPC 越权基线固化（`authz_baseline_test.go`，F-DEL-04 直连伪造 user_id） | 未实施（不在指定文件清单；需真实 RPC 直连环境。DeleteFeed 属主校验已在 Logic 单测覆盖 F-DEL-03） |
| P0-5 | 消息一致性演练（MQ 不可用） + errorx `[bizerror]` 编解码回归 | 已完成（MQ 故障以 Publisher stub 在 Logic 单测层固化，替代停机演练） |
| P0-6 | 唯一索引 Model 集成（users / relations / likes / collections 的 1062） | 已完成（真实 MySQL + 探活 Skip + 自清理） |

## 2. 新增 / 修改文件清单

### 2.1 新增测试文件（14 个，共 79 个顶层测试函数）

| 文件 | 用例数 | 覆盖 |
|---|---|---|
| `common/jwtx/jwtx_test.go` | 10 | J-JWT 系列：签发/解析、user_id 字符串化、错 secret、过期、篡改、alg=none、nbf、边界 ID、并发 |
| `common/errorx/grpc_test.go` | 10 | E-GX 系列：`ToGRPCError`/`FromGRPCError`/`TryParse` 编解码回归 + Feed 无拦截器退化基线 |
| `app/user/rpc/internal/logic/test_helpers_test.go` | - | `stubUsersModel`（含 uk_username 1062 模拟）+ miniredis 测试环境 |
| `app/user/rpc/internal/logic/registerLogic_test.go` | 8 | U-REG-01/02/03；并发同名注册（R-P0-4 基线固化） |
| `app/user/rpc/internal/logic/loginLogic_test.go` | 5 | U-LOG-01/02/03；防枚举一致性 |
| `app/user/rpc/internal/logic/getUserLogic_test.go` | 4 | U-GET-01/03/04 |
| `app/user/rpc/internal/logic/updateUserLogic_test.go` | 6 | U-UPD-01/02；`user:brief` 缓存不失效基线（R-P1-3） |
| `app/user/rpc/internal/logic/batchGetUsersLogic_test.go` | 8 | U-BGU-01~08：缓存命中/部分回源回填/降级/保序 |
| `app/feed/rpc/internal/logic/feed_write_test_helpers_test.go` | - | `recordingPublisher`、`ctrlFeedsModel`、`errRelation` |
| `app/feed/rpc/internal/logic/createfeedlogic_test.go` | 7 | F-CRT-01~04；参数校验矩阵；IsVip 失败行为；MQ 失败一致性（R-P0-2） |
| `app/feed/rpc/internal/logic/deletefeedlogic_test.go` | 9 | F-DEL-01/02/03；幂等；事件字段；MQ 失败一致性（R-P0-2） |
| `app/gateway/internal/handler/auth_handler_test.go` | 5 | U-LOG-04、U-ME-01/03：401 矩阵、user_id 透传、claim 类型兼容基线 |
| `app/interaction/rpc/internal/logic/mq_failure_consistency_test.go` | 3 | R-P0-2：点赞/收藏 Redis 成功 + `interaction-event` 发送失败/Producer 为 nil |
| `app/user/model/usersmodel_integration_test.go` | 1 | U-REG-04：uk_username 1062（真实 MySQL） |
| `app/relation/model/relationsmodel_integration_test.go` | 1 | R-FL-04：uk_follow 1062（真实 MySQL） |
| `app/interaction/model/interactionmodel_integration_test.go` | 2 | P0-6：likes / collections uk_user_feed 1062（真实 MySQL） |

### 2.2 修改的生产代码（1 个文件，行为不变）

- `app/feed/rpc/internal/svc/servicecontext.go`：将 `Producer *mq.Producer` 抽象为接口 `Producer Publisher`（`SendSync(topic string, body []byte) error`），与 Interaction 服务既有做法一致，仅为依赖注入，生产行为无任何变化（`*mq.Producer` 天然满足该接口）。

## 3. 已实现基线编号

- **User**：U-REG-01/02/03/04、U-LOG-01/02/03/04、U-GET-01/03/04、U-UPD-01/02、U-BGU-01~08
- **Feed**：F-CRT-01/02/03/04、F-DEL-01/02/03
- **Gateway/JWT**：U-ME-01/03、J-JWT-01~10
- **errorx**：E-GX-01~09
- **Relation Model**：R-FL-04
- **风险基线固化**：R-P0-2（Feed created/deleted、Interaction like/collect 四条 MQ 失败路径）、R-P0-4（并发注册 1062 未兜底）、R-P1-1（Feed 无 ErrorInterceptor 错误码退化）、R-P1-3（`user:brief` 脏读窗口）

## 4. 执行结果

执行环境：本机 MySQL 8.0 / Redis 可用（Model 集成测试实跑未 Skip；无 MySQL 环境时自动 `t.Skip`）。

| 命令 | 结果 |
|---|---|
| `go test ./app/user/rpc/internal/logic -count=1` | PASS（31 个顶层用例） |
| `go test ./app/feed/rpc/internal/logic -count=1` | PASS（32 个，含既有用例） |
| `go test ./app/gateway/internal/handler -count=1` | PASS（5 个） |
| `go test ./common/jwtx ./common/errorx -count=1` | PASS（10 + 10 个） |
| `go test ./app/interaction/rpc/internal/logic -count=1` | PASS（31 个，含既有用例，其中新增 3 个） |
| `go test ./app/{user,relation,interaction}/model -count=1` | PASS（4 个集成用例，真实 MySQL） |
| `go test ./... -count=1` | 除 2 个**既有**集成测试包外全部 PASS（见 4.1） |
| `go test -race ./... -count=1` | 同上；本次新增/修改的所有包在 `-race` 下全部 PASS |

### 4.1 既有失败（非本次引入，均未改动）

1. **`app/relation/rpc/tests`**：`relation-test.yaml` 的 `ListenOn: 0.0.0.0:9003` 与本机正在运行的 `feed-rpc` 开发实例（9003 端口）冲突，`TestMain` 启动即失败。属环境/配置冲突。
2. **`app/interaction/rpc/tests`**：`TestIntegration_LikeMainFlow` / `CollectMainFlow` / `ColdKeyRebuild`（`-race` 下另有 2 个并发用例）失败。根因分析：取消点赞后 `like:feed:{id}` Set 变空被 Redis 自动删除，状态查询把"key 不存在"当冷 key 触发 DB 回源重建，而此时消费者已落库 status=1、unlike 事件尚未消费，读到旧状态 `IsLiked=true` —— 属实现层一致性竞态（详见第 5 节缺陷 6），非测试代码问题。

失败/跳过统计：本次新增 79 个用例 **79 通过 / 0 失败 / 0 跳过**（有 MySQL 环境）；无 MySQL 时 4 个 Model 集成用例转为 Skip。

## 5. 发现的生产代码问题（未修改代码，仅记录）

1. **R-P0-2（高）MQ 发送失败无补偿**：CreateFeed/DeleteFeed DB 已提交、点赞/收藏 Redis 已写入后，MQ 发送失败仅记日志，接口仍返回成功；事件永久丢失（帖子永不进时间线 / 互动永不落库），无重试、无对账。
2. **R-P0-4 并发注册 1062 未兜底**：`registerLogic` 对 Insert 的 `mysql.MySQLError 1062` 无识别，并发同名注册时败者收到裸 DB 错误（退化为 code=1），而非 10001。
3. **R-P1-1 Feed 服务未注册 `ErrorInterceptor`**：Feed Logic 返回的业务码（12001/12002/...）经 Gateway `TryParse` 失败统一退化为 `ServerError(1)`。
4. **R-P1-3 `user:brief:{id}` 快照缓存不失效**：UpdateUser 只失效 goctl 行缓存，brief 快照最长 600s 脏读。
5. **JWT 数字型 `user_id` claim 兼容缺口**：go-zero JWT 中间件将数字 claim 解码为 `json.Number`，`middleware.UserIDFromContext` 的类型断言（string/float64/int64/int）不含该类型，返回 0 → 触发"getMe 静默成功返回 null"链路（关联 R-P2-1）。
6. **Interaction 空 Set 冷 key 误判竞态（既有集成测试失败根因）**：取消互动使 Redis Set 变空即被删除，读路径误判为冷 key 回源 DB，读到异步消费前的旧状态；建议为空集合保留哨兵成员或以 TTL 标记区分"空"与"未加载"。
7. **`relation-test.yaml` 端口冲突**：测试配置监听 9003 与 Feed 服务默认端口重叠。
8. **DeleteFeed 未删除 `feed:{id}` 缓存**：基线宣称"DEL feed:{id}"，当前 Logic 实现无任何 Redis 操作，已删帖详情缓存最长残留至 TTL（读端 status 过滤兜底）。

## 6. 基线与代码不一致项（以代码实际行为为准固化）

| # | 基线预期 | 代码实际行为 | 固化测试 |
|---|---|---|---|
| 1 | 空用户名/密码注册报参数错误 | RPC 层无校验，注册成功 | `registerLogic_test.go` |
| 2 | GetUser 非法 ID（0/负）报参数错误 | 按查无处理返回 10003 | `getUserLogic_test.go` |
| 3 | U-ME-03：数字型 user_id claim 可兼容读取 | `json.Number` 未覆盖，返回 0 | `auth_handler_test.go` |
| 4 | F-CRT：IsVip RPC 失败降级为非大V继续发布 | 整个请求失败 | `createfeedlogic_test.go` |
| 5 | F-DEL：feed 不存在返回 12001 | 返回裸 `model.ErrNotFound` | `deletefeedlogic_test.go` |
| 6 | F-DEL：删除后 DEL `feed:{id}` | Logic 无 Redis 删除操作 | `deletefeedlogic_test.go` |
| 7 | BatchGetUsers 重复 ID 去重 | 重复返回 | `batchGetUsersLogic_test.go` |
| 8 | Feed 业务码经网关透传 | 无拦截器，退化为 code=1 | `grpc_test.go` |

## 7. 后续 P1 工作建议（本次未实施）

1. Gateway 缺失 Logic 单测（register/login/getUser/getMe/updateMe、createFeed、getFeedDetail、userFeeds、listReplies）。
2. FeedCard 聚合降级断言（R-P1-5 镜像 0 值基线、comment_count 数据源）。
3. comment-event 去重缺陷复现（`worker_dedup_test.go`，E-CE-03）。
4. Relation Total 语义与回源（R-LS-02）、Follow/Unfollow 交错并发收敛（R-UF-03）。
5. Interaction 弱测试加强（BatchGetUserInteractionStatus 逐项断言、>1000 回源边界）。
6. 为 `app/relation/rpc/tests`、`app/interaction/rpc/tests` 既有集成测试补环境探活 Skip 与独立端口，使 `go test ./...` 在无基础设施时可全绿（P1-6）；并修复第 5 节缺陷 6/7 后恢复其稳定性。
7. P0 遗留：`app/user/rpc/tests` 集成版并发注册（U-REG-05）、`app/feed/rpc/tests/authz_baseline_test.go`（F-DEL-04 直连越权红线）。

## 关联文档

- [API 测试基线](./api-test-baseline.md)
- [开发规范](./agent/dev-guidelines.md)
- [服务设计总览](./design/service-design.md)
- [数据模型](./design/data-model.md)
