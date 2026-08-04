# Agent 场景分版设计

> 按 V1（个性化推荐）→ V2（运营诊断）→ V3（执行与审批）三个版本逐步定义 Agent 能力，每个场景明确：可用数据、工具编排、输出契约、防幻觉 Prompt 约束，以及当前后端可行性。

---

## 1. 概述与定位

版本推进原则：**每一版都必须端到端可跑、数据全部真实**。后端缺数据的能力宁可显式声明「暂不支持」，也不允许模型编造（缺口清单见 [06-backend-gaps.md](./06-backend-gaps.md)）。

| 版本 | 场景 | 后端可行性 |
|---|---|---|
| V1-lite | 推荐未互动过的近期视频 | ✅ 现状可做 |
| V1-full | 按题材/时长/未观看精确过滤 | ⚠️ 需 `feeds` 补列 |
| V2 | 播放/点击/完播诊断 | ❌ 需 stats 服务 |
| V3 | 修改元数据（审批闭环） | ⚠️ 审批可做，执行需 `UpdateFeed` |

## 2. 架构与职责（各版场景设计）

### 2.1 V1-lite：个性化推荐（现状可交付）

**示例输入**：「给我推荐几部最近没看过的视频。」

工具编排（典型路径）：

1. `get_user_interaction_history(kind=both)` → 已互动 feed_id 集合（「看过」的近似）。
2. `search_candidate_videos`（分页拉取，最多 3 页）→ 候选池。
3. `get_interaction_status`（候选批量）→ 二次确认过滤。
4. `get_user_follows` + `get_author_videos`（可选）→ 关注作者的近期发布加权。
5. `get_video_stats` + `get_comment_counts` → 热度信号。
6. `propose` 生成 Top-N 推荐 + 理由。

**输出契约**（写入 `agent_runs.result` 与 `recommendation_records`）：

```json
{
  "type": "recommendation",
  "items": [
    {
      "feed_id": 1024,
      "title": "……",
      "reason": "你关注的作者 @xx 3 天前发布；收藏数 152 高于候选池均值；你未点赞/收藏过",
      "evidence": ["relation.GetFollows", "interaction.BatchGetFeedStats"]
    }
  ],
  "disclaimer": "系统暂无观看记录，『没看过』按未点赞且未收藏近似判断"
}
```

**Prompt 约束（节选，propose 节点 System Prompt）**：

- 只能推荐出现在工具结果中的 `feed_id`；理由中的每个数字必须能在事实上下文中找到。
- 用户条件涉及题材/时长时，必须回答「当前视频库没有分类/时长字段，无法按此条件精确过滤」，可按标题关键词做**弱匹配**并显式标注为弱匹配。
- `evidence` 字段必须列出理由依赖的工具 `source`。

### 2.2 V1-full：条件化推荐（依赖后端补列）

`feeds` 补 `category`/`duration_sec`/`tags` 后（[06-backend-gaps.md](./06-backend-gaps.md) §2）：

- `intent` 节点将「悬疑」「不要太长」解析为 `{"category":"悬疑","max_duration_sec":2400}`。
- `get_video_detail` 返回含新字段，过滤由 `aggregate` 节点**代码执行**（不靠模型心算），过滤明细进入事实上下文。
- 移除 V1-lite 的题材/时长 disclaimer。

### 2.3 V2：运营诊断（依赖 stats 服务）

**示例输入**：「找出最近七天播放量低于平均值且完播率下降的视频。」

前置：stats 服务提供 `get_play_metrics`（[06-backend-gaps.md](./06-backend-gaps.md) §4）。编排：

1. `get_play_metrics(window=7d, scope=all)` → 播放量/完播率/CTR。
2. `aggregate` **代码计算**均值、环比、筛选命中集合（模型不做算术）。
3. `get_video_detail` + `get_video_stats` + `get_hot_comments` → 补充上下文。
4. `propose` 输出诊断 JSON：

```json
{
  "type": "diagnosis",
  "window": "2026-07-21 ~ 2026-07-27",
  "items": [{
    "video_id": 1024,
    "metrics": {"play_count": 3200, "avg_play": 5100, "completion_rate_wow": -0.18},
    "problems": ["播放量低于全库均值 37%", "完播率环比下降 18%", "收藏率正常"],
    "suggestions": ["更换封面与标题（当前 CTR 1.2% < 均值 2.8%）", "减少首页推荐曝光", "保留搜索流量入口"]
  }]
}
```

**硬约束**：`metrics` 内数值必须由 `aggregate` 代码回填（非模型生成）；模型只产出 `problems/suggestions` 文本，且引用的数字必须与 `metrics` 一致（grounding 校验见 [07-observability.md](./07-observability.md)）。stats 未上线前，该意图直接返回「暂不支持：系统尚无播放统计数据」。

### 2.4 V3：执行与审批（依赖 `UpdateFeed`）

**示例输入**：「把低点击率视频的标题全部优化一下。」

编排：诊断（同 V2 或按现有点赞/收藏率近似）→ `propose` 生成 ApprovalPlan（每条含 before/after/reason）→ `Interrupt` 暂停 →「计划修改 8 个视频标题，是否批准执行？」→ approve → `execute`（`update_video_meta` 逐条）→ `verify` 回读比对 → 执行报告。完整机制见 [04-approval.md](./04-approval.md)。

现状可先交付的 V3 子集：用 `delete_video`（已有 RPC）演练「下架违规/低质内容」审批闭环，验证全链路后再等 `UpdateFeed`。

## 3. 数据模型

各场景结果统一写 `agent_runs.result`（type 区分）；推荐场景另写 `recommendation_records`。见 [03-state-session.md](./03-state-session.md)。

## 4. 接口与契约

三类意图共用同一组 HTTP API（发起 run / 查询 / 审批），见 [08-api.md](./08-api.md)。

## 5. 错误码

意图超出当前版本能力 → 正常返回 `succeeded`，result 中 `type=unsupported` + 说明缺失的数据基础（不算失败，便于统计需求热度）。

## 6. 缓存与一致性

推荐结果不缓存复用（个性化 + 新鲜度要求）；诊断类 run 结果保留在 `agent_runs.result` 中供回看，不做二次分发。

## 7. 测试策略

- 每场景准备**固定工具回放数据集**（录制的 `agent_tool_calls`），断言输出契约字段完整、`feed_id` 全部来自工具结果、数字全部可溯源。
- 对抗测试：构造诱导性输入（「就当播放量是 10 万」），断言 Agent 拒绝采用非工具数据。

## 8. 演进与 TODO

- [ ] V1-full 上线后将 `intent` 的条件 Schema 扩展为受控枚举（category 值域来自 DB 字典）。
- [ ] V2 上线后补「推荐位调整方案」场景（依赖推荐位数据模型，见缺口清单）。

## 关联文档

- [Agent 服务总览](./00-overview.md)
- [工具契约](./02-tools.md)
- [审批流程设计](./04-approval.md)
- [后端缺口清单](./06-backend-gaps.md)
- [可观测性设计](./07-observability.md)
