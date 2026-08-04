# 实施路线与演示脚本

> 定义五个阶段的交付顺序、每阶段的产出与验收、依赖与风险，以及最终演示脚本。

---

## 1. 排期原则

每个阶段都必须产出**可独立运行、可演示**的成果，且后一阶段依赖前一阶段的真实数据。

反面做法（明确避免）：先做聊天界面，再补数据——会导致 Agent 只能空谈。

## 2. 阶段一：请求标识链路 + 行为埋点

| 项 | 内容 |
|----|------|
| 目标 | 让「一次请求」和「一次观看」变成可追踪、可统计的数据 |
| 交付 | `RequestIDMiddleware`；gRPC 透传拦截器；`FeedSource` 枚举与打标；inbox 回源重建；`feed:trace:{request_id}`；`feed-behavior-event` 契约与上报接口；Behavior Worker（幂等 + 明细 + 小时指标） |
| 涉及 | `app/gateway`、`common/{requestid,interceptors,event/behavior}`、`app/feed/rpc`、`app/interaction/{rpc,worker}`、`deploy/sql/interaction.sql` |
| 验收 | A8（request_id 可检索）、A9（重复消费不重复累加）、E2E-4/E2E-7/E2E-9 |
| 演示 | 刷一次流 → 拿 `request_id` 查 Trace → 看到各数据源返回量；上报行为 → 指标增长 |
| 风险 | 拦截器接入点漏改→ 用 `request_id_missing_total` 指标兜底发现 |

## 3. 阶段二：Content Service

| 项 | 内容 |
|----|------|
| 目标 | 视频发布后自动产出内容画像 |
| 交付 | `app/content/rpc`（9007）+ `app/content/worker`；FFmpeg 音频/抽帧；ASR/OCR/多模态接入；`feed_content_profiles`；幂等与重试；`feed-deleted` 下线 |
| 涉及 | `api/proto/content/content.proto`、`deploy/sql/content.sql`、外部模型服务接入层 |
| 验收 | A1、A2、A3、E2E-1/2/3 |
| 演示 | 发一条标题模糊的视频 → 后台自动生成「西安周边露营攻略」类标签与摘要 |
| 风险 | 外部服务成本与限流→ 并发上限 + 关键帧上限 + 输入截断；FFmpeg 安全 → 见 [13](./13-security.md) §5 |

## 4. 阶段三：内容检索 + 兴趣画像

| 项 | 内容 |
|----|------|
| 目标 | 内容可被语义检索，用户兴趣可量化 |
| 交付 | ES 索引与三路召回 + RRF 融合 + 业务过滤/排序；`SearchContent`；兴趣权重与时间衰减；`user:interest:{uid}` + `user_interest_profiles`；`GetUserInterestProfile` |
| 涉及 | `app/content/rpc`、`app/interaction/worker`、评测集与 `scripts/eval-search.sh` |
| 验收 | A4、A11、E2E-8 |
| 演示 | 直接调 HTTP 检索接口，用「西安周边新手露营」命中阶段二生成的画像 |
| 风险 | 评测集质量决定准确率可信度 → 标注需两人交叉复核 |

## 5. 阶段四：Eino Agent

| 项 | 内容 |
|----|------|
| 目标 | 自然语言入口打通三类场景 |
| 交付 | `app/agent/rpc`（9006）；Eino 单Agent + 8 个只读 Tool；Run 状态机与限额；输出校验器；推荐原因规则引擎；创作者漏斗诊断；Gateway Agent 路由 |
| 涉及 | `api/proto/agent/agent.proto`、`deploy/sql/agent.sql`、`app/gateway`、`common/errorx` |
| 验收 | A5、A6、A7、A10、A12、E2E-5/6/10 |
| 演示 | 会话中问「找露营视频」「为什么推荐这条」「分析我这条视频为什么播放差」 |
| 风险 | 模型不稳定 → 输出校验 + 模板降级；成本 → Run 限额与 token 统计 |

## 6. 阶段五：可观测性与诊断

| 项 | 内容 |
|----|------|
| 目标 | 系统可度量、问题可定位 |
| 交付 | 各服务 `Prometheus` / `Telemetry` 配置；本项目全部自定义指标；日志字段统一；MQ trace 传播；告警规则；内部 Feed 请求诊断接口/页面 |
| 涉及 | 全部服务 `etc/*.yaml`、`common/mq`、Grafana 面板 |
| 验收 | [12-observability.md](./12-observability.md) §7 全部用例 |
| 演示 | 用 `request_id` 串起Gateway → Feed RPC → Redis → 聚合层 → 下游 RPC 全链路 |
| 风险 | 指标 label 基数爆炸 → 禁止高基数 label（已列为红线） |

## 7. 依赖关系

```text
阶段一（request_id + 埋点）
   ├──▶ 阶段二（Content）────┐
   │                        ├──▶ 阶段三（检索 + 兴趣）──▶ 阶段四（Agent）──▶ 阶段五（可观测）
   └────────────────────────┘
说明：
  阶段三的兴趣画像同时依赖阶段一的行为事件与阶段二的内容标签；
  阶段四的创作者分析依赖阶段一的指标与阶段二的画像；
  阶段四的推荐解释依赖阶段一的来源标记与阶段三的兴趣画像。
```

## 8. 主要风险与对策

| 风险 | 影响 | 对策 |
|------|------|------|
| 外部模型/ASR/OCR 成本超预算 | 无法长期运行 | 关键帧 ≤ 20、字幕截断、并发上限、只分析视频类、失败不无限重试 |
| 客户端埋点不可控（本项目以后端为主） | 数据质量差 | 服务端重判阈值 + 数据质量校验（[14](./14-acceptance-test.md) §5） |
| 模型幻觉 | 回答不可信 | Go 计算 + 输出校验器 + 模板降级 |
| Trace 数据量 | Redis 内存压力 | 采样 +短 TTL + 只存必要字段 |
| 多服务改造铺开过大 | 交付风险 | 严格按阶段推进，每阶段可独立演示与回滚 |

## 9. 最终演示脚本

| 序号 | 演示 | 操作 | 观察点 |
|------|------|------|--------|
| D1 | 内容自动理解 | 发布一条标题模糊的露营视频 | 数十秒后 `GET /api/v1/feeds/{id}/content-profile` 返回类别「户外旅行」、标签「露营/西安周边」、字幕与 OCR |
| D2 | 自然语言检索 | 会话中输入「找一些西安周边适合新手露营的视频」 | 返回真实 Feed 卡片 + 匹配原因；断言 feed 全部存在 |
| D3 | 推荐解释 | 刷流后点某条内容「为什么推荐」 | 返回 `source=RECOMMEND_POOL` + `INTEREST_TOPIC_MATCH` + 命中标签证据 |
| D4 | 创作者分析 | 输入「分析我这条视频为什么播放效果不好」 | 回答含曝光/播放率/快速划走率/完播率与同类中位数对比，结论定位到「前三秒」 |
| D5 | 链路诊断 | 用 D3 的 `request_id` 调内部 Trace 接口 | 展示 inbox/outbox/推荐池/同城池读取量、合并量、返回量与耗时 |

演示同时体现的后端能力：推拉结合 Feed、RocketMQ 异步解耦与幂等消费、多模态内容理解、行为采集、兴趣画像、语义检索、Agent Tool Calling、分布式链路追踪、微服务权限治理。

## 10. 演进与 TODO

- 阶段六（可选）：受控写能力 + 审批流（内容画像纠错落地）。
- 阶段七（可选）：兴趣画像入在线召回，做 A/B 效果验证。
- 阶段八（可选）：行为明细迁移分析型存储，支持任意维度诊断。

---

## 关联文档

- [总览与定位](./00-overview.md)
- [架构与服务拆分](./01-architecture.md)
- [请求标识与 Feed 链路追踪](./02-request-trace.md)
- [内容分析服务设计](./04-content-analysis.md)
- [Agent 服务设计](./09-agent-service.md)
- [验收标准与测试策略](./14-acceptance-test.md)
- [可观测性](./12-observability.md)
