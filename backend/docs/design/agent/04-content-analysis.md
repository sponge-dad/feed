# 内容分析服务设计

>定义视频内容理解流水线：触发与幂等、任务状态机、FFmpeg/ASR/OCR/多模态各阶段、模型输出校验、失败重试与删除下线。属实施阶段二。

---

## 1. 概述与定位

Content 服务域把「视频文件」转换为「结构化内容画像」，供检索、推荐解释与创作者分析使用。

| 组件 | 职责 |
|------|------|
| `app/content/worker`（独立进程） | 消费 `feed-created` / `feed-deleted`，执行分析流水线，写画像与索引 |
| `app/content/rpc`（9007） | 查询画像、批量查询、内容检索、重试分析（内部） |

硬性约束：

- **异步**：分析不在发帖主链路，失败不影响发帖与刷流。
- **幂等**：`feed_id + media_hash + model_version` 三元组唯一，不重复分析。
- **有界**：单任务的时长、文件大小、关键帧数、模型输入长度都有上限，防止成本失控。
- **不落隐私**：不记录长期有效的媒体地址，不把完整字幕写入日志。

## 2. 触发与幂等

```text
feed-created（event_id, feed_id, user_id, city_code, created_at）
  →查Feed 详情（Feed RPC.GetFeedDetail）
  → feed_type != 2（非视频）→ 返回 nil，直接 ACK
  → 计算 media_hash
  → 幂等判定
  → 执行流水线
```

`media_hash` 计算优先级：

1. 对象存储返回的 ETag / CRC64（若可从 URL HEAD 获取），成本最低；
2. 否则取下载后文件的 SHA-256 前 32 字节 hex。

幂等实现（双保险）：

| 层 | 手段 |
|----|------|
| DB | `feed_content_profiles.uk_feed_id` 唯一键；已存在且 `analysis_status=COMPLETED` 且 `model_version` 相同 → 跳过 |
| Redis | `content:analysis:lock:{feed_id}` SETNX（TTL = `FFmpegTimeoutSec × 3`），防止同一 feed 被两个 Worker 实例并发分析；任务结束后 DEL |

同一 feed 被重复投递（`feed-created` 重复消费）时，最终**只会有一条有效画像**。

## 3. 任务状态机

| 状态 | 含义 | 失败可重试 |
|------|------|:---:|
| `PENDING` | 任务已创建，等待调度 | - |
| `DOWNLOADING` | 下载媒体到本地临时目录 | ✅ |
| `EXTRACTING` | FFmpeg 提取音频 + 抽关键帧 | ✅ |
| `ASR_RUNNING` | 语音识别 | ✅ |
| `OCR_RUNNING` | 关键帧文字识别 | ✅ |
| `VISION_RUNNING` | 多模态摘要与标签生成 | ✅ |
| `INDEXING` | 写检索索引（ES/向量） | ✅ |
| `COMPLETED` | 全部完成 | - |
| `FAILED` | 超过最大重试次数 | 仅内部手动 `RetryContentAnalysis` |

状态每次流转都`UPDATE feed_content_profiles SET analysis_status=?, updated_at=?`，用于外部观测进度（创作者页面可显示「分析中」）。

**部分成功策略**：ASR 或 OCR 单独失败时，不整单失败——记录 `error_message`，跳过该字段继续 VISION 阶段，最终状态仍可为 `COMPLETED`（画像标注 `degraded=true`）。只有下载/抽帧失败（无任何可分析素材）才判失败。

## 4. 流水线各阶段

### 4.1 下载

| 项| 规则 |
|----|------|
| 地址来源 | Feed RPC 返回的媒体 key，由 Content Worker 调用 COS 签名生成**内部临时签名地址**（有效期≤ 10min，复用 `docs/design/oss/` 的签名能力） |
| 主机白名单 | 只允许 `AllowedMediaHosts`；解析后的 IP 必须为公网地址，禁止 `127.*/10.*/172.16-31.*/192.168.*/169.254.*` 及 IPv6 回环（SSRF 防护） |
| 大小上限 | `MaxVideoBytes`（默认 200MB），边下边校验，超限立即中断 |
| 时长上限 | `MaxVideoDurationSec`（默认 600s），由 `ffprobe` 先探测，超限直接置 `FAILED`（原因 `video_too_long`） |
| 落盘 | `TempDir/{feed_id}/`，任务结束（含失败）在 `defer` 中整目录删除；进程启动时清理残留|
| 日志 | 只记录 `feed_id`与文件大小，**禁止**记录签名地址 |

### 4.2 FFmpeg 音频提取与抽帧

统一通过 `exec.CommandContext` 传参数数组调用，**不使用 shell**，不拼接命令字符串；所有路径由程序生成，不含用户输入。

```bash
# 音频：16kHz 单声道 wav，多数 ASR 服务的推荐输入
ffmpeg -nostdin -y -i input.mp4 -vn -ac 1 -ar 16000 -f wav audio.wav

# 关键帧：场景切换 + 固定间隔兜底，缩放到长边 720，最多 KeyFrameMax 张
ffmpeg -nostdin -y -i input.mp4 -vf "select='gt(scene,0.3)',scale=720:-1" -vsync vfr frame_%03d.jpg
ffmpeg -nostdin -y -i input.mp4 -vf "fps=1/3,scale=720:-1" fallback_%03d.jpg
```

抽帧策略：优先取场景切换帧；不足 5 张时用固定间隔（每 3s 一帧）补齐；最终按时间均匀采样裁剪到 ≤ `KeyFrameMax`（默认 20，短视频足够）。首帧（封面对应帧）必须保留，因为开头信息对创作者诊断最关键。

资源限制：

- `context.WithTimeout(FFmpegTimeoutSec)`，超时 kill 进程组（`Setpgid` + `kill(-pid)`），防止僵尸进程。
- 并发上限 `MaxConcurrency`（默认 2），用带缓冲 channel 做信号量。
- 磁盘：单任务临时目录上限（默认 1GB），超限中断。

### 4.3 ASR

- 输入 `audio.wav`，输出分段字幕：`[{start_ms, end_ms, text}]`。
- 拼接为 `transcript`（全文）与 `transcript_segments`（JSON，供后续做「开头 3秒讲了什么」分析）。
- 语言检测结果写 `language`（如 `zh-CN`）；无语音（纯音乐/静音）→ `transcript` 为空并标记 `no_speech`。
- 调用外部服务，超时 60s，重试 2 次（指数退避）。

### 4.4 OCR

- 对关键帧批量识别，输出去重后的文字数组 `ocr_text`。
- 去重规则：文本归一化（去空格、统一大小写）后完全相同则合并；单条长度 > 100 字符截断。
- 数组上限 30 条，超出取出现频次最高者（字幕条常驻，频次高，正是想要的内容）。

### 4.5 多模态摘要

输入拼装（顺序即优先级，超长按此顺序截断）：

```text
title / description（原文，各≤ 200 字）
transcript（≤ TranscriptMaxRunes，默认 4000 字，超长取「开头 60% + 结尾 20%」）
ocr_text（≤ 30 条）
关键帧（≤ 8 张送入多模态模型，按时间均匀采样，首帧必送）
```

要求模型输出严格 JSON：

```json
{
  "category": "户外旅行",
  "summary": "视频介绍了西安周边一处适合周末露营的营地",
  "topics": ["露营", "西安周边", "周末出游"],
  "objects": ["帐篷", "草地", "汽车", "烧烤架"],
  "scenes": ["户外", "营地", "山地"],
  "styles": ["攻略", "体验分享"]
}
```

Prompt 约束：只允许基于给定素材归纳；不确定则留空；禁止编造地点与品牌；输出必须是 JSON 且不带解释文字。

### 4.6 向量化与索引

- 用摘要 + 标题 + 标签拼成一段文本，调用 embedding 服务生成向量（维度写入配置 `EmbeddingDim`）。
- 写入 ES 文档（含向量字段），详见 [05-content-search.md](./05-content-search.md)。
- 索引失败可单独重试（状态停在 `INDEXING`），不需要重跑昂贵的 ASR/OCR/多模态阶段。

## 5. 模型输出校验（入库前必过）

| 校验 | 规则 | 不通过处理 |
|------|------|-----------|
| JSON 结构 | 必须能反序列化到目标结构体；用 `json.Unmarshal` 到明确结构体，不用 `interface{}` 动态执行 | 重试 1 次，仍失败则该字段留空 |
| `category` | 必须在**类目白名单**内（配置维护，如户外旅行/美食/健身/知识科普/…），否则映射为 `其他` | 映射兜底 |
| `topics` | 数量 ≤ 10，单条长度 1~20 字符，去重，去除纯符号/纯数字 | 超限截断 |
| `objects` / `scenes` / `styles` | 数量 ≤ 15 / 8 / 5，单条 ≤ 20 字符 | 超限截断 |
| `summary` | 长度 20~200 字，去除换行 | 超长截断 |
| 敏感词 | 标签与摘要过敏感词表；命中则丢弃该标签，摘要命中则整体置空并标记 `sensitive_blocked` | 记录审计日志 |
| 一致性 | 标签不得包含明显与素材无关的地名/品牌（用「素材文本是否包含该词」做弱校验，仅告警不阻断） | 告警 |

原则：**模型输出是不可信输入**，必须经结构、范围、白名单、敏感词四层校验后才允许入库。

## 6. 失败重试与死信

| 层 | 策略 |
|----|------|
| 阶段内 | 外部服务调用重试 2 次，指数退避（1s、4s） |
| 任务级 | `retry_count` 最多 3 次；每次重试由 RocketMQ 重投驱动（回调返回 error）|
| 超限 | `analysis_status=FAILED`，写 `error_message`（≤ 1024 字符，脱敏），返回 nil 让消息 ACK，避免死信堆积 |
| 恢复 | 内部接口 `RetryContentAnalysis(feed_id, force)`：重置状态为 `PENDING` 并重新入队；`force=true` 时忽略幂等（用于模型升级重跑） |
| 模型升级 | `model_version` 变化时，通过内部批量任务重跑，不影响线上旧画像（更新为原子替换） |

不可恢复错误（直接 `FAILED`，不重试）：媒体地址非法/不在白名单、`feed_type` 非视频、视频时长或体积超限、媒体已被删除（404）。

## 7. 删除与下线

```text
feed-deleted → Content Worker
  1. UPDATE feed_content_profiles SET analysis_status='DISABLED' WHERE feed_id=?
  2. 删除 ES 文档（幂等，不存在也算成功）
  3. 删除 Redis 画像缓存 content:profile:{feed_id}
```

采用「软禁用 + 删索引」而非物理删除：保留分析结果便于问题追溯，但检索与Agent 一律查不到（Content RPC 对 `DISABLED` 返回「不存在」语义）。

## 8. 对外查询语义

| 状态 | `GetContentProfile` 返回 |
|------|--------------------------|
| 无记录 | `15001 内容画像不存在` |
| `PENDING`~`INDEXING` | `15002 内容分析进行中`（附 `analysis_status`，便于前端展示进度） |
| `FAILED` | `15003 内容分析失败`（内部用户可见 `error_message`，普通用户不可见） |
| `DISABLED` | 按不存在处理 |
| `COMPLETED` | 完整画像；**字幕全文与 OCR 全文仅作者本人或内部用户可见**，其他调用方只返回 `category/summary/topics/scenes` |

## 9. 测试策略

| 层次 | 用例 |
|------|------|
| 单元 | 抽帧数量裁剪、`transcript` 截断策略、标签校验（超长/超量/敏感词/非白名单类目） |
| 单元 | FFmpeg 参数构造不含shell 元字符；超时后进程被kill |
| 单元 | 媒体地址为内网 IP / 非白名单域名 → 直接失败且不发起请求 |
| 集成 | 同一 `feed-created` 重复消费 3 次 → 只有 1 条画像、外部模型只被调用 1 次 |
| 集成 | 图文 Feed（`feed_type=1`）不触发分析 |
| 集成 | ASR 失败但 OCR 成功 → 状态 `COMPLETED` 且标记降级 |
| 集成 | `feed-deleted` 后画像禁用、ES 文档消失、检索不返回该feed |
| 集成 | 分析全程失败不影响发帖与Timeline（Feed 接口正常） |

## 10. 演进与 TODO

- 按镜头切分做「分段摘要」，支持「视频第几秒讲了什么」。
- 抽帧改为镜头边界检测 + 显著性打分，替代固定阈值。
- 自建推理服务（Python）替换外部 ASR/OCR，降低单位成本。
- 画像纠错流程：创作者反馈 → 人工审核 → 覆盖字段并记录来源。

---

## 关联文档

- [架构与服务拆分](./01-architecture.md)
- [自然语言内容检索](./05-content-search.md)
- [数据模型](./10-data-model.md)
- [接口契约](./11-api.md)
- [安全要求](./13-security.md)
- [对象存储设计](../oss/README.md)
- [Feed MQ 事件设计](../feed/07-mq-event.md)
