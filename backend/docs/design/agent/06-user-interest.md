# 用户兴趣画像

> 定义兴趣画像的数据来源、行为权重、时间衰减、Redis/MySQL 存储结构与对外查询口径。属实施阶段三。

---

## 1. 概述与定位

兴趣画像用于两件事：**推荐原因解释**（这条内容为什么符合你的兴趣）与**兴趣摘要查询**（我最近在看什么）。第一版不参与在线召回排序，避免影响现有 Timeline。

- 维度：内容标签（`topics`）与内容类别（`category`）。
- 算法：规则权重 + 时间衰减，不训练模型。
- 更新：Behavior Worker 异步更新，延迟秒级到分钟级即可。
- 归属：Interaction 服务域的 profile 子模块（见 [01](./01-architecture.md) 决策 D1）。

## 2. 数据来源与权重

行为 → 兴趣分数增量：

| 行为 | 权重 | 来源 Topic |
|------|:---:|-----------|
| 完成播放 `FINISH` | +5 | `feed-behavior-event` |
| 收藏 `COLLECT` | +4 | `interaction-event` |
| 点赞 `LIKE` | +3 | `interaction-event` |
| 评论 `CREATE` | +3 | `comment-event` |
| 分享 `SHARE` | +3 | `feed-behavior-event` |
| 有效播放 `EFFECTIVE_PLAY` | +2 | `feed-behavior-event` |
| 普通曝光 `EXPOSE` | 0（不入画像） | `feed-behavior-event` |
| 快速划走 `SKIP` | -2 | `feed-behavior-event` |
| 取消收藏 `UNCOLLECT` | -3 | `interaction-event` |
| 取消点赞 `UNLIKE` | -2 | `interaction-event` |

更新流程：

```text
行为事件（含 feed_id）
  → 取内容画像标签：先读 Redis content:profile:{feed_id}（TTL 1h），未命中调 Content RPC.BatchGetContentProfile
  → 画像不存在或未完成 → 本次跳过（不猜测标签）
  → 对category 与 topics（最多取前 5 个）执行 ZINCRBY user:interest:{user_id} delta member
  → 标签维度与类别维度用前缀区分：t:{topic} / c:{category}
  → 裁剪：ZREMRANGEBYRANK 保留 Top 200；清理 score ≤ 0.1 的成员
```

设计说明：

- **同一 feed 的重复行为不叠加**：`interest:dedup:{user_id}:{feed_id}:{action}`（SETNX，TTL 24h）；否则反复播放同一视频会让画像失真。
- 负向行为不会把分数打到负无穷：`ZINCRBY` 后若 score < 0 则置0（Lua 脚本保证原子）。
- `EXPOSE` 权重为 0，但仍用于统计「看过没兴趣」的曝光基数，供后续演进使用。

## 3. 时间衰减

避免早期行为长期主导，采用**半衰期 14 天**的指数衰减：

```text
daily_factor = 0.5 ^ (1/14) ≈ 0.9517
```

实现方式（选定「定时批量衰减」）：

| 方案 | 说明 | 是否采用 |
|------|------|:---:|
| 定时批量衰减 | 每日 04:00 对活跃用户集合 `interest:active`（当日有行为的用户）的 ZSet 全成员乘以 `daily_factor`（Lua 脚本，单用户一次原子执行） | ✅ |
| 懒衰减 | 读时按 `last_update` 现算 | ❌ 需额外存每成员时间戳，复杂度高于收益 |

- 活跃集合：Behavior Worker 写画像时 `SADD interest:active:{yyyyMMdd} user_id`（TTL 7 天）；衰减任务只处理最近 7 天活跃用户，长期不活跃用户在下次行为前不衰减（读取时若`updated_at` 过旧，则在返回前做一次补偿衰减）。
- 衰减后 score< 0.1 的成员直接移除，控制 ZSet 大小。

## 4. 存储结构

|存储 | Key / 表 | 结构 | 内容 | TTL |
|------|----------|------|------|-----|
| Redis | `user:interest:{user_id}` | ZSet | member = `t:露营` / `c:户外旅行`，score = 当前权重 | 90 天（每次更新刷新） |
| Redis | `interest:active:{yyyyMMdd}` | Set | 当日有行为的用户 | 7 天 |
| Redis | `interest:dedup:{user_id}:{feed_id}:{action}` | String | 同 feed 同行为去重 | 24h |
| Redis | `content:profile:{feed_id}` | String(JSON) | 画像标签缓存（供画像更新与解释复用） | 1h |
| MySQL | `user_interest_profiles` | 行 | `interest_json` 快照 + `version` + `calculated_at` | 长期 |

MySQL 快照职责：

- Redis 故障或数据丢失后的恢复基准。
- 离线分析与效果回溯。
- 写入策略：每日全量快照活跃用户（批量 UPSERT），或用户兴趣变化累计超过阈值时触发；`version` 单调递增，便于并发写入时识别新旧。

`interest_json` 结构：

```json
{
  "categories": [{ "name": "户外旅行", "score": 18.4 }, { "name": "美食", "score": 6.1 }],
  "topics": [{ "name": "露营", "score": 22.7 }, { "name": "西安周边", "score": 9.3 }],
  "total_actions": 156,
  "window_days": 30
}
```

## 5. 对外查询口径

`GetUserInterestProfile(user_id, top_n)`：

| 规则 | 说明 |
|------|------|
| 权限 | **只能查本人**；`ctx` 中的身份与 `user_id` 不一致直接 `errorx.Forbidden`（内部用户例外） |
| 返回内容 | Top N（默认 10）标签与类别 + **归一化后的相对占比**，不返回原始权重与计算过程 |
| 归一化 | `ratio = score / Σscore`，保留 1 位小数百分比 |
| 冷启动 | 有效行为不足（`total_actions < 10`）返回 `is_cold_start=true` + 空列表，Agent 需如实说明「行为数据不足」 |
| 时效 | 附`calculated_at`，Agent 回答中必须体现数据时间口径 |

返回示例：

```json
{
  "user_id": 10086,
  "is_cold_start": false,
  "top_topics": [{ "name": "露营", "ratio": 0.31 }, { "name": "西安周边", "ratio": 0.13 }],
  "top_categories": [{ "name": "户外旅行", "ratio": 0.52 }],
  "total_actions": 156,
  "calculated_at": 1785302400000
}
```

不返回：单条行为明细、内部权重、衰减因子、去重键。理由：既满足解释需求，又避免暴露可被反推的推荐内部机制。

## 6. 隐私与合规

- 画像只保存标签与分数，不保存「用户看过哪条视频」的可读列表（明细在 `feed_behavior_events`，受内部权限保护）。
- 提供用户主动重置入口（预留接口，第一版仅内部可调用）：删除 ZSet + 快照。
- 兴趣标签在推荐解释中使用时，只允许出现在**本人**的解释里；跨用户使用一律禁止。

## 7. 测试策略

| 层次 | 用例 |
|------|------|
| 单元 | 权重计算：FINISH+5、SKIP-2、负向后 score 不小于 0 |
| 单元 | 同 feed 同行为重复上报 → 只计一次 |
| 单元 | 衰减因子 14 天半衰期：初始 10分，14 天后≈5 分 |
| 单元 | Top 200 裁剪与低分清理 |
| 集成 | 画像未完成的 feed 行为 → 不更新兴趣、不报错 |
| 集成 | 查询他人兴趣 → `Forbidden` |
| 集成 | Redis 清空后由 MySQL 快照恢复，Top 列表基本一致 |
| 集成 | 冷启动用户返回 `is_cold_start=true` |

## 8. 演进与 TODO

- 引入短期兴趣（近 1 小时会话内）与长期兴趣双通道。
- 负反馈细化（「不感兴趣」显式按钮）。
- 画像入在线召回（需先做 A/B 与效果评估）。
- 从规则权重演进为离线特征 + 模型打分。

---

## 关联文档

- [行为事件采集与指标聚合](./03-behavior-event.md)
- [内容分析服务设计](./04-content-analysis.md)
- [推荐原因解释](./07-recommend-reason.md)
- [数据模型](./10-data-model.md)
- [安全要求](./13-security.md)
