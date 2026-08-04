# 验收标准与测试策略

> 定义测试分层、关键用例，以及需求文档验收标准到具体验证方法的映射。

---

## 1. 测试分层

沿用仓库既有分层（`AGENTS.md` §5）：

| 类型 | 位置 | 依赖 | 关注点 |
|------|------|------|--------|
| 单元 | `app/<svc>/rpc/internal/logic/*_test.go`、`internal/worker/*_test.go` | `miniredis` + model stub + 外部服务 mock | 阈值判定、参数校验、幂等分支、公式计算 |
| 集成 | `app/<svc>/rpc/tests/*_test.go` | 真实 MySQL/Redis + 启动服务 | 跨组件行为、权限、幂等、降级 |
| 契约 | `app/agent/rpc/tests/contract_*_test.go` | mock 模型 | Tool schema、输出校验器、结构字段断言 |
| 评测 | `scripts/eval-search.sh`、`scripts/eval-agent.sh` | 评测集 | 检索准确率、Tool 选择准确率 |
| 压测 | `scripts/benchmark-behavior.sh`、`scripts/benchmark-agent.sh` | `ghz`/`hey` | 消费吞吐、Run 并发、模型限流表现 |

测试环境隔离：`feed_content_test`、`feed_agent_test`、`feed_interaction_test`；Redis 使用独立 DB 或 key 前缀；外部模型/ASR/OCR 一律用 fake 实现（禁止在CI 中真实计费调用）。

## 2. 关键测试用例

各模块细化用例见各篇「测试策略」小节，此处只列跨模块的端到端用例：

| 编号 | 场景 | 期望 |
|------|------|------|
| E2E-1 | 发布视频 → 等待分析 → 查询画像 | 异步窗口内 `COMPLETED`，标签/摘要非空 |
| E2E-2 | 分析服务全挂 → 发帖与刷流 | 发帖成功、Timeline 正常，画像为「分析中/失败」 |
| E2E-3 | 重复投递同一 `feed-created` 3 次 | 1 条画像、外部模型调用 1 次 |
| E2E-4 | 刷流 → 上报全套行为 → 查指标 | 各指标与上报量一致；重复上报不增加 |
| E2E-5 | 刷流 → 点「为什么推荐」 | 返回结构化 source + reason_codes |
| E2E-6 | 创作者问「为什么播放差」 | 回答中的数字与 Tool 返回一致，含同类对比 |
| E2E-7 | 用 `request_id` 查 Trace | 能看到 inbox/outbox/推荐池/同城池各自返回量 |
| E2E-8 | 自然语言检索 | 返回的 feed 全部真实存在且状态正常 |
| E2E-9 | 全链路 `request_id` | 响应头/体一致，且在 4 个服务日志中可检索到 |
| E2E-10 | 越权组合（他人指标/兴趣/Trace） | 全部拒绝，无数据泄漏 |

## 3. 验收标准映射

| # | 需求验收标准 | 验证方法 | 通过判据 |
|---|--------------|----------|----------|
| A1 | 视频发布后异步窗口内生成内容画像 | E2E-1 + `content_analysis_duration_seconds` | 测试环境 P95 < 5min，状态 `COMPLETED` |
| A2 | 内容分析失败不影响发布与刷流 | E2E-2（注入模型/FFmpeg 故障） | 发帖与 Timeline 接口成功率100% |
| A3 | 同一 `feed-created` 重复消费只生成一条画像 | E2E-3 + `uk_feed_id` 断言 | 表中1 行，`retry_count` 不异常增长 |
| A4 | 检索结果真实存在且满足状态与权限 | E2E-8：索引中人为留下已删除 feed | 结果不含该 feed；全部结果经 `BatchGetFeeds` 校验通过 |
| A5 | 推荐原因包含结构化来源 | E2E-5：断言 `source` ∈ 五种枚举、`reasons[].code` 非空 | 无「仅泛化文本」的响应 |
| A6 | 创作者只能查本人数据 | E2E-10 |越权一律 `14005`/`16010` |
| A7 | 分析数字全部来自指标接口 | 数字一致性校验器（扫描回答中的数字，逐个在 Tool 结果 JSON 中查找） | 不一致率 0；不一致即降级并计入 `agent_llm_guard_total` |
| A8 | 每个请求都有非空 `request_id` 且可检索 | E2E-9 + `request_id_missing_total` | 响应体/头非空；4 个服务日志均能grep 到；指标为 0 |
| A9 | 行为事件重复消费不重复累加 | E2E-4（重复投递 5 次 + 重复 flush 2 次） | 指标值不变 |
| A10 | Tool 调用成功率 ≥ 99%（测试环境） | 压测 500 次覆盖8 个 Tool，读 `agent_tool_call_total` | `success / total ≥ 0.99` |
| A11 | 检索主题匹配准确率 ≥ 85% | `scripts/eval-search.sh`，100 条标注query | Precision@5 ≥ 0.85 |
| A12 | 无数据/权限不足/分析未完成/下游超时返回明确错误 | 四类故障注入用例 | 返回对应 `error_code`，回答含明确说明，无编造 |

## 4. 故障注入清单

| 注入 | 期望行为 |
|------|----------|
| Redis 不可用 | Timeline 正常（Trace 降级）；指标查询走 MySQL；兴趣更新失败重试 |
| MySQL 从库延迟 | 指标查询容忍延迟并在回答中标注数据时间 |
| ES 不可用 | 检索返回 `15007`，Agent 明确告知 |
| 模型超时 | 重试 1 次后 `16009`，Run `FAILED` 且可查原因 |
| ASR/OCR 失败 | 画像降级完成（`degraded=1`），不整单失败 |
| RocketMQ 不可用 | 埋点上报返回失败但不影响刷流；发帖仍成功（副作用延后） |
| 下游 RPC 超时 | Tool 返回 `UPSTREAM_TIMEOUT`，回答说明获取失败 |

## 5. 数据质量校验

上线后需持续校验（也是测试用例）：

| 校验 | 规则 | 异常处理 |
|------|------|----------|
| 播放量 ≤曝光量 | 比例 > 1 说明客户端曝光漏报 | 告警并核查上报逻辑 |
| 完播量 ≤ 播放量 | 同上 | 告警 |
| 快速划走 + 有效播放 ≤ 播放 | 逻辑一致性 | 告警 |
| 客户端时间偏差 | `abs(timestamp - server_time)` P99 < 5min | 超限丢弃并统计|
| 无曝光的播放占比 | < 5% | 超限排查埋点顺序 |

## 6. CI 要求

```bash
go build ./...
go test -race ./...
gofmt -l .          # 输出为空
```

新增要求：

- Content Worker 单测必须 mock FFmpeg（用假二进制或注入执行器接口），CI 不依赖真实ffmpeg。
- Agent 单测必须 mock ChatModel（固定返回 tool_calls 序列），保证可重复且零成本。
- 评测脚本不进 CI 门禁（耗时且需外部服务），改为发布前手动执行并归档结果。

## 7. 演进与 TODO

- 引入 Agent 回归评测集（意图分类准确率、Tool 选择准确率、拒答率）。
- 埋点 SDK 侧一致性测试（客户端与服务端口径对齐）。
- 影子流量对比：新旧解释文案的用户点击率。

---

## 关联文档

- [总览与定位](./00-overview.md)
- [自然语言内容检索](./05-content-search.md)
- [创作者作品表现分析](./08-creator-metrics.md)
- [Agent 服务设计](./09-agent-service.md)
- [可观测性](./12-observability.md)
- [安全要求](./13-security.md)
- [Feed 测试策略](../feed/08-test-strategy.md)
