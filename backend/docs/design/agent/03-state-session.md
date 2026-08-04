# Agent 状态与会话设计

> 定义 Agent 的状态三层模型（当前任务状态 / 近期会话摘要 / 长期用户偏好）与 `feed_agent` 库四张表的 DDL、索引及 Redis key 约定。核心原则：**不把全量聊天记录传给模型**。

---

## 1. 概述与定位

状态分三层，各有不同的载体与生命周期：

| 层 | 内容 | 载体 | 生命周期 |
|---|---|---|---|
| 当前任务状态 | 本次 run 的意图、已调工具结果、方案草稿、审批计划 | Eino Graph State（内存）+ checkpoint 落 `agent_runs` | 单次 run |
| 近期会话摘要 | 最近 N 轮对话的 LLM 摘要（≤500 字） | `agent_sessions.summary` + Redis 缓存 | 会话内滚动更新 |
| 长期用户偏好 | 题材偏好、活跃时段、推荐反馈统计 | `recommendation_records` 聚合 + Redis | 跨会话持久 |

模型每轮实际收到的上下文 = 系统 Prompt + 会话摘要 + 长期偏好摘要 + 当前 run 的事实上下文，**不含**历史原始消息。

## 2. 架构与职责

### 2.1 Graph State（当前任务状态）

```go
// AgentState Eino Graph 全局状态（示意）
type AgentState struct {
    RunID       int64             // 当前 run
    UserID      int64             // JWT 解析，不可被模型改写
    Role        string            // user / operator
    Intent      *Intent           // 意图识别结果（任务类型 + 条件）
    Facts       []ToolFact        // 工具返回的结构化事实（含 source）
    ToolBudget  int               // 剩余工具调用预算
    Iterations  int               // decide 循环轮数
    Proposal    json.RawMessage   // 方案草稿（推荐列表/诊断/修改计划）
    PendingPlan *ApprovalPlan     // 待审批计划（仅写操作场景）
}
```

审批中断时整个 State + Graph checkpoint 序列化（JSON）写入 `agent_runs.checkpoint`，恢复时反序列化继续执行（见 [04-approval.md](./04-approval.md)）。

### 2.2 摘要策略

- 每次 run 结束后，将「用户消息 + 最终结论」追加摘要：调用一次 ChatModel 把旧摘要与新轮次压缩为 ≤500 字。
- 摘要更新是异步 goroutine，失败仅记日志不阻塞响应（与仓库缓存约定一致）。

## 3. 数据模型

独立库 `feed_agent`（建表脚本规划为 `deploy/sql/agent.sql`；注意仓库已知陷阱：MySQL 自动建表只执行一次，需手动执行新脚本）。

```sql
CREATE DATABASE IF NOT EXISTS `feed_agent` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
USE `feed_agent`;

-- 会话：一个用户与 Agent 的一段连续交互
CREATE TABLE IF NOT EXISTS `agent_sessions` (
    `id`         BIGINT UNSIGNED NOT NULL COMMENT 'Snowflake ID',
    `user_id`    BIGINT UNSIGNED NOT NULL COMMENT '所属用户',
    `role`       VARCHAR(16)     NOT NULL DEFAULT 'user' COMMENT 'user:普通用户 operator:运营',
    `title`      VARCHAR(128)    NOT NULL DEFAULT '' COMMENT '会话标题（首条消息截断）',
    `summary`    TEXT            NULL COMMENT '近期会话 LLM 摘要（<=500字）',
    `status`     TINYINT         NOT NULL DEFAULT 1 COMMENT '1:活跃 2:归档',
    `created_at` DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at` DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    KEY `idx_user_updated` (`user_id`, `updated_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Agent 会话表';

-- 运行：会话内一次任务执行（一次用户指令触发一个 run）
CREATE TABLE IF NOT EXISTS `agent_runs` (
    `id`           BIGINT UNSIGNED NOT NULL COMMENT 'Snowflake ID',
    `session_id`   BIGINT UNSIGNED NOT NULL,
    `user_id`      BIGINT UNSIGNED NOT NULL COMMENT '冗余，便于越权校验',
    `user_input`   TEXT            NOT NULL COMMENT '用户原始指令',
    `intent`       JSON            NULL COMMENT '意图识别结果',
    `status`       VARCHAR(24)     NOT NULL DEFAULT 'running'
        COMMENT 'running / awaiting_approval / approved / rejected / executing / succeeded / failed / cancelled / expired',
    `result`       JSON            NULL COMMENT '最终结构化结果（推荐/诊断/执行报告）',
    `checkpoint`   MEDIUMTEXT      NULL COMMENT '审批中断时的 Graph checkpoint（JSON）',
    `error_msg`    VARCHAR(1024)   NOT NULL DEFAULT '',
    `token_input`  INT UNSIGNED    NOT NULL DEFAULT 0 COMMENT '输入 token 总量',
    `token_output` INT UNSIGNED    NOT NULL DEFAULT 0 COMMENT '输出 token 总量',
    `started_at`   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `finished_at`  DATETIME        NULL,
    PRIMARY KEY (`id`),
    KEY `idx_session_started` (`session_id`, `started_at`),
    KEY `idx_status` (`status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Agent 任务运行表';

-- 工具调用留痕：每次工具执行一行
CREATE TABLE IF NOT EXISTS `agent_tool_calls` (
    `id`          BIGINT UNSIGNED NOT NULL COMMENT 'Snowflake ID',
    `run_id`      BIGINT UNSIGNED NOT NULL,
    `seq`         INT             NOT NULL COMMENT 'run 内调用序号，从 1 开始',
    `tool_name`   VARCHAR(64)     NOT NULL,
    `input`       JSON            NULL COMMENT '入参（模型生成，经校验后的最终值）',
    `output`      JSON            NULL COMMENT '出参（超长截断存摘要 + 完整长度）',
    `ok`          TINYINT         NOT NULL DEFAULT 1 COMMENT '1:成功 0:失败',
    `error_msg`   VARCHAR(1024)   NOT NULL DEFAULT '',
    `cost_ms`     INT UNSIGNED    NOT NULL DEFAULT 0 COMMENT '耗时毫秒',
    `created_at`  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    KEY `idx_run_seq` (`run_id`, `seq`),
    KEY `idx_tool_created` (`tool_name`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Agent 工具调用留痕表';

-- 推荐记录：推荐结果 + 用户后续行为回收（长期偏好的数据源）
CREATE TABLE IF NOT EXISTS `recommendation_records` (
    `id`          BIGINT UNSIGNED NOT NULL COMMENT 'Snowflake ID',
    `run_id`      BIGINT UNSIGNED NOT NULL,
    `user_id`     BIGINT UNSIGNED NOT NULL,
    `feed_id`     BIGINT UNSIGNED NOT NULL COMMENT '被推荐的帖子',
    `reason`      VARCHAR(512)    NOT NULL DEFAULT '' COMMENT '推荐理由（模型生成，仅展示）',
    `rank`        INT             NOT NULL DEFAULT 0 COMMENT '在本次推荐中的排序',
    `feedback`    TINYINT         NOT NULL DEFAULT 0 COMMENT '0:未知 1:点击 2:点赞 3:收藏 4:负反馈',
    `created_at`  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `feedback_at` DATETIME        NULL,
    PRIMARY KEY (`id`),
    KEY `idx_user_created` (`user_id`, `created_at`),
    KEY `idx_run` (`run_id`),
    UNIQUE KEY `uk_run_feed` (`run_id`, `feed_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='推荐记录与反馈表';
```

### 3.1 Redis key 约定（沿用现有 Redis，独立前缀）

| key | 类型 | 内容 | TTL |
|---|---|---|---|
| `agent:session:summary:{session_id}` | string | 会话摘要缓存 | 24h |
| `agent:pref:{user_id}` | hash | 长期偏好聚合（题材计数、负反馈作者等） | 7d，写 DB 后刷新 |
| `agent:run:lock:{run_id}` | string(NX) | run 恢复执行互斥锁 | 60s |

### 3.2 长期偏好聚合

定时（或 run 结束时惰性）从 `recommendation_records` 聚合：各 `feedback` 类型按 feed 维度回查 `feeds` 元数据统计题材/作者分布。**注**：题材维度依赖后端补 `category` 列（[06-backend-gaps.md](./06-backend-gaps.md) §2），补齐前偏好仅含作者与互动率维度。

## 4. 接口与契约

四张表通过 `goctl model` 生成基础 CRUD，复杂查询（如按状态批量捞超时 run）在 `customAgentRunModel` 中扩展，遵循仓库 model 层约定。

## 5. 错误码

见 [01-architecture.md](./01-architecture.md) §5（60001/60002 与会话、run 相关）。

## 6. 缓存与一致性

- 摘要与偏好：先写 DB，再删/更新 Redis；缓存失败仅日志（仓库通用约定）。
- `agent_runs.status` 是唯一事实源；任何恢复/取消操作先 `SELECT ... FOR UPDATE`（或乐观版本号）校验状态迁移合法性。

## 7. 测试策略

- 状态机测试：穷举 `agent_runs.status` 的合法/非法迁移。
- checkpoint 序列化回归：State 结构变更时旧 checkpoint 反序列化兼容（版本字段 `v`）。
- 摘要异步更新的收敛测试（参考仓库「缓存一致性窗口」已知陷阱）。

## 8. 演进与 TODO

- [ ] 长期偏好升级为向量库（embedding 检索）时，本文档 §3.2 需重写。
- [ ] `agent_tool_calls.output` 超长治理：>64KB 时仅存摘要与对象存储引用。

## 关联文档

- [Agent 服务总览](./00-overview.md)
- [架构与编排设计](./01-architecture.md)
- [审批流程设计](./04-approval.md)
- [可观测性设计](./07-observability.md)
- [数据模型总览](../data-model.md)
