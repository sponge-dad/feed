# 推荐原因解释

> 定义 Feed 来源枚举到 `reason_codes` 的映射规则、解释生成流程、降级策略与对外契约。属实施阶段四。

---

## 1. 概述与定位

用户点击「为什么推荐」或在会话中询问某条内容的展示原因时，系统必须给出**基于结构化事实**的解释。

红线：解释文本只能由 `reason_codes` + 后端提供的字段渲染，**禁止模型自行推测**推荐机制。模型的作用仅是把多个 reason 组织成一句通顺的话。

数据依赖：

| 依赖 | 来源 | 缺失时 |
|------|------|--------|
| Feed 来源 | `feed:trace:{request_id}` 的 `f:{feed_id}`（见 [02](./02-request-trace.md)） | 走降级解释 |
| 关注关系 | Relation RPC `IsFollowing` | reason 不生成 |
| 同城信息 | Feed 的 `city_code` + 用户当前城市 | reason 不生成 |
| 兴趣匹配 | 用户兴趣画像 ∩ 内容画像标签（见 [06](./06-user-interest.md)、[04](./04-content-analysis.md)） | reason 不生成 |

## 2. 来源枚举

| Source | 触发路径 | 用户可读语义 |
|--------|----------|--------------|
| `FOLLOW_INBOX` | 关注作者发布后由 Feed Worker 推入 `inbox:{userID}` | 你关注的作者发布了新内容 |
| `VIP_OUTBOX` | 读取时从关注的大 V `outbox:{authorID}` 拉取 | 你关注的（活跃/大 V）作者的作品 |
| `RECOMMEND_POOL` | 命中公共推荐池 `feed:recommend` | 系统推荐 |
| `CITY_POOL` | 命中同城池 `feed:city:{cityCode}` | 与你所在城市相关 |
| `INBOX_REBUILD` | 收件箱缺失后回源重建（阶段一补齐） | 你关注的作者的历史内容（补齐后展示） |

## 3. reason_codes 字典

| Code | 触发条件（全部由后端判定） | 需要的数据 | 文案模板 |
|------|---------------------------|-----------|----------|
| `FOLLOW_AUTHOR` | source∈ {FOLLOW_INBOX, VIP_OUTBOX, INBOX_REBUILD} 且 `IsFollowing(user, author)` 为真 | 作者昵称、发布时间 | 你关注了 {author}，这是 TA 于 {published_at} 发布的作品 |
| `FOLLOW_AUTHOR_UNFOLLOWED` | source 为关注类但当前已取关（inbox 残留） | 作者昵称 | 这条内容来自你曾关注的作者 {author} |
| `SAME_CITY` | source = CITY_POOL，或 feed.city_code == 用户城市 | 城市名 | 这条内容发布于{city_name}，与你当前所在城市一致 |
| `INTEREST_TOPIC_MATCH` | source = RECOMMEND_POOL 且 用户 Top 兴趣标签 ∩ 内容 topics ≠ ∅ | 命中标签（≤ 3）、行为依据 | 你最近较多完整观看/收藏「{topics}」相关内容，这条视频同属该主题 |
| `INTEREST_CATEGORY_MATCH` | 同上，但只命中类别 | 类别名 | 你近期偏好「{category}」类内容 |
| `POPULAR_IN_POOL` | source = RECOMMEND_POOL 且无兴趣命中，且内容互动量位于同类前列 | 互动量分位 | 这条内容近期在同类中互动较多 |
| `FRESH_CONTENT` | 发布时间在 24h 内且无其他 reason | 发布时间 | 这是近 24 小时内发布的新内容 |
| `REBUILD_BACKFILL` | source = INBOX_REBUILD | - | 你的关注流数据在重建后补齐了这条内容 |
| `NO_TRACE` | 未找到该请求的来源记录 | - | 暂时无法定位这条内容的具体来源（展示记录已过期） |

生成规则：

- 一条内容可命中多个 reason，按优先级取前 3：`FOLLOW_AUTHOR` > `INTEREST_TOPIC_MATCH` > `SAME_CITY` > `INTEREST_CATEGORY_MATCH` > `POPULAR_IN_POOL` > `FRESH_CONTENT` >其余。
- 每个 reason 必须携带 `evidence`（结构化证据字段），前端可直接渲染，不依赖模型。
-兴趣类 reason 的 `evidence` 只包含标签名与「行为类型计数」的粗粒度描述（如 `finish_count>=3`），不暴露具体看过哪些视频。

## 4. 生成流程

```text
GET /api/v1/feeds/{feedId}/recommendation-reason?request_id=xxx
  1. 身份：user_id 取自 JWT
  2. 归属校验：trace.user_id == user_id（否则 Forbidden，防止探测他人 Feed）
  3. 取 source：feed:trace:{request_id} 的 f:{feed_id}
  4. 并行取：Feed 详情、IsFollowing、用户城市、兴趣画像、内容画像
  5. 规则引擎按 §3 生成 reason_codes + evidence（纯 Go 代码）
  6. 渲染：默认用模板文案直接返回（不调模型）
  7. Agent 会话场景：把 reason_codes + evidence 交给模型组织成自然语言
```

模型在第 7 步的约束（写入 Prompt）：

- 只能使用给定 reason 与 evidence 中出现的事实；
- 不得新增未给出的原因（如「因为你和作者互动频繁」）；
- 不得声明因果确定性（用「可能/主要因为系统识别到」而非「一定是」）；
- 输出 ≤ 120 字。

## 5. 降级策略

| 情况 | 行为 |
|------|------|
| `request_id` 未提供 | 不查 Trace，仅用「关注关系 + 同城 + 兴趣匹配」生成解释，并标注 `source=UNKNOWN` |
| Trace 已过期（TTL） | 返回 `NO_TRACE` + 可判定的其它 reason |
| 内容画像未完成 | 跳过兴趣类 reason，不阻断 |
| 下游 RPC 超时 | 该 reason 跳过；若全部失败则返回明确错误，Agent 告知「暂时无法获取推荐原因」，**禁止**编造 |

原则：解释可以不完整，但不能虚假。

## 6. 对外契约

```json
{
  "feed_id": 88901,
  "request_id": "9f2c1d...",
  "source": "RECOMMEND_POOL",
  "reasons": [
    {
      "code": "INTEREST_TOPIC_MATCH",
      "text": "你最近较多完整观看「露营」相关内容，这条视频同属该主题",
      "evidence": { "matched_topics": ["露营", "西安周边"], "signal": "finish_and_collect", "window_days": 30 }
    },
    {
      "code": "SAME_CITY",
      "text": "这条内容发布于西安，与你当前所在城市一致",
      "evidence": { "city_name": "西安", "city_code": "610100" }
    }
  ],
  "generated_at": 1785302400000
}
```

Agent 场景返回同样的 `reasons` 结构 + 一段`natural_language` 文本，前端可二选一展示。

## 7. 测试策略

| 层次 | 用例 |
|------|------|
| 单元 | 各 source → reason 映射正确；优先级排序正确；最多 3 条 |
| 单元 | 兴趣命中标签为空时不生成 `INTEREST_TOPIC_MATCH` |
| 集成 | 关注流内容 → 含 `FOLLOW_AUTHOR` 且 evidence 中作者与实际一致 |
| 集成 | 用他人的 `request_id` 查询 → `Forbidden` |
| 集成 | Trace 过期 → 返回 `NO_TRACE` 且不报500 |
| 集成 | 兴趣画像与内容画像均缺失 → 至少返回一条可解释 reason 或明确的「无法定位」 |
| 契约 | 模型输出中出现未在 evidence 内的实体（用规则校验器扫描）→ 视为失败并降级为模板文案 |

## 8. 演进与 TODO

- 引入负反馈解释（「我不想看这类」→ 反向调整兴趣）。
- 待有正式粗排/精排后，补充「排序阶段」解释（分数构成、多样性打散）。
- 解释文案支持多语言与个性化语气。

---

## 关联文档

- [请求标识与 Feed 链路追踪](./02-request-trace.md)
- [用户兴趣画像](./06-user-interest.md)
- [Agent 服务设计](./09-agent-service.md)
- [接口契约](./11-api.md)
- [Feed 关注流设计](../feed/03-timeline-follow.md)
- [Feed 推荐流设计](../feed/04-timeline-recommend.md)
