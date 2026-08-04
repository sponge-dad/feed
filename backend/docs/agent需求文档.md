# 短视频内容理解与 Feed 智能助手需求文档

文档版本：V1.0
项目名称：FeedMind Agent
项目类型：短视频内容理解与 Feed 智能助手
目标场景：类抖音、类小红书 Feed 系统
主要用户：普通用户、内容创作者
后端技术栈：Go、go-zero、gRPC、MySQL、Redis、RocketMQ、Eino、FFmpeg
项目目标：秋招简历项目与现有业务系统实际接入

## 1. 项目背景

现有系统已经实现 Gateway、Feed、User、Comment、Relation 和 Interaction 等服务。

外部 HTTP 请求经过 Gateway 完成路由匹配和 JWT 鉴权，Gateway 再通过 go-zero zRPC 调用下游服务。Feed 服务负责动态的创建、删除和时间流读取，并采用 Redis 有序集合实现 inbox、outbox、推荐池和同城池。

系统采用推拉结合的 Feed 模型。普通用户发布内容后，由异步 Worker 将内容推送到粉丝 inbox；大 V 内容保存在 outbox 和推荐池中，在读取时进行拉取和合并。Feed 发布、删除、评论计数和互动计数等副作用通过 RocketMQ 异步处理，以降低主链路延迟。

现有系统已经具备点赞、收藏、评论、关注、发帖和删除等业务能力，但尚未建设曝光、播放、播放时长、完播和快速划走等行为埋点，也没有保存 Feed 请求级的召回来源、过滤过程和排序记录。

视频和图文统一保存在 `feeds` 表中，通过 `feed_type` 区分。当前视频数据主要包含标题、描述、媒体地址和封面，缺少字幕、内容标签、场景、主题、实体和内容摘要等机器理解结果。

因此，当前系统只能根据关注关系、发布时间、推荐池和地域信息分发内容，无法深入理解视频内容，也无法准确回答以下问题：

```text
这条视频主要讲了什么？
帮我找一些西安周边露营的视频。
为什么系统给我展示这条内容？
我的视频为什么播放效果较差？
系统认为我的视频属于什么类别？
```

本项目通过新增内容分析能力、行为采集能力和 Agent 服务，使用户可以通过自然语言查询内容、理解推荐原因，并使创作者能够分析作品表现。

## 2. 项目定位

FeedMind Agent 不替代 Feed Service，也不直接参与每一次在线 Feed 请求。

在线刷流仍然由现有 Feed Service 完成。Agent 位于在线 Feed 链路之外，通过受控的 RPC Tool 查询 Feed、Interaction、Relation、User 和内容分析结果。

Agent 的主要职责是理解用户提出的问题、选择合适的业务工具、获取真实数据，并将结构化结果组织成自然语言答案。

视频理解本身不由 Agent 实时完成。视频发布后，独立的内容分析服务通过异步任务提前解析视频，Agent 查询已经生成的内容画像。

整体架构如下：

```text
客户端
   │
   ▼
Gateway :8080
   │
   ├── User RPC
   ├── Feed RPC
   ├── Comment RPC
   ├── Relation RPC
   ├── Interaction RPC
   └── Agent RPC
              │
              ▼
        Agent Service
              │
              ├── Feed Tool
              ├── Content Tool
              ├── Interaction Tool
              ├── Relation Tool
              ├── User Interest Tool
              └── Metrics Tool

视频发布
   │
   ▼
Feed Service
   │
   ├── MySQL 写入 feeds
   └── RocketMQ feed-created
              │
              ├── Feed Worker
              │      ├── Outbox
              │      ├── Inbox
              │      ├── Recommend Pool
              │      └── City Pool
              │
              └── Content Analysis Worker
                     ├── 下载视频
                     ├── FFmpeg 提取音频
                     ├── 视频关键帧抽取
                     ├── ASR 语音识别
                     ├── OCR 画面文字识别
                     ├── 多模态内容分析
                     └── 保存内容画像

客户端行为
   │
   ▼
Gateway 埋点接口
   │
   ▼
RocketMQ feed-behavior-event
   │
   ▼
Behavior Worker
   ├── 行为明细
   ├── 内容指标聚合
   └── 用户兴趣画像
```

## 3. 建设目标

系统完成后，视频发布会自动触发内容分析，生成内容摘要、分类、主题标签、OCR 文字、语音字幕、场景标签和内容向量。

普通用户可以通过自然语言搜索短视频，例如：

```text
帮我找一些适合周末去的西安露营地。
```

Agent 可以检索标题、描述、字幕、OCR 文字、内容标签和语义向量，并返回符合要求的 Feed 内容。

普通用户可以查看某条内容的推荐原因，例如：

```text
为什么给我展示这条视频？
```

系统根据 Feed 来源、关注关系、同城信息和用户兴趣画像返回可解释结果。

内容创作者可以询问：

```text
分析一下我的视频为什么播放效果不好。
```

Agent 查询曝光、播放、有效播放、播放时长、完播、快速划走、点赞、收藏和评论数据，并与同类型内容的平均数据进行比较。

内部开发人员可以按照 `request_id` 查询某次 Feed 请求的数据来源、返回数量和聚合状态，用于定位 Feed 链路问题。

## 4. 第一版功能范围

第一版需要形成完整业务闭环，而不是只实现大模型聊天接口。

第一版包含视频内容分析、行为事件采集、内容指标聚合、用户兴趣画像、自然语言内容检索、推荐原因解释、创作者作品分析和 Agent 执行记录。

第一版不允许 Agent 执行写操作。Agent 不能修改视频标题、内容标签、Feed 权重、用户兴趣或推荐池配置。

第一版使用单 Agent 架构。Eino Agent 根据用户问题选择 Tool，但业务指标计算、权限校验、内容过滤和结果排序必须由后端代码完成。

第一版不要求建设复杂的深度学习推荐模型，也不要求替换现有推拉结合 Timeline。

## 5. 用户角色与权限

普通用户可以查询公开 Feed 内容，可以查看自己的兴趣摘要和自己的推荐原因，也可以执行自然语言内容搜索。

普通用户不能查询其他用户的兴趣画像，不能查看内部排序分数，不能查看其他用户的行为明细。

内容创作者可以查看自己发布作品的内容理解结果和表现指标，可以获取标题、标签、开头内容和目标受众方面的分析建议。

创作者不能查看其他创作者的详细数据，只能看到经过聚合处理的同类内容平均指标。

内部管理员和开发人员可以查看 Feed 请求 Trace、内容分析失败信息和 Tool 调用记录。该能力通过内部权限控制，不对普通用户开放。

用户身份必须来自 Gateway JWT 中间件注入的 Context。Agent 不接受模型生成或用户输入的 `user_id` 作为真实身份依据。

## 6. 核心功能需求

### 6.1 视频内容自动分析

当用户发布 `feed_type=2` 的视频内容后，Feed Service 正常完成 MySQL 写入并发送 `feed-created` 事件。

内容分析 Worker 订阅该事件。当事件对应视频 Feed 时，创建内容分析任务。

内容分析过程包括视频文件下载、音频提取、关键帧抽取、语音识别、画面文字识别和多模态内容总结。

系统需要生成以下结果：

```json
{
  "feed_id": "88901",
  "category": "户外旅行",
  "summary": "视频介绍了西安周边一处适合周末露营的营地",
  "topics": ["露营", "西安周边", "周末出游"],
  "objects": ["帐篷", "草地", "汽车", "烧烤架"],
  "scenes": ["户外", "营地", "山地"],
  "style": ["攻略", "体验分享"],
  "language": "zh-CN",
  "transcript": "今天带大家看一个距离西安市区约一个小时的露营地……",
  "ocr_text": ["西安周边露营", "自驾约一小时"],
  "content_status": "COMPLETED"
}
```

内容分析必须异步执行，不能阻塞发帖主流程。

同一个视频不得重复执行分析。系统使用 `feed_id + media_hash + model_version` 作为幂等依据。

任务失败后最多自动重试三次。超过重试次数后设置为 `FAILED`，并记录错误原因。

删除视频后，应通过 `feed-deleted` 事件删除或禁用对应的内容画像和向量索引。

### 6.2 自然语言内容检索

普通用户可以输入自然语言查找 Feed 内容，例如：

```text
找一些西安周边适合新手的露营视频。
找几条简单的宿舍减脂餐教程。
有没有讲 Go 微服务限流的视频？
```

Agent 首先从请求中提取主题、地域、内容类型、时间范围和其他条件。

系统使用标题、描述、字幕、OCR 文字、主题标签和内容向量进行混合检索。

检索结果必须继续执行业务过滤，包括 Feed 状态、作者状态、内容审核状态和用户可见性。

结果由后端检索服务排序，大模型只负责生成结果说明，不能凭空构造 Feed。

返回结果包含 Feed ID、标题、封面、作者、内容摘要、匹配标签和匹配原因。

### 6.3 推荐原因解释

用户可以在 Feed 卡片中点击“为什么推荐”，也可以在 Agent 会话中询问某个 Feed 的展示原因。

当前 Feed 系统需要为每条返回内容补充来源字段：

```text
FOLLOW_INBOX
VIP_OUTBOX
RECOMMEND_POOL
CITY_POOL
INBOX_REBUILD
```

`FOLLOW_INBOX` 表示该内容由关注作者发布后推入用户 inbox。

`VIP_OUTBOX` 表示内容来自大 V 作者 outbox 拉取。

`RECOMMEND_POOL` 表示内容来自公共推荐池。

`CITY_POOL` 表示内容根据城市或地域匹配进入结果。

`INBOX_REBUILD` 表示用户 inbox 缺失后，通过回源重建获得。

对于关注流内容，解释可以是：

```text
你关注了该作者，这条内容是作者最近发布的作品。
```

对于同城池内容，解释可以是：

```text
这条内容发布于你当前所在城市，内容主题与本地生活相关。
```

对于推荐池内容，需要结合用户兴趣画像和视频内容标签生成解释：

```text
你最近完整观看和收藏了多条露营相关内容，这条视频同样属于户外露营主题。
```

推荐原因必须从结构化 `reason_codes` 生成，不允许大模型自行推测。

### 6.4 用户行为采集

现有点赞、取消点赞、收藏和取消收藏继续使用 `interaction-event`。

新增 `feed-behavior-event` Topic，用于记录 Feed 展示和播放行为。

事件结构建议如下：

```go
type FeedBehaviorEvent struct {
    EventID         string `json:"event_id"`
    RequestID       string `json:"request_id"`
    UserID          int64  `json:"user_id"`
    FeedID          int64  `json:"feed_id"`
    AuthorID        int64  `json:"author_id"`
    ActionType      string `json:"action_type"`
    Position        int32  `json:"position"`
    WatchDurationMs int64  `json:"watch_duration_ms"`
    MediaDurationMs int64  `json:"media_duration_ms"`
    Timestamp       int64  `json:"timestamp"`
}
```

`ActionType` 第一版支持：

```text
EXPOSE
PLAY
EFFECTIVE_PLAY
FINISH
SKIP
SHARE
```

曝光事件在内容真正进入用户可视区域后上报，不能在接口返回时直接算作曝光。

有效播放可以定义为播放时间达到三秒，或者播放时长达到视频总时长的某个比例。

快速划走由 `SKIP` 表示，需要同时上报实际观看时长。

事件携带 `request_id` 和 `position`，用于分析内容在 Feed 中的展示位置及其后续行为。

消费端使用 `event_id` 进行幂等去重。

### 6.5 用户兴趣画像

Behavior Worker 根据用户的观看和互动行为异步更新兴趣画像。

兴趣画像以内容标签和内容类别为主要维度。

第一版可以采用规则权重，不需要训练机器学习模型。建议初始行为权重如下：

```text
完成播放：+5
收藏：+4
点赞：+3
评论：+3
分享：+3
有效播放：+2
普通曝光：0
快速划走：-2
取消收藏：-3
取消点赞：-2
```

用户兴趣分数需要加入时间衰减，避免早期行为长期主导用户画像。

Redis 使用 ZSet 保存实时兴趣：

```text
user:interest:{user_id}
```

Member 为标签或类别，Score 为当前兴趣权重。

MySQL 保存周期性快照，用于数据恢复和离线分析。

Agent 只能查询用户本人的兴趣摘要，不能返回过细的内部权重计算过程。

### 6.6 创作者作品表现分析

创作者可以要求 Agent 分析自己作品的表现。

系统需要提供以下指标：

```text
曝光量
播放量
播放率
有效播放量
有效播放率
平均播放时长
完播率
快速划走率
点赞率
收藏率
评论率
分享率
```

基础公式如下：

```text
播放率 = 播放量 / 曝光量
有效播放率 = 有效播放量 / 曝光量
完播率 = 完播量 / 播放量
快速划走率 = 快速划走量 / 曝光量
点赞率 = 点赞量 / 播放量
收藏率 = 收藏量 / 播放量
```

Agent 需要将作品指标与同类别、相近发布时间和相近视频时长的内容进行比较。

Agent 不得只输出“内容质量较低”之类的笼统结论。

期望结果如下：

```text
该视频最近 24 小时获得 8,214 次曝光，播放率为 10.7%，低于同类视频平均值 15.2%。

前三秒快速划走率为 61.4%，高于同类平均值 43.8%。已经开始播放的用户中，完播率为 48.3%，接近同类平均水平。

主要问题发生在视频开始前和前三秒，说明封面、标题或开头信息表达不足，而不是视频中后段内容明显偏弱。
```

系统可以根据内容分析结果给出改进建议，例如封面信息不明确、标题缺少主题词、视频开头未快速展示核心内容。

建议必须明确标注为分析建议，不能表述为确定因果关系。

### 6.7 内容理解结果查询

创作者可以查看自己视频的机器理解结果。

系统展示自动识别的内容类别、主题标签、字幕、OCR 文字、场景和内容摘要。

创作者发现识别不准确时，第一版只允许提交反馈，不直接修改内容画像。修改和审核能力放到后续版本。

### 6.8 Feed 请求链路追踪

Gateway 必须生成真正可用的 `request_id`。

`request_id` 与 go-zero 框架的 `trace_id` 保持不同职责：

```text
request_id：标识一次业务 HTTP 请求，并返回给客户端。
trace_id：用于 OpenTelemetry 分布式链路追踪。
run_id：标识一次 Agent 执行任务。
event_id：标识一次 MQ 业务事件，用于幂等。
```

Gateway 收到请求后生成 `request_id`，写入 Context，并通过 gRPC Metadata `x-request-id` 传递到下游服务。

HTTP 响应头和响应体都需要返回 `request_id`。

Feed RPC、User RPC、Relation RPC 和 Interaction RPC 的日志都需要打印相同的 `request_id`。

Feed Timeline RPC 需要记录本次请求读取了哪些数据源，以及每个数据源返回的数量。

第一版不需要记录完整精排过程，因为当前系统尚未实现正式粗排和精排模型，但必须记录现有的 inbox、outbox、推荐池、同城池和重建路径。

## 7. Agent 功能设计

Agent Service 建议新增为独立的 go-zero RPC 服务：

```text
app/agent/rpc
```

服务注册到现有 etcd：

```yaml
Name: agent.rpc
ListenOn: 0.0.0.0:9006
Etcd:
  Hosts:
    - 127.0.0.1:2479
  Key: agent.rpc
```

Gateway 增加 Agent HTTP 路由，通过 Agent RPC 创建会话和执行任务。

Agent 第一版采用单 Agent 和多个 Tool 的模式。

核心 Tool 包括：

```go
type GetFeedDetailTool struct{}
type GetFeedSourceTool struct{}
type GetContentProfileTool struct{}
type SearchContentTool struct{}
type GetUserInterestTool struct{}
type GetCreatorMetricsTool struct{}
type GetPeerMetricsTool struct{}
type GetFeedRequestTraceTool struct{}
```

`GetFeedDetailTool` 查询 Feed 基础信息和作者信息。

`GetFeedSourceTool` 查询 Feed 在指定请求中的数据来源和推荐原因编码。

`GetContentProfileTool` 查询视频内容摘要、标签、字幕、OCR 和场景。

`SearchContentTool` 根据自然语言结构化条件检索 Feed。

`GetUserInterestTool` 查询当前用户兴趣摘要。

`GetCreatorMetricsTool` 查询创作者本人作品的行为指标。

`GetPeerMetricsTool` 查询经过匿名聚合的同类内容平均指标。

`GetFeedRequestTraceTool` 仅对内部用户开放，用于查询 Feed 请求读取路径和聚合结果。

Agent 不能直接访问 Redis 和业务数据库。所有 Tool 必须通过 RPC 或专用 Repository 接口访问数据。

## 8. Agent 执行流程

一次 Agent 请求生成一个 `run_id`。

任务状态如下：

```text
CREATED
UNDERSTANDING
TOOL_CALLING
ANALYZING
GENERATING
SUCCEEDED
FAILED
CANCELLED
```

典型执行过程为：

```text
用户输入
   ↓
Gateway JWT 鉴权
   ↓
创建 Agent Run
   ↓
识别用户意图
   ↓
权限检查
   ↓
选择 Tool
   ↓
Tool 参数校验
   ↓
调用业务 RPC
   ↓
校验数据完整性
   ↓
生成结构化结论
   ↓
大模型组织自然语言
   ↓
记录执行结果
```

模型负责意图识别、Tool 选择和语言生成。

Go 代码负责身份校验、权限控制、Tool 参数校验、指标计算、内容过滤和最终结果校验。

Tool 调用失败时，Agent 必须明确说明数据获取失败，不能伪造成功结果。

单次 Agent Run 最多允许调用八次 Tool 和四次模型，防止循环调用。

## 9. 内容分析服务设计

内容分析能力不建议直接运行在 Feed RPC 进程内，避免 FFmpeg 和模型调用占用 Feed 服务资源。

建议新增独立服务：

```text
app/content/rpc
app/content/worker
```

Content Worker 订阅 `feed-created`，Content RPC 对外提供内容画像查询和内容搜索接口。

内容分析任务状态如下：

```text
PENDING
DOWNLOADING
EXTRACTING
ASR_RUNNING
OCR_RUNNING
VISION_RUNNING
INDEXING
COMPLETED
FAILED
```

FFmpeg 负责音频提取和关键帧抽取。

视频可以按照固定间隔和镜头变化结合的方式抽帧。短视频第一版最多保留十到二十张关键帧，防止分析成本失控。

ASR 输出字幕和时间区间。

OCR 输出关键帧上的文字内容。

多模态模型结合标题、描述、字幕、OCR 和关键帧生成内容摘要与标签。

大模型生成的标签需要经过长度、数量和敏感词校验后才能入库。

## 10. 数据库设计

新增 `feed_content_profiles` 表保存内容画像：

```sql
CREATE TABLE feed_content_profiles (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    feed_id BIGINT NOT NULL,
    media_hash VARCHAR(128) NOT NULL DEFAULT '',
    category VARCHAR(64) NOT NULL DEFAULT '',
    summary TEXT,
    topics JSON,
    objects JSON,
    scenes JSON,
    styles JSON,
    transcript MEDIUMTEXT,
    ocr_text TEXT,
    language VARCHAR(32) NOT NULL DEFAULT '',
    analysis_status VARCHAR(32) NOT NULL,
    model_version VARCHAR(64) NOT NULL DEFAULT '',
    error_message VARCHAR(1024) NOT NULL DEFAULT '',
    analyzed_at DATETIME(3),
    created_at DATETIME(3) NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_feed_id (feed_id),
    KEY idx_category_status (category, analysis_status)
);
```

新增 `feed_behavior_events` 表保存必要的行为明细。数据量扩大后可迁移到分析型存储：

```sql
CREATE TABLE feed_behavior_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    event_id VARCHAR(64) NOT NULL,
    request_id VARCHAR(64) NOT NULL DEFAULT '',
    user_id BIGINT NOT NULL,
    feed_id BIGINT NOT NULL,
    author_id BIGINT NOT NULL,
    action_type VARCHAR(32) NOT NULL,
    position INT NOT NULL DEFAULT 0,
    watch_duration_ms BIGINT NOT NULL DEFAULT 0,
    media_duration_ms BIGINT NOT NULL DEFAULT 0,
    event_time DATETIME(3) NOT NULL,
    created_at DATETIME(3) NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_event_id (event_id),
    KEY idx_feed_event_time (feed_id, event_time),
    KEY idx_user_event_time (user_id, event_time),
    KEY idx_request_id (request_id)
);
```

新增 `feed_metrics_hourly` 表保存聚合指标：

```sql
CREATE TABLE feed_metrics_hourly (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    feed_id BIGINT NOT NULL,
    stat_hour DATETIME NOT NULL,
    expose_count BIGINT NOT NULL DEFAULT 0,
    play_count BIGINT NOT NULL DEFAULT 0,
    effective_play_count BIGINT NOT NULL DEFAULT 0,
    finish_count BIGINT NOT NULL DEFAULT 0,
    skip_count BIGINT NOT NULL DEFAULT 0,
    watch_duration_ms BIGINT NOT NULL DEFAULT 0,
    like_count BIGINT NOT NULL DEFAULT 0,
    collect_count BIGINT NOT NULL DEFAULT 0,
    comment_count BIGINT NOT NULL DEFAULT 0,
    share_count BIGINT NOT NULL DEFAULT 0,
    created_at DATETIME(3) NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_feed_hour (feed_id, stat_hour),
    KEY idx_stat_hour (stat_hour)
);
```

新增 `user_interest_profiles` 表保存兴趣快照：

```sql
CREATE TABLE user_interest_profiles (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id BIGINT NOT NULL,
    interest_json JSON NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    calculated_at DATETIME(3) NOT NULL,
    created_at DATETIME(3) NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_user_id (user_id)
);
```

Agent Service 自有表包括：

```text
agent_sessions
agent_messages
agent_runs
agent_tool_calls
```

`agent_tool_calls` 只保存经过脱敏的输入和输出摘要，不保存用户 JWT、媒体签名地址和完整隐私数据。

## 11. RPC 接口需求

Feed RPC 需要增加以下接口：

```go
GetFeedDetail
GetFeedBatch
GetFeedSource
GetFeedRequestTrace
GetCreatorFeedList
```

Content RPC 需要提供：

```go
GetContentProfile
BatchGetContentProfile
SearchContent
RetryContentAnalysis
```

Interaction RPC 需要增加或补充：

```go
GetFeedMetrics
BatchGetFeedMetrics
GetCreatorMetrics
GetPeerAverageMetrics
```

User 或新增画像模块需要提供：

```go
GetUserInterestProfile
```

Agent RPC 需要提供：

```go
CreateSession
SendMessage
GetRun
GetSessionMessages
CancelRun
```

所有 RPC 请求必须支持通过 Context Metadata 透传：

```text
x-request-id
x-trace-id
x-agent-run-id
```

## 12. Gateway HTTP 接口

创建 Agent 会话：

```http
POST /api/agent/sessions
```

发送消息：

```http
POST /api/agent/sessions/:session_id/messages
```

获取执行结果：

```http
GET /api/agent/runs/:run_id
```

查询内容分析结果：

```http
GET /api/feeds/:feed_id/content-profile
```

查询推荐原因：

```http
GET /api/feeds/:feed_id/recommendation-reason?request_id=xxx
```

上报 Feed 行为：

```http
POST /api/feed/behaviors
```

行为上报接口应支持批量提交，降低客户端请求数量。

## 13. 后端依赖

现有基础依赖继续使用 go-zero、zRPC、MySQL、Redis、RocketMQ 和 etcd。

Agent 新增 Eino 依赖，用于 ChatModel、Tool Calling 和 Agent 编排：

```text
github.com/cloudwego/eino
github.com/cloudwego/eino-ext/components/model/ark
```

内容处理依赖 FFmpeg。Go 服务通过 `os/exec` 或封装后的任务执行器调用 FFmpeg，但必须设置执行超时、输入路径校验和资源限制。

大模型、ASR 和 OCR 第一版建议使用外部服务接口，避免同时维护 Python 推理环境。后续需要降低成本时，再拆出独立 Python 推理服务。

语义检索第一版数据量较小时，可以使用 Elasticsearch 或支持向量检索的 Redis。只使用 MySQL `LIKE` 不足以支持字幕和语义搜索。

Prometheus 需要从间接依赖变为正式依赖，并为 Gateway、Feed、Content 和 Agent 服务开启指标暴露。

OpenTelemetry 需要通过 go-zero Telemetry 配置正式启用，将 HTTP、RPC、MQ 消费和 Agent Tool 调用串联起来。

## 14. 可观测性要求

系统需要采集以下指标：

```text
agent_run_total
agent_run_duration_seconds
agent_tool_call_total
agent_tool_call_duration_seconds
agent_llm_request_total
content_analysis_total
content_analysis_duration_seconds
content_analysis_failed_total
feed_behavior_event_total
feed_behavior_consume_lag
feed_request_total
feed_returned_count
```

日志必须包含：

```text
request_id
trace_id
run_id
event_id
user_id
feed_id
tool_name
service_name
```

RocketMQ 消费日志不能只记录错误文本，还需要记录 Topic、EventID、FeedID 和重试次数。

## 15. 安全要求

Agent 不允许执行任意 SQL、任意 RPC、系统命令或 Redis 命令。

每个 Tool 必须拥有固定的输入结构和固定的下游接口。

用户只能查询自己的兴趣画像和自己的创作者数据。

视频字幕和 OCR 结果可能包含隐私数据，发送给模型前必须进行长度限制和必要脱敏。

视频下载地址应使用内部临时签名地址，不能把长期有效的对象存储地址记录到 Agent 日志。

用户输入可能包含提示词注入。用户输入只能作为业务数据，不能覆盖系统 Prompt、权限规则和 Tool 注册范围。

## 16. 验收标准

视频发布成功后，应在异步时间窗口内生成内容画像。内容分析失败不得影响 Feed 发布和刷流。

同一个 `feed-created` 事件被重复消费时，只能生成一条有效内容画像。

自然语言内容检索返回的 Feed 必须真实存在，并满足状态和权限要求。

推荐原因必须包含明确的结构化来源，不能只返回泛化文本。

创作者只能查询本人作品数据。

作品表现分析中的所有数字必须来自指标接口，不能由模型生成。

Gateway 返回的每个请求都必须包含非空 `request_id`，并能在 Feed、Interaction、Content 和 Agent 服务日志中检索到。

行为事件重复消费时不能导致指标重复累加。

Agent Tool 调用成功率在测试环境中应达到 99% 以上。

内容检索评测集上的主题匹配准确率应达到 85% 以上。

Agent 在无数据、权限不足、内容分析未完成和下游超时等情况下，需要返回明确错误状态，不得生成伪造结论。

## 17. 项目实施顺序

第一阶段先修复请求标识链路，并增加曝光、播放、完播和快速划走事件。这是后续创作者分析和推荐解释的基础。

第二阶段建设 Content Service，完成 FFmpeg、ASR、OCR、多模态摘要和内容画像入库。

第三阶段建设内容搜索和用户兴趣画像。

第四阶段接入 Eino Agent，实现内容查询、推荐解释和创作者分析 Tool。

第五阶段补充 Prometheus、OpenTelemetry、Agent Trace 和 Feed 请求诊断页面。

该实施顺序能够保证每一个阶段都有独立可运行成果，也能避免先完成聊天界面但没有真实业务数据可调用的问题。

## 18. 最终演示效果

演示一：创作者发布一条标题较模糊的视频。后台消费 `feed-created` 事件，自动提取字幕、OCR 和关键帧，并生成“西安周边露营攻略”等内容标签。

演示二：普通用户输入“找一些西安周边适合新手露营的视频”。Agent 调用内容检索 Tool，返回真实 Feed 卡片和匹配原因。

演示三：用户点击某条内容的“为什么推荐”。系统根据关注关系、同城来源、推荐池来源和兴趣标签生成解释。

演示四：创作者输入“分析我这条视频为什么播放效果不好”。Agent 查询真实曝光、播放、快速划走和完播数据，与同类内容比较，并给出基于指标的分析。

演示五：开发人员使用 `request_id` 查询一次 Feed 请求，查看其经过 Gateway、Feed RPC、Redis Timeline、聚合层和下游 RPC 的完整链路。

项目最终能够体现 Feed 推拉结合、RocketMQ 异步解耦、内容多模态理解、行为数据采集、用户兴趣画像、语义检索、Agent Tool Calling、分布式链路追踪、幂等消费和微服务权限治理等后端能力。
