# FeedMind Agent 设计文档目录

> 本目录存放「短视频内容理解与 Feed 智能助手」（FeedMind Agent）的设计方案：内容多模态理解、行为采集与指标聚合、用户兴趣画像、语义检索、推荐原因解释、创作者作品分析与 Agent 编排。

---

## 文档索引

| 文件 | 主题 | 一句话说明 |
|------|------|-----------|
| [00-overview.md](./00-overview.md) | 总览与定位 | 项目定位、现状差距、第一版范围、角色权限、标识体系 |
| [01-architecture.md](./01-architecture.md) | 架构与服务拆分 | 新增服务/进程、端口、关键链路、配置、改造点、决策记录 |
| [02-request-trace.md](./02-request-trace.md) |请求标识与链路追踪 | `request_id` 贯通、Feed 来源标记、请求级 Trace |
| [03-behavior-event.md](./03-behavior-event.md) | 行为采集与指标聚合 |埋点契约、上报接口、幂等消费、小时级指标 |
| [04-content-analysis.md](./04-content-analysis.md) | 内容分析服务 | FFmpeg/ASR/OCR/多模态流水线、状态机、幂等与重试 |
| [05-content-search.md](./05-content-search.md) | 自然语言内容检索 | 索引结构、三路召回与 RRF、业务过滤排序、评测 |
| [06-user-interest.md](./06-user-interest.md) | 用户兴趣画像 | 行为权重、时间衰减、Redis/MySQL 存储、查询口径 |
| [07-recommend-reason.md](./07-recommend-reason.md) | 推荐原因解释 | 来源枚举 → `reason_codes` → 文案，降级策略 |
| [08-creator-metrics.md](./08-creator-metrics.md) | 创作者作品分析 | 指标口径与公式、同类匿名对比、漏斗诊断与建议边界 |
| [09-agent-service.md](./09-agent-service.md) | Agent服务设计 | Eino 编排、Run 状态机、8 个只读 Tool、限额与注入防护 |
| [10-data-model.md](./10-data-model.md) | 数据模型 | 新增 MySQL 表、Redis Key、ES 索引、容量与清理 |
| [11-api.md](./11-api.md) | 接口契约 | Gateway HTTP、各服务新增 RPC、Metadata、错误码段 |
| [12-observability.md](./12-observability.md) | 可观测性 | Prometheus 指标、日志字段、OTel 链路、告警 |
| [13-security.md](./13-security.md) | 安全要求 | 权限模型、越权与注入防护、SSRF/RCE、脱敏与审计 |
| [14-acceptance-test.md](./14-acceptance-test.md) | 验收与测试 | 测试分层、端到端用例、验收标准映射、故障注入 |
| [15-roadmap.md](./15-roadmap.md) | 实施路线 | 五阶段交付顺序、依赖、风险、演示脚本 |

## 阅读顺序

```
00-overview → 01-architecture
                    ↓
        02-request-trace → 03-behavior-event            （阶段一：数据基础）
                    ↓
              04-content-analysis                （阶段二：内容理解）
                    ↓
        05-content-search → 06-user-interest（阶段三：检索与画像）
                    ↓
   07-recommend-reason → 08-creator-metrics → 09-agent-service   （阶段四：Agent）
                    ↓
        10-data-model / 11-api（随时查阅的契约）
                    ↓
   12-observability → 13-security → 14-acceptance-test → 15-roadmap
```

## 与需求文档的关系

- 源需求：[短视频内容理解与 Feed 智能助手需求文档](../../agent需求文档.md)（V1.0，FeedMind Agent）。
- 本目录在需求基础上补充可落地细节（口径、状态机、幂等、权限、限额、DDL、错误码）。
- 与需求文档不一致之处（HTTP 前缀、行为上报路径、指标与画像的服务归属、端口分配等）集中记录在 [01-architecture.md](./01-architecture.md) §7 决策记录，不在其它文档分散解释。

## 实现状态

| 模块 | 状态 | 说明 |
|------|------|------|
| 请求标识链路 | 设计完成，未实现 | 当前 `common/response` 的 `request_id` 恒为空（无人写入） |
| Feed 来源与请求 Trace | 设计完成，未实现 | `FeedBrief`尚无 `source`；无 inbox 回源重建 |
| 行为埋点与指标 | 设计完成，未实现 | 需新增 `feed-behavior-event` 与三张表 |
| Content 服务 | 设计完成，未实现 | 需新增 `app/content/{rpc,worker}` |
| 内容检索 | 设计完成，未实现 | 需引入 ES（或 Redis Stack） |
| 兴趣画像 | 设计完成，未实现 | Interaction 服务域内实现 |
| Agent 服务 | 设计完成，未实现 | 需新增 `app/agent/rpc` 与 Eino 依赖 |
| 可观测性 | 设计完成，未实现 | 各服务 `etc/*.yaml`尚无 `Prometheus`/`Telemetry` |

## 关联文档

- [design 目录索引](../README.md)
- [系统架构](../architecture.md)
- [服务拆分](../service-design.md)
- [全局数据模型](../data-model.md)
- [REST API 设计规范](../api-spec/README.md)
- [Feed 服务设计](../feed/README.md)
- [Interaction 服务设计](../interaction/README.md)
- [文档编写规范](../../agent/doc-writing-guide.md)
