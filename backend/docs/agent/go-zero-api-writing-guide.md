# go-zero API 文件编写规范

> 本文档约束 Feed 项目对外 HTTP 网关的 `.api` 文件编写规范。
> `.api` 文件是 go-zero 框架特有的 REST 接口定义语法，通过 `goctl api go` 生成
> HTTP 网关代码。它与 `.proto` 文件配合使用：proto 负责内部 gRPC，api 文件负责
> 对外 REST。

---

## 1. 定位与文件组织

### 1.1 定位

| 文件 | 用途 | 生成产物 |
|---|---|---|
| `.proto` | 内部 gRPC 服务间通信 | `*_grpc.pb.go`、`*.pb.go` |
| `.api` | 对外 HTTP REST 接口 | `gateway` 的 handler/routes/types |
| `api-spec/*.md` | 接口文档（人类可读） | 无代码生成，用于阅读 |

**核心原则**：`.api` 文件只定义网关层可见的 HTTP 接口，不直接写业务逻辑。handler 里调用内部 RPC Client，把请求转发给对应微服务。

### 1.2 目录结构

```
app/gateway/api/
├── gateway.api          # 入口文件，聚合各模块
├── user.api             # 用户模块接口
├── relation.api         # 关注关系模块接口
├── feed.api             # Feed 流模块接口
├── comment.api          # 评论模块接口
└── interaction.api      # 互动模块接口（点赞/收藏）
```

入口文件 `gateway.api` 使用 `import` 引入其他模块：

```api
import (
    "user.api"
    "relation.api"
    "feed.api"
    "comment.api"
    "interaction.api"
)
```

### 1.3 文件名

- 小写英文，模块名命名，如 `feed.api`。
- 一个模块一个文件，不允许合并。

---

## 2. 文件头部规范

每个 `.api` 文件开头必须包含文件职责注释：

```api
// feed.api
//
// 职责：Feed 模块对外 HTTP REST 接口定义，由 goctl 生成网关 handler。
// 说明：handler 内部通过 RPC Client 调用 Feed 服务，网关层做聚合/鉴权/适配。
```

---

## 3. 类型定义（type）

### 3.1 请求/响应结构体命名

采用与 REST 接口对应的命名：

| 接口 | 请求类型 | 响应类型 |
|---|---|---|
| `POST /api/v1/feeds` | `CreateFeedReq` | `CreateFeedResp` |
| `GET /api/v1/feeds/:feedId` | `GetFeedReq` | `GetFeedResp` |
| `GET /api/v1/feeds` | `ListFeedsReq` | `ListFeedsResp` |

### 3.2 结构体字段标签

字段必须加 `json` tag，与 REST 文档中的 JSON 字段名一致（snake_case）：

```api
type CreateFeedReq {
    Description string `json:"description"`  // 帖子描述
    FeedType    int32  `json:"feed_type"`    // 1=图文，2=视频
    CoverUrl    string `json:"cover_url"`    // 封面 URL
    VideoUrl    string `json:"video_url"`    // 视频 URL，图文可不传
    CityCode    string `json:"city_code"`    // 城市代码
    CityName    string `json:"city_name"`    // 城市名称
}
```

### 3.3 路径参数与 Query 参数

- 路径参数用 `path` tag：

```api
type GetFeedReq {
    FeedId int64 `path:"feedId"`  // 路径 /api/v1/feeds/:feedId
}
```

- Query 参数用 `form` tag：

```api
type ListFeedsReq {
    Page     int64 `form:"page,default=1"`
    PageSize int64 `form:"page_size,default=20"`
}
```

### 3.4 验证标签

go-zero 的 `.api` 语法支持 `validate` 标签：

```api
type CreateFeedReq {
    Description string `json:"description" validate:"lte=2000"`  // 最多 2000 字
    FeedType    int32  `json:"feed_type" validate:"oneof=1 2"`   // 只允许 1 或 2
}
```

常用验证规则：

| 规则 | 说明 | 示例 |
|---|---|---|
| `required` | 必填 | `validate:"required"` |
| `gte=1` | 大于等于 1 | 分页页码 |
| `lte=2000` | 小于等于 2000 | 文案长度 |
| `oneof=1 2` | 枚举值 | 帖子类型 |
| `min=1` | 字符串最小长度 | 用户名 |

### 3.5 响应结构体

响应结构体定义网关返回的 `data` 字段内容：

```api
type CreateFeedResp {
    FeedId    int64  `json:"feed_id"`
    CreatedAt int64  `json:"created_at"`
}
```

统一响应结构通常在 `common/response/response.go` 中封装，api 文件里不定义外层 `code`/`message`。

---

## 4. 接口路由定义（service）

### 4.1 service 块

一个 `.api` 文件包含一个 `service` 块，定义该模块的所有 HTTP 路由：

```api
@server(
    jwt: Auth            // 声明该 service 需要 JWT 鉴权（按需）
    prefix: /api/v1     // 前缀
)
service feed-api {
    @handler createFeed
    post /feeds (CreateFeedReq) returns (CreateFeedResp)

    @handler getFeed
    get /feeds/:feedId (GetFeedReq) returns (GetFeedResp)

    @handler listFeeds
    get /feeds (ListFeedsReq) returns (ListFeedsResp)
}
```

### 4.2 方法语义

| HTTP 方法 | 使用场景 | 示例 |
|---|---|---|
| GET | 查询 | `GET /feeds/:feedId` |
| POST | 创建 / 复杂动作 | `POST /feeds` |
| PUT | 全量更新 | `PUT /feeds/:feedId` |
| DELETE | 删除 | `DELETE /feeds/:feedId` |

### 4.3 路径设计

遵循 REST 资源命名，与 `api-spec/*.md` 保持一致：

- 资源用名词复数：`/feeds`、`/users`、`/comments`。
- 具体资源用路径参数：`/feeds/:feedId`。
- 当前登录用户资源用 `me`：`/users/me/feeds`。
- 动作类接口可用动词，但尽量通过 POST + 路径表达：
  - `POST /relations/follow` 关注
  - `POST /feeds/:feedId/like` 点赞
  - `POST /feeds/:feedId/collect` 收藏

### 4.4 鉴权声明

按接口粒度声明是否需要登录：

```api
// 不需要登录的接口单独放一个 service
@server(
    prefix: /api/v1
)
service feed-api-public {
    @handler listPublicFeeds
    get /feeds/public (ListPublicFeedsReq) returns (ListFeedsResp)
}

// 需要登录的接口放另一个 service
@server(
    jwt: Auth
    prefix: /api/v1
)
service feed-api {
    @handler createFeed
    post /feeds (CreateFeedReq) returns (CreateFeedResp)
}
```

- 需要登录的 service 加 `jwt: Auth`。
- 不需要登录的 service 不加 `jwt`。
- 同一模块如果既有公开接口又有需登录接口，拆成两个 `service` 块。

### 4.5 handler 命名

handler 函数名使用小驼峰：

```api
@handler createFeed
@handler getFeed
@handler listFeeds
@handler deleteFeed
@handler likeFeed
```

handler 名应该能一眼看出对应接口，避免缩写。

---

## 5. 与内部 RPC 的对应关系

### 5.1 网关 handler 调用 RPC Client

`.api` 文件只定义接口形状，业务逻辑写在生成的 handler 中。handler 模板如下：

```go
// createFeedHandler.go（goctl 生成后需手动填充）
func createFeedHandler(ctx *svc.ServiceContext) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        var req types.CreateFeedReq
        if err := httpx.Parse(r, &req); err != nil {
            httpx.Error(w, err)
            return
        }

        l := logic.NewCreateFeedLogic(r.Context(), ctx)
        resp, err := l.CreateFeed(&req)
        if err != nil {
            httpx.Error(w, err)
        } else {
            httpx.OkJson(w, resp)
        }
    }
}
```

### 5.2 多服务聚合

网关如果需要聚合多个下游服务，在 logic 层完成：

```go
// gateway logic 示例
func (l *GetFeedLogic) GetFeed(req *types.GetFeedReq) (*types.GetFeedResp, error) {
    // 1. 调用 Feed 服务获取帖子
    feedResp, err := l.svcCtx.FeedClient.GetFeed(...)
    // 2. 调用 User 服务批量获取作者信息
    userResp, err := l.svcCtx.UserClient.BatchGetUsers(...)
    // 3. 调用 Interaction 服务获取点赞/收藏状态
    statResp, err := l.svcCtx.InteractionClient.BatchGetStats(...)
    // 4. 组装返回
}
```

`.api` 文件本身不体现聚合逻辑，只定义对外的 HTTP 接口形态。

---

## 6. 生成命令

在项目根目录执行：

```bash
goctl api go -api app/gateway/api/gateway.api -dir app/gateway
```

生成产物包括：

```
app/gateway/
├── etc/gateway.yaml
├── gateway.go
├── internal/
│   ├── config/
│   ├── handler/         # HTTP handler
│   ├── logic/           # 业务逻辑入口
│   ├── svc/             # ServiceContext
│   └── types/           # 请求/响应类型
└── ...
```

生成后的 `handler` 和 `types` 目录**禁止手动修改**，只允许在 `logic/` 中补充业务逻辑。

---

## 7. 检查清单

新增或修改 `.api` 文件后，必须检查：

- [ ] 文件路径为 `app/gateway/api/{module}.api`
- [ ] 文件头有职责说明注释
- [ ] 入口 `gateway.api` 已 `import` 该模块
- [ ] 类型定义字段都有 `json` tag，且使用 snake_case
- [ ] 路径参数用 `path` tag，Query 参数用 `form` tag
- [ ] 必填字段使用 `validate:"required"` 等验证规则
- [ ] 路由路径与 `docs/design/api-spec/*.md` 保持一致
- [ ] service 块按是否需要登录拆分，加 `jwt: Auth` 或取消
- [ ] handler 命名使用小驼峰，语义清晰
- [ ] 生成代码不手动修改 `handler/` 和 `types/`

---

## 8. 示例：feed.api 骨架

```api
// feed.api
//
// 职责：Feed 模块对外 HTTP REST 接口定义，由 goctl 生成网关 handler。

// 类型定义
type CreateFeedReq {
    Description string `json:"description" validate:"required,lte=2000"`
    FeedType    int32  `json:"feed_type" validate:"required,oneof=1 2"`
    CoverUrl    string `json:"cover_url" validate:"required"`
    VideoUrl    string `json:"video_url"`
    CityCode    string `json:"city_code"`
    CityName    string `json:"city_name"`
}

type CreateFeedResp {
    FeedId    int64 `json:"feed_id"`
    CreatedAt int64 `json:"created_at"`
}

type GetFeedReq {
    FeedId int64 `path:"feedId"`
}

type GetFeedResp {
    FeedId      int64  `json:"feed_id"`
    AuthorId    int64  `json:"author_id"`
    Description string `json:"description"`
    FeedType    int32  `json:"feed_type"`
    CoverUrl    string `json:"cover_url"`
    VideoUrl    string `json:"video_url"`
    CityCode    string `json:"city_code"`
    CityName    string `json:"city_name"`
    CreatedAt   int64  `json:"created_at"`
}

type ListFeedsReq {
    Page     int64 `form:"page,default=1" validate:"gte=1"`
    PageSize int64 `form:"page_size,default=20" validate:"gte=1,lte=50"`
}

type ListFeedsResp {
    List      []FeedBrief `json:"list"`
    Page      int64       `json:"page"`
    PageSize  int64       `json:"page_size"`
    Total     int64       `json:"total"`
    HasMore   bool        `json:"has_more"`
}

type FeedBrief {
    FeedId    int64  `json:"feed_id"`
    AuthorId  int64  `json:"author_id"`
    CoverUrl  string `json:"cover_url"`
    FeedType  int32  `json:"feed_type"`
    CreatedAt int64  `json:"created_at"`
}

// 公开接口（不需要登录）
@server(
    prefix: /api/v1
)
service feed-api-public {
    @handler listFeeds
    get /feeds (ListFeedsReq) returns (ListFeedsResp)

    @handler getFeed
    get /feeds/:feedId (GetFeedReq) returns (GetFeedResp)
}

// 需登录接口
@server(
    jwt: Auth
    prefix: /api/v1
)
service feed-api {
    @handler createFeed
    post /feeds (CreateFeedReq) returns (CreateFeedResp)
}
```

---

## 9. 与现有文档的关系

- `../design/api-spec/*.md` 是接口定义源文档，`.api` 文件必须与之一致。
- `proto-writing-guide.md` 约束内部 gRPC 接口定义，`.api` 文件通过 handler 调用这些 gRPC 接口。
- `api-writing-guide.md` 约束 Markdown 接口文档的写法，本规范约束 `.api` 代码文件的写法。
- 三份文档若有冲突，以 `.api` 文件的实际生成为准，并同步修正文档。
