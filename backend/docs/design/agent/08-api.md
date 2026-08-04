# Agent 对外 HTTP API 设计

> 定义 Agent 独立 HTTP 服务（go-zero rest，端口 8090）的 REST 接口契约、鉴权方式与 DeepSeek（OpenAI 兼容）LLM 配置约定。

---

## 1. 概述与定位

- 独立服务，不经过现有 Gateway（8080）；复用同一套 JWT 签发体系（`common/jwtx`），token 与主站通用。
- 响应包装沿用 `common/response` 的统一结构（`code/msg/data`）。
- 首版为**同步发起 + 轮询查询**模型；流式（SSE）列入演进项。

## 2. 架构与职责（路由清单）

前缀 `/api/v1/agent`，全部需要 JWT（`Authorization: Bearer <token>`）。

| 方法 | 路径 | 用途 |
|---|---|---|
| POST | `/sessions` | 创建会话 |
| GET | `/sessions` | 我的会话列表（分页） |
| GET | `/sessions/:session_id` | 会话详情（含摘要） |
| POST | `/sessions/:session_id/runs` | 发起任务（用户自然语言指令） |
| GET | `/runs/:run_id` | 查询 run 状态与结果（含审批计划） |
| POST | `/runs/:run_id/approve` | 批准执行（可部分批准） |
| POST | `/runs/:run_id/reject` | 拒绝计划 |
| POST | `/runs/:run_id/cancel` | 取消运行中/待审批的 run |
| POST | `/recommendations/:record_id/feedback` | 推荐反馈回收（点击/点赞/负反馈） |

### 2.1 核心接口契约

**发起任务** `POST /sessions/:session_id/runs`

```json
// 请求
{ "input": "给我推荐几部最近没看过的视频" }

// 响应 202（异步执行中）
{ "code": 0, "msg": "ok", "data": { "run_id": "1948390123456", "status": "running" } }
```

**查询 run** `GET /runs/:run_id`

```json
{
  "code": 0, "msg": "ok",
  "data": {
    "run_id": "1948390123456",
    "status": "awaiting_approval",        // 状态机见 04-approval.md
    "result": null,                        // 终态时为结构化结果（05-scenarios.md 契约）
    "plan": { "plan_id": "…", "summary": "计划修改 8 个视频标题", "items": [ … ] },
    "usage": { "token_input": 5210, "token_output": 830 }
  }
}
```

**审批** `POST /runs/:run_id/approve`

```json
// 请求：item_ids 省略 = 整单批准；传子集 = 部分批准
{ "item_ids": ["p1", "p3"] }

// 响应：进入 executing，前端继续轮询 GET /runs/:run_id
{ "code": 0, "msg": "ok", "data": { "run_id": "1948390123456", "status": "executing" } }
```

### 2.2 鉴权与越权防护

- JWT 解析出 `user_id` 后写入 context；**所有** session/run/record 访问校验属主（`resource.user_id == JWT.user_id`），非属主返回 60002。
- 运营类意图（诊断/批量修改）要求会话 `role=operator`；角色由服务端根据运营白名单判定，**不接受**客户端声明。
- 用户输入仅作为自然语言处理，工具参数中的身份字段一律来自服务端上下文（防提示注入越权，详见 [02-tools.md](./02-tools.md) §2.1）。
- 限流：单用户并发 run ≤ 2、每日 run 数上限（配置），超限返回 429 语义错误码。

## 3. 数据模型

无新增表；接口读写 [03-state-session.md](./03-state-session.md) 定义的四张表。

## 4. 接口与契约（配置）

`app/agent/etc/agent.yaml`（示例，密钥一律环境变量）：

```yaml
Name: agent-api
Host: 0.0.0.0
Port: 8090

Auth:
  AccessSecret: ${JWT_SECRET}        # 与 Gateway 同源

LLM:
  BaseUrl: https://api.deepseek.com  # OpenAI 兼容端点
  Model: deepseek-chat
  ApiKeyEnv: AGENT_LLM_API_KEY       # 只存环境变量名，启动时读取
  TimeoutSec: 60
  MaxIterations: 6                   # decide 循环上限
  ToolBudget: 20                     # 单 run 工具调用预算

Mysql:
  DataSource: ${AGENT_MYSQL_DSN}     # feed_agent 库

Redis:
  Host: ${REDIS_ADDR}

Downstream:                          # zrpc 客户端，etcd 服务发现
  Etcd:
    Hosts: [127.0.0.1:2479]          # 注意本地 etcd 为 2479（仓库已知陷阱）
  Keys:
    User: user.rpc
    Relation: relation.rpc
    Feed: feed.rpc
    Interaction: interaction.rpc
    Comment: comment.rpc
```

安全约定：

- `AGENT_LLM_API_KEY`、DSN、JWT secret 仅来自环境变量/配置中心，禁止硬编码与入库。
- LLM `BaseUrl` 白名单校验（仅允许配置的公网端点），禁止运行时被改写为内网地址（SSRF 防护）。
- 用户输入与模型输出在前端展示前按 HTML 转义（XSS 防护，推荐理由等为模型生成文本）。

## 5. 错误码

复用 [01-architecture.md](./01-architecture.md) §5 的 60000 段；HTTP 层统一 200 + 业务 code（沿用仓库 Gateway 惯例），系统级异常 5xx。

## 6. 缓存与一致性

`GET /runs/:run_id` 直接读 DB（低频轮询，不缓存），避免审批状态的缓存不一致引发重复审批。

## 7. 测试策略

- handler 层：JWT 缺失/过期、越权访问他人 run、部分批准参数校验。
- 集成：完整 V1 链路（创建会话 → 发起 → 轮询 → 结果落表）与 V3 链路（发起 → 待审批 → approve → 执行报告）。
- 压测：`scripts/benchmark-agent.sh`（规划），`hey` 压 `POST /runs` 验证限流与降级。

## 8. 演进与 TODO

- [ ] SSE 流式：`GET /runs/:run_id/events` 推送节点级进度（Eino callback 已有切入点）。
- [ ] 审批 Webhook / IM 通知（当前依赖前端轮询发现待审批状态）。
- [ ] 对外契约稳定后，在 `../api-spec/` 下新增 `agent.md` 模块并互链。

## 关联文档

- [Agent 服务总览](./00-overview.md)
- [架构与编排设计](./01-architecture.md)
- [审批流程设计](./04-approval.md)
- [状态与会话设计](./03-state-session.md)
- [API 契约总览](../api-spec/README.md)
