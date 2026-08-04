# Agent 工具契约

> 定义 Agent 可调用的全部工具：名称、描述、入参/出参 Schema、到现有 gRPC RPC 的映射，以及防幻觉（grounding）与调用控制约束。工具是 Agent 与业务世界的唯一接口。

---

## 1. 概述与定位

- 每个工具是 `internal/tools/` 下的一个 Eino `InvokableTool`，内部封装一次（或一组）zrpc 调用。
- 工具**只返回结构化 JSON**，不返回自然语言；空结果返回空数组而非报错，让模型显式感知「没有数据」。
- 所有 RPC 映射均以 `api/proto/**/*.proto` 现有定义为准；后端缺口对应的「未来工具」单列于 §5，未实现前不注册进 Graph。

## 2. 架构与职责

### 2.1 设计规则

- **必须**：工具描述（description）写清「返回什么、不返回什么」，例如 `getUserInteractionHistory` 需注明「系统当前没有播放/观看记录，本工具返回的是点赞与收藏历史」。
- **必须**：入参用 JSON Schema 声明类型、范围与默认值；工具内部再做一次服务端校验（不信任模型产出的参数）。
- **必须**：`user_id` 一律取自会话上下文（JWT 解析结果），**不允许**模型在参数中指定他人 `user_id`（越权防护）；运营类工具需校验会话角色为 operator。
- **禁止**：工具内部拼接自然语言、内部调用 LLM。
- **建议**：批量接口单次 ≤ 100 个 ID（与下游 proto 注释一致），工具层自动分批。

## 3. 数据模型（工具输出通用结构）

```json
{
  "ok": true,
  "data": { },
  "error": "",            // ok=false 时的结构化错误说明
  "source": "interaction.GetUserLikedFeeds",  // 数据来源 RPC，用于 grounding 追溯
  "fetched_at": 1753689600000
}
```

`source` 与 `fetched_at` 随结果进入事实上下文，供 [07-observability.md](./07-observability.md) 的 grounding 校验使用。

## 4. 接口与契约（工具清单）

### 4.1 读工具（V1 即注册）

| 工具名 | 映射 RPC | 入参（Schema 摘要） | 出参（data 摘要） |
|---|---|---|---|
| `search_candidate_videos` | `feed.GetRecommendTimeline` | `page`(int,≥1), `page_size`(int,1-50,默认20) | `feeds[]`: {feed_id, author_id, feed_type, title, cover_url, like_count, comment_count, created_at} |
| `get_city_videos` | `feed.GetCityTimeline` | `city_code`(string,可空), `page`, `page_size` | 同上 |
| `get_video_detail` | `feed.GetFeed` / `BatchGetFeeds` | `feed_ids`(int64[],1-100) | `videos{}`: feed_id → FeedInfo（含 description/status/city/媒体） |
| `get_user_interaction_history` | `interaction.GetUserLikedFeeds` + `GetUserCollectedFeeds` | `kind`(enum: liked/collected/both), `limit`(int,≤200) | `liked_feed_ids[]`, `collected_feed_ids[]`, `totals` |
| `get_video_stats` | `interaction.BatchGetFeedStats` | `feed_ids`(int64[],1-100) | `stats[]`: {feed_id, like_count, collect_count} |
| `get_interaction_status` | `interaction.BatchGetUserInteractionStatus` | `feed_ids`(int64[],1-100) | `status[]`: {feed_id, is_liked, is_collected} |
| `get_user_follows` | `relation.GetFollows` | `page`, `page_size`(默认20) | `followee_ids[]`, `total` |
| `get_author_info` | `user.BatchGetUsers` / `GetUser` | `user_ids`(int64[],1-100) | `users[]`: {id, nickname, avatar} |
| `get_author_videos` | `feed.GetUserFeeds` | `author_id`, `page`, `page_size` | `feeds[]`（FeedBrief） |
| `get_hot_comments` | `comment.GetHotComments` | `feed_id`, `limit`(≤10) | `comments[]`: {comment_id, content, like_count} |
| `get_comment_counts` | `comment.BatchGetCommentCount` | `feed_ids`(int64[],1-100) | `counts{}`: feed_id → count |

关键描述约定（防幻觉，写进 description）：

- `get_user_interaction_history`：「系统**没有**播放/观看记录；`liked+collected` 是『用户接触过』的最接近近似。」
- `get_video_stats`：「仅有点赞数与收藏数；**没有**播放量、点击率、完播率。」
- `search_candidate_videos`：「`FeedBrief` **不含**分类、标签、时长字段，无法按题材/时长精确过滤。」

### 4.2 写工具（V3 注册，全部要求经审批节点）

| 工具名 | 映射 RPC | 状态 | 说明 |
|---|---|---|---|
| `delete_video` | `feed.DeleteFeed` | ✅ 现有 | 软删除；仅 operator 角色 |
| `like_video` / `collect_video` | `interaction.LikeFeed` / `CollectFeed` | ✅ 现有 | 仅代当前用户操作，演示写链路用 |
| `update_video_meta` | `feed.UpdateFeed` | ❌ 待后端 | 修改标题/标签/分类，见 [06-backend-gaps.md](./06-backend-gaps.md) §3 |
| `set_video_shelf_status` | `feed.UpdateFeed`（status 扩展） | ❌ 待后端 | 上/下架 |
| `adjust_recommend_slot` | 无对应模型 | ❌ 待后端 | 推荐位调整，需先有推荐位数据模型 |

写工具统一约束：**必须**携带 `plan_item_id`（对应审批计划条目）才允许执行；无审批上下文时直接拒绝（60003）。

### 4.3 未来工具（依赖 stats 服务，V2）

| 工具名 | 依赖 | 出参 |
|---|---|---|
| `get_play_metrics` | stats 服务（[06-backend-gaps.md](./06-backend-gaps.md) §4） | play_count / avg_watch_duration / completion_rate / ctr |
| `get_user_watch_history` | stats 服务 play_records | 真实观看历史（替换近似方案） |

## 5. 错误码

工具层错误统一映射：下游 gRPC 错误 → `ok=false` + `error` 字段（含 `common/errorx` 码），**不向模型抛异常文本堆栈**；连续失败按 [01-architecture.md](./01-architecture.md) §2.3 降级。

## 6. 缓存与一致性

- 只读工具结果在单次 run 内做内存级 memo（相同工具+相同参数不重复调用），跨 run 不缓存（保证数据新鲜）。
- 写工具不做任何缓存，执行前后各留痕一次（见 [07-observability.md](./07-observability.md)）。

## 7. 测试策略

- 每个工具单测：参数校验（越界/越权 user_id）、分批逻辑、错误映射。
- Schema 快照测试：工具 JSON Schema 变更必须显式更新快照，防止悄悄破坏模型侧契约。

## 8. 演进与 TODO

- [ ] 后端补齐 `UpdateFeed` 后启用 `update_video_meta`，同步更新本表状态列。
- [ ] stats 服务上线后注册 §4.3 工具，并**删除** `get_user_interaction_history` 描述中的「近似」措辞。

## 关联文档

- [Agent 服务总览](./00-overview.md)
- [架构与编排设计](./01-architecture.md)
- [审批流程设计](./04-approval.md)
- [后端缺口清单](./06-backend-gaps.md)
- [可观测性设计](./07-observability.md)
