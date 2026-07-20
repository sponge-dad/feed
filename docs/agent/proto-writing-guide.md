# Protobuf 编写规范

> 本文档约束 Feed 项目内部 gRPC 服务间通信的 `.proto` 文件编写规范。
> 已生成的 `user.proto` / `relation.proto` 不 retroactive 修改，后续 `feed.proto`、
> `comment.proto`、`interaction.proto` 必须严格遵循本规范。

---

## 1. 定位：proto 只负责 RPC 定义，不定义 HTTP 接口

| 文件类型 | 用途 | 示例 |
|---|---|---|
| `.proto` | 内部微服务之间的 gRPC 通信契约 | `api/proto/feed/feed.proto` |
| `.api` | 对外 HTTP 网关的 REST 接口定义 | `app/gateway/api/feed.api` |
| `.md`（api-spec） | 接口文档（人类可读） | `docs/design/api-spec/feed.md` |

**重要原则**：go-zero 的 HTTP 网关不是 gRPC-Gateway 模式，不会读取 proto 中的 `google.api.http` 注解。因此：

- proto 中**只定义 `service` 和 `rpc`**，不定义 HTTP 路径/方法。
- HTTP 接口全部用 go-zero 的 `.api` 语法定义，与 proto 分开维护。

---

## 2. 文件组织

### 2.1 目录结构

每个服务的 proto 独立目录，便于 `goctl rpc protoc` 生成：

```
api/proto/
├── user/user.proto
├── relation/relation.proto
├── feed/feed.proto
├── comment/comment.proto
└── interaction/interaction.proto
```

### 2.2 文件名

- 小写，与服务名一致，例如 `feed.proto`。
- 一个 proto 文件对应一个服务，不允许多服务合并。

---

## 3. proto 头部规范

### 3.1 语法与包声明

```protobuf
syntax = "proto3";

package feed;                              // 小写，与服务名一致
option go_package = "./feed";              // 用于 goctl 生成 Go 包的路径
```

- `package` 必须与服务名一致，避免不同服务之间消息名冲突。
- `go_package` 固定写成 `./feed` 形式，goctl 会根据 `--go_out` 自动组织目录。

### 3.2 不引入 `google/api/http.proto`

本规范禁止在 proto 中使用 `google.api.http` 注解定义 HTTP 接口。所有对外 HTTP 接口定义写在 `.api` 文件中。

---

## 4. 消息命名规范

### 4.1 请求/响应消息命名

采用 **`<Action><Req|Resp>`** 形式，与 rpc 方法一一对应：

| rpc 方法 | 请求 | 响应 |
|---|---|---|
| `CreateFeed` | `CreateFeedReq` | `CreateFeedResp` |
| `GetFeed` | `GetFeedReq` | `GetFeedResp` |
| `ListFeeds` | `ListFeedsReq` | `ListFeedsResp` |
| `DeleteFeed` | `DeleteFeedReq` | `DeleteFeedResp` |
| `LikeFeed` | `LikeFeedReq` | `LikeFeedResp` |

禁止出现 `Request` / `Response` 全拼，统一用 `Req` / `Resp` 缩写。

### 4.2 业务领域消息命名

除请求/响应外，可定义领域消息供多处复用：

```protobuf
message FeedInfo { ... }      // 帖子详情
message FeedBrief { ... }     // 帖子卡片（列表用）
message FeedStats { ... }     // 帖子计数（点赞/评论/收藏）
message Author { ... }        // 作者信息（如果内部服务需要复用）
```

领域消息名使用大驼峰，语义清晰，避免缩写。

---

## 5. 字段定义规范

### 5.1 字段编号规则

- 字段编号从 1 开始连续递增，不跳号。
- 编号 1~15 留给最常用字段，编码更省空间。
- 已废弃字段**不要复用**编号，保留注释说明。

### 5.2 字段类型选择

| 业务类型 | proto 类型 | 说明 |
|---|---|---|
| 主键 ID、用户 ID、时间戳 | `int64` | 统一用 int64，兼容 Snowflake ID 和 Unix 毫秒时间戳 |
| 状态码、枚举值 | `int32` | 枚举值本身较小 |
| 计数 | `int64` | 例如点赞数、粉丝数，可能很大 |
| 布尔状态 | `bool` | 例如 `is_top`、`is_deleted` |
| 字符串 | `string` | 用户名、昵称、URL、JSON 串等 |
| 数组 | `repeated T` | 例如 `repeated int64 user_ids` |
| 嵌套对象 | `MessageType` | 例如 `FeedInfo author` |

### 5.3 时间戳字段

- 内部 RPC 时间戳统一用 **毫秒级 Unix 时间戳**（int64）。
- 字段名统一为 `created_at`、`updated_at` 等。
- 禁止在 proto 里使用字符串时间格式。

### 5.4 字段命名

proto 字段使用 **小写下划线**（snake_case）：

```protobuf
message GetFeedReq {
    int64 feed_id = 1;
    int64 user_id = 2;   // 当前调用者，用于权限/关注状态判断
}
```

goctl 生成 Go 结构体后会自动转成大驼峰，例如 `FeedId`、`UserId`。

### 5.5 可选/必填

proto3 默认所有字段都是可选（无 `hasField`），因此：

- 字段含义必须清晰，让调用方知道哪些必须传。
- 使用 0 值表示"未设置"时，必须在注释里说明。
- 对于更新类接口（如 `UpdateFeedReq`），用**空字符串 / 0 表示不更新**。

```protobuf
message UpdateFeedReq {
    int64 feed_id = 1;
    string description = 2;  // 空字符串表示不更新
    string cover_url = 3;    // 空字符串表示不更新
}
```

---

## 6. Service 与 RPC 命名规范

### 6.1 service 名

```protobuf
service Feed { ... }
```

- 大驼峰，与模块名一致，单数形式。

### 6.2 rpc 方法名

采用 **动词 + 名词** 的大驼峰形式：

| 动作 | 命名示例 | 说明 |
|---|---|---|
| 创建 | `CreateFeed` | 创建帖子 |
| 查询单个 | `GetFeed` | 查询帖子详情 |
| 查询列表 | `ListFeeds` / `GetFeeds` | 列表类查询，用 `List` 或 `Get` + 复数 |
| 更新 | `UpdateFeed` | 更新帖子 |
| 删除 | `DeleteFeed` | 删除帖子 |
| 动作类 | `LikeFeed`、`CollectFeed` | 点赞/收藏等操作 |

禁止出现动词不规范的命名，例如 `FeedCreate`、`FeedInfoGet`。

### 6.3 批量接口命名

批量接口统一以 `Batch` 开头：

```protobuf
rpc BatchGetFeeds(BatchGetFeedsReq) returns (BatchGetFeedsResp);
rpc BatchGetStats(BatchGetStatsReq) returns (BatchGetStatsResp);
```

### 6.4 单接口方法顺序

一个 service 内建议按以下顺序组织 rpc 方法：

1. 创建类（Create）
2. 查询类（Get / List / BatchGet）
3. 更新类（Update）
4. 删除类（Delete）
5. 动作类（Like / Follow / Comment）
6. 统计类（GetStats / BatchGetStats）

---

## 7. 注释规范

### 7.1 文件头注释

每个 proto 文件开头必须包含注释，说明：

- 文件职责
- 所属服务
- 主要接口列表

```protobuf
// feed.proto
//
// 职责：Feed 服务内部 gRPC 接口定义。
// 服务范围：帖子发布、帖子查询、推荐/关注/同城三种流、作者信息填充。
// 主要接口：CreateFeed, GetFeed, ListFeeds, DeleteFeed, BatchGetFeeds 等。
syntax = "proto3";

package feed;
option go_package = "./feed";
```

### 7.2 消息/字段注释

消息和字段必须加注释，说明业务含义、取值范围、特殊约定：

```protobuf
// FeedInfo 帖子完整信息
message FeedInfo {
    int64  feed_id     = 1;  // 帖子 ID，Snowflake 全局唯一
    int64  author_id   = 2;  // 作者用户 ID
    string description = 3;  // 帖子描述/文案
    int32  feed_type   = 4;  // 帖子类型：1=图文，2=视频
    int64  created_at  = 5;  // 发布时间，毫秒级 Unix 时间戳
}
```

### 7.3 枚举注释

枚举必须定义注释，并说明每个值的含义：

```protobuf
// FeedType 帖子类型
enum FeedType {
    FEED_TYPE_UNKNOWN = 0;  // 未知，proto3 默认值
    FEED_TYPE_IMAGE   = 1;  // 图文
    FEED_TYPE_VIDEO   = 2;  // 视频
}
```

- 枚举第一个值必须定义 `UNKNOWN = 0`，这是 proto3 的默认值要求。
- 枚举值名使用 `FEED_TYPE_XXX` 的大写下划线前缀，避免全局冲突。

---

## 8. 错误处理

### 8.1 错误码在 errorx 中定义

proto 层面不定义错误码，错误码统一在 `common/errorx/errorx.go` 中按服务分段维护：

| 码段 | 服务 |
|---|---|
| 10000~10999 | User |
| 11000~11999 | Relation |
| 12000~12999 | Feed |
| 13000~13999 | Comment |
| 14000~14999 | Interaction |

### 8.2 错误提示

logic 层返回错误时，使用 `errorx.New(code)` 或 `errorx.NewWithMsg(code, "msg")`。

---

## 9. 生成规则

### 9.1 生成命令

```bash
goctl rpc protoc api/proto/feed/feed.proto \
  --go_out=app/feed/rpc \
  --go-grpc_out=app/feed/rpc \
  --zrpc_out=app/feed/rpc \
  --home /root/.go-zero
```

- 生成目录固定为 `app/{service}/rpc/`，与 User/Relation 保持一致。
- 生成后的 `.pb.go` 文件**禁止手动修改**。

### 9.2 生成后扩展点

goctl 生成后，以下文件需要手动补充：

| 文件 | 说明 |
|---|---|
| `internal/config/config.go` | 补充 Mysql / CacheRedis / 业务配置 |
| `internal/svc/servicecontext.go` | 补充依赖注入（Redis / Model / IdGen） |
| `internal/logic/*.go` | 实现业务逻辑 |
| `etc/{service}.yaml` | 补充运行时配置 |
| `model/` | 用 goctl model 生成 SQL model |

---

## 10. 检查清单

新增或修改 proto 文件后，必须检查：

- [ ] 文件路径为 `api/proto/{service}/{service}.proto`
- [ ] 包含 `syntax = "proto3"`、正确的 `package` 和 `go_package`
- [ ] 不引入 `google/api/http.proto`，不定义 HTTP 注解
- [ ] 请求/响应消息命名与 rpc 方法一一对应，使用 `Req`/`Resp` 缩写
- [ ] 字段使用 snake_case，ID 和时间戳用 int64
- [ ] 枚举第一个值为 `UNKNOWN = 0`，枚举名使用服务前缀
- [ ] 消息和字段都有业务含义注释
- [ ] rpc 方法顺序合理，批量接口以 `Batch` 开头
- [ ] 生成后 `.pb.go` 不手动修改

---

## 11. 示例：feed.proto 骨架

```protobuf
// feed.proto
//
// 职责：Feed 服务内部 gRPC 接口定义。
// 服务范围：帖子发布、帖子详情、推荐/关注/同城三种流、批量查询。
syntax = "proto3";

package feed;
option go_package = "./feed";

// FeedType 帖子类型
enum FeedType {
    FEED_TYPE_UNKNOWN = 0;
    FEED_TYPE_IMAGE   = 1;  // 图文
    FEED_TYPE_VIDEO   = 2;  // 视频
}

// FeedInfo 帖子完整信息
message FeedInfo {
    int64  feed_id      = 1;  // 帖子 ID
    int64  author_id    = 2;  // 作者用户 ID
    string description  = 3;  // 帖子描述
    int32  feed_type    = 4;  // 帖子类型
    string cover_url    = 5;  // 封面图 URL
    string video_url    = 6;  // 视频 URL，图文为空
    string city_code    = 7;  // 发布城市代码
    string city_name    = 8;  // 发布城市名称
    int64  created_at   = 9;  // 发布时间，毫秒级 Unix
}

// FeedBrief 帖子卡片（列表用）
message FeedBrief {
    int64  feed_id     = 1;
    int64  author_id   = 2;
    string cover_url   = 3;
    int32  feed_type   = 4;
    int64  created_at  = 5;
}

// ---- 创建 ----
message CreateFeedReq {
    int64  author_id   = 1;
    string description = 2;
    int32  feed_type   = 3;
    string cover_url   = 4;
    string video_url   = 5;
    string city_code   = 6;
    string city_name   = 7;
}
message CreateFeedResp {
    FeedInfo feed = 1;
}

// ---- 获取详情 ----
message GetFeedReq {
    int64 feed_id = 1;
    int64 user_id = 2;  // 当前查看者，用于判断权限/关注状态
}
message GetFeedResp {
    FeedInfo feed = 1;
}

// ---- 列表 ----
message ListFeedsReq {
    int64 user_id  = 1;
    int64 page     = 2;
    int64 page_size = 3;
}
message ListFeedsResp {
    repeated FeedBrief feeds = 1;
    int64 total = 2;
}

// ==================== 服务定义 ====================
service Feed {
    rpc CreateFeed(CreateFeedReq) returns (CreateFeedResp);
    rpc GetFeed(GetFeedReq) returns (GetFeedResp);
    rpc ListFeeds(ListFeedsReq) returns (ListFeedsResp);
}
```

---

## 12. 与现有文档的关系

- `../design/api-spec/README.md` 是 REST 接口通用约定。
- `../design/service-design.md` 和 `../design/data-model.md` 是业务设计依据。
- 本规范只约束 `.proto` 文件，不与 `.api` 文件或 REST 文档冲突。
