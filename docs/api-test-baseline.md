# API 接口测试基线文档

## 文档信息

| 项 | 内容 |
|---|---|
| 文档名称 | API 接口测试基线文档 |
| 生成日期 | 2026-07-29 |
| 分析对象 | 当前仓库 `main` 分支工作区代码（以实际代码行为为最终依据） |
| 技术栈 | Go 1.21 + go-zero 1.7.3；MySQL 8.0（sqlx）；Redis 7（go-redis/v9 + go-zero redis）；Apache RocketMQ（rocketmq-client-go/v2）；gRPC + Protobuf；JWT（golang-jwt/v5）；Snowflake + UUID；测试：Testify / GoMock / SQLMock / Miniredis |
| 统计口径 | HTTP API 28 个；gRPC 方法 36 个（User 5 / Relation 6 / Feed 8 / Comment 7 / Interaction 10）；RocketMQ Topic 4 个（逻辑事件 8 类）；现有测试文件 34 个 |
| 约定 | 关键判断均标注 `代码依据：相对路径:行号` 或 `相对路径，函数名`；代码无法确认的信息标注 `待确认` |

## 系统架构与测试范围

### 系统拓扑

- **HTTP Gateway**（`app/gateway`，端口 8080，路由前缀 `/api/v1`）：BFF 层，负责 JWT 认证、参数解析、RPC 编排与 FeedCard 聚合。代码依据：`app/gateway/etc/gateway.yaml:2-3`、`app/gateway/internal/handler/routes.go:32`。
- **User RPC**（9001）、**Relation RPC**（9002）、**Feed RPC**（9003）、**Comment RPC**（9004）、**Interaction RPC**（9005），经 etcd（`127.0.0.1:2479`）注册发现。代码依据：各服务 `etc/*.yaml`。
- Gateway 初始化 5 个 RPC Client：UserRpc、RelationRpc、FeedRpc、CommentRpc、InteractionRpc。代码依据：`app/gateway/internal/svc/serviceContext.go:41-45`。
- 各 RPC 服务依赖：
  - User：MySQL(users) + Redis + jwtx.Manager + bcrypt 池，**无 MQ**。代码依据：`app/user/rpc/internal/svc/serviceContext.go:46-62`。
  - Relation：MySQL(relations) + Redis + idgen，**无 MQ**。代码依据：`app/relation/rpc/internal/svc/servicecontext.go`。
  - Feed：MySQL(feeds) + Redis + RocketMQ Producer/Consumer + RelationRpc 客户端。代码依据：`app/feed/rpc/internal/svc/servicecontext.go:45`、`app/feed/rpc/etc/feed.yaml:31-32`。
  - Comment：MySQL(comments) + Redis + RocketMQ Producer + UserRpc/FeedRpc 客户端。代码依据：`app/comment/rpc/internal/svc/serviceContext.go:24-45`。
  - Interaction：MySQL(likes/collections) + Redis + RocketMQ Producer/Consumer + idgen。代码依据：`app/interaction/rpc/internal/svc/serviceContext.go:34-56`。
- 部署组件（docker-compose）：MySQL 8.0 单实例（**无主从**）、Redis 7、etcd 3.5（宿主机 2479）、RocketMQ namesrv 9876 + broker（10909/10911/10912，`autoCreateTopicEnable=true`）+ dashboard 9877。代码依据：`deploy/docker-compose.yaml`、`deploy/broker.conf`。

### 三类系统入口（测试边界）

1. **外部 HTTP API**：定义于 `app/gateway/api/*.api`，共 28 个，经 Gateway Handler/Logic 转 RPC。
2. **内部 gRPC API**：定义于 `api/proto/**/*.proto`，共 36 个方法。RPC 服务**不做任何调用方身份认证**，完全信任入参 user_id（见风险 R-P0-1）。
3. **异步消息入口**：RocketMQ 4 个 Topic（`feed-created`、`feed-deleted`、`comment-event`、`interaction-event`），均无 Tag 过滤，消费方为 Feed Worker 与 Interaction Worker。代码依据：`common/mq/consumer.go:30`（`MessageSelector{}` 空选择器）。

## HTTP API 总览

路由前缀统一 `/api/v1`。JWT 通过 go-zero 内置 `rest.WithJwt(Auth.AccessSecret)` 注册，仅 register/login 免认证。代码依据：`app/gateway/internal/handler/routes.go:32-197`。实现状态基于对应 Logic 是否存在真实 RPC 调用判定。

| # | 模块 | 接口 | 方法 | 路径 | Handler | Gateway Logic | 调用 RPC | JWT | 实现状态 |
|---|---|---|---|---|---|---|---|---|---|
| 1 | user | 注册 | POST | /api/v1/users/register | registerHandler.go | logic/registerLogic.go | User.Register | 否 | 已实现 |
| 2 | user | 登录 | POST | /api/v1/users/login | loginHandler.go | logic/loginLogic.go | User.Login | 否 | 已实现 |
| 3 | user | 上传凭证 | POST | /api/v1/upload/token | uploadTokenHandler.go | logic/uploadTokenLogic.go | 无 | 是 | **占位**（恒返回 10006，`uploadTokenLogic.go:27-29`） |
| 4 | user | 查看他人主页 | GET | /api/v1/users/:userId | getUserHandler.go | logic/getUserLogic.go | User.GetUser + Relation.GetFollows/GetFans/IsFollow | 是 | 已实现 |
| 5 | user | 查看本人信息 | GET | /api/v1/users/me | getMeHandler.go | logic/getMeLogic.go | User.GetUser | 是 | 已实现 |
| 6 | user | 更新本人资料 | PATCH | /api/v1/users/me | updateMeHandler.go | logic/updateMeLogic.go | User.UpdateUser | 是 | 已实现 |
| 7 | relation | 关注 | POST | /api/v1/relations/follow | relation/followHandler.go | logic/relation/followLogic.go | Relation.Follow (+GetFans) | 是 | 已实现 |
| 8 | relation | 取关 | DELETE | /api/v1/relations/follow | relation/unfollowHandler.go | logic/relation/unfollowLogic.go | Relation.Unfollow (+GetFans) | 是 | 已实现 |
| 9 | relation | 关注列表 | GET | /api/v1/relations/following | relation/followingListHandler.go | logic/relation/followingListLogic.go | Relation.GetFollows + User.BatchGetUsers + Relation.IsFollow | 是 | 已实现 |
| 10 | relation | 粉丝列表 | GET | /api/v1/relations/followers | relation/followerListHandler.go | logic/relation/followerListLogic.go | Relation.GetFans + User.BatchGetUsers + Relation.IsFollow | 是 | 已实现 |
| 11 | relation | 是否关注 | GET | /api/v1/relations/is-following | relation/isFollowingHandler.go | logic/relation/isFollowingLogic.go | Relation.IsFollow | 是 | 已实现 |
| 12 | feed | 发布帖子 | POST | /api/v1/feeds | feed/createFeedHandler.go | logic/feed/createFeedLogic.go | Feed.CreateFeed | 是 | 已实现 |
| 13 | feed | 删除帖子 | DELETE | /api/v1/feeds/:feedId | feed/deleteFeedHandler.go | logic/feed/deleteFeedLogic.go | Feed.GetFeed + Feed.DeleteFeed | 是 | 已实现 |
| 14 | feed | 帖子详情 | GET | /api/v1/feeds/:feedId | feed/getFeedDetailHandler.go | logic/feed/getFeedDetailLogic.go | Feed.GetFeed + User.GetUser + Relation.IsFollow + Interaction.GetFeedStats/GetUserInteractionStatus | 是 | 已实现 |
| 15 | feed | 时间线 | GET | /api/v1/feeds/timeline | feed/timelineHandler.go | logic/feed/timelineLogic.go | Feed.GetRecommend/GetFollow/GetCityTimeline + 聚合 RPC | 是 | 已实现 |
| 16 | feed | 用户帖子列表 | GET | /api/v1/users/:userId/feeds | feed/userFeedsHandler.go | logic/feed/userFeedsLogic.go | Feed.GetUserFeeds + 聚合 RPC | 是 | 已实现 |
| 17 | comment | 发表评论 | POST | /api/v1/feeds/:feedId/comments | comment/createCommentHandler.go | logic/comment/createCommentLogic.go | Comment.CreateComment (+User.GetUser 补昵称) | 是 | 已实现 |
| 18 | comment | 评论列表 | GET | /api/v1/feeds/:feedId/comments | comment/listCommentsHandler.go | logic/comment/listCommentsLogic.go | Comment.ListComments + Comment.GetHotComments（并行） | 是 | 已实现 |
| 19 | comment | 回复列表 | GET | /api/v1/comments/:rootId/replies | comment/listRepliesHandler.go | logic/comment/listRepliesLogic.go | Comment.ListReplies | 是 | 已实现 |
| 20 | comment | 删除评论 | DELETE | /api/v1/comments/:commentId | comment/deleteCommentHandler.go | logic/comment/deleteCommentLogic.go | Comment.DeleteComment | 是 | 已实现 |
| 21 | comment | 评论点赞 | POST | /api/v1/comments/:commentId/like | comment/likeCommentHandler.go | logic/comment/likeCommentLogic.go | 无 | 是 | **占位**（恒返回 ServerError"评论点赞功能暂未开放"，`likeCommentLogic.go:35`） |
| 22 | comment | 取消评论点赞 | DELETE | /api/v1/comments/:commentId/like | comment/unlikeCommentHandler.go | logic/comment/unlikeCommentLogic.go | 无 | 是 | **占位**（同上，`unlikeCommentLogic.go:33`） |
| 23 | interaction | 点赞 | POST | /api/v1/feeds/:feedId/like | interaction/likeFeedHandler.go | logic/interaction/likeFeedLogic.go | Interaction.LikeFeed (+GetFeedStats) | 是 | 已实现 |
| 24 | interaction | 取消点赞 | DELETE | /api/v1/feeds/:feedId/like | interaction/unlikeFeedHandler.go | logic/interaction/unlikeFeedLogic.go | Interaction.UnlikeFeed (+GetFeedStats) | 是 | 已实现 |
| 25 | interaction | 收藏 | POST | /api/v1/feeds/:feedId/collect | interaction/collectFeedHandler.go | logic/interaction/collectFeedLogic.go | Interaction.CollectFeed (+GetFeedStats) | 是 | 已实现 |
| 26 | interaction | 取消收藏 | DELETE | /api/v1/feeds/:feedId/collect | interaction/uncollectFeedHandler.go | logic/interaction/uncollectFeedLogic.go | Interaction.UncollectFeed (+GetFeedStats) | 是 | 已实现 |
| 27 | interaction | 我的点赞 | GET | /api/v1/users/me/likes | interaction/myLikesHandler.go | logic/interaction/myLikesLogic.go | Interaction.GetUserLikedFeeds + Feed.BatchGetFeeds + 聚合 RPC | 是 | 已实现 |
| 28 | interaction | 我的收藏 | GET | /api/v1/users/me/collects | interaction/myCollectsHandler.go | logic/interaction/myCollectsLogic.go | Interaction.GetUserCollectedFeeds + Feed.BatchGetFeeds + 聚合 RPC | 是 | 已实现 |

> Handler 路径省略公共前缀 `app/gateway/internal/handler/`，Logic 路径省略 `app/gateway/internal/`。

## gRPC API 总览

| 服务 | RPC 方法 | 请求 / 响应 | Logic 文件 | 主要调用方 | 依赖组件 |
|---|---|---|---|---|---|
| User(9001) | Register | RegisterReq / RegisterResp | registerLogic.go | Gateway | MySQL(users)、bcrypt、jwtx |
| User | Login | LoginReq / LoginResp | loginLogic.go | Gateway | MySQL、bcrypt、jwtx |
| User | GetUser | GetUserReq / GetUserResp | getUserLogic.go | Gateway、Comment（fillUserInfos 不用此方法，用 BatchGetUsers） | MySQL（goctl 行缓存） |
| User | UpdateUser | UpdateUserReq / UpdateUserResp | updateUserLogic.go | Gateway | MySQL（goctl 缓存自动失效） |
| User | BatchGetUsers | BatchGetUsersReq / BatchGetUsersResp | batchGetUsersLogic.go | Gateway 聚合、Comment 服务 | MySQL、Redis(`user:brief:{id}` TTL 600s) |
| Relation(9002) | Follow | FollowReq / FollowResp | followLogic.go | Gateway | MySQL(relations 唯一索引)、Redis(ZSet/计数/vip Set) |
| Relation | Unfollow | UnfollowReq / UnfollowResp | unfollowLogic.go | Gateway | 同上 |
| Relation | GetFollows | GetFollowsReq / GetFollowsResp | getFollowsLogic.go | Gateway、Feed(关注流) | Redis ZSet + MySQL 回源 |
| Relation | GetFans | GetFansReq / GetFansResp | getFansLogic.go | Gateway、Feed Worker(fanout) | 同上 |
| Relation | IsFollow | IsFollowReq / IsFollowResp | isFollowLogic.go | Gateway | MySQL（逐条查询，N+1） |
| Relation | IsVip | IsVipReq / IsVipResp | isVipLogic.go | Feed(CreateFeed、关注流) | Redis(vip Set/计数) + MySQL 重建 |
| Feed(9003) | CreateFeed | CreateFeedReq / CreateFeedResp | createfeedlogic.go | Gateway | MySQL(feeds)、Relation.IsVip、RocketMQ(feed-created) |
| Feed | DeleteFeed | DeleteFeedReq / DeleteFeedResp | deletefeedlogic.go | Gateway | MySQL(软删)、Redis(Del 详情)、RocketMQ(feed-deleted) |
| Feed | GetFeed | GetFeedReq / GetFeedResp | getfeedlogic.go | Gateway、Comment(存在性校验) | Redis(`feed:{id}` Hash 30d) + MySQL |
| Feed | BatchGetFeeds | BatchGetFeedsReq / BatchGetFeedsResp | batchgetfeedslogic.go | Gateway(我的点赞/收藏) | MySQL（无缓存） |
| Feed | GetRecommendTimeline | GetRecommendTimelineReq / Resp | getrecommendtimelinelogic.go | Gateway | Redis(`feed:recommend` ZSet) + MySQL |
| Feed | GetFollowTimeline | GetFollowTimelineReq / Resp | getfollowtimelinelogic.go | Gateway | Redis(inbox/outbox) + Relation.GetFollows/IsVip + MySQL |
| Feed | GetCityTimeline | GetCityTimelineReq / Resp | getcitytimelinelogic.go | Gateway | Redis(`feed:city:{code}`) + MySQL 降级 |
| Feed | GetUserFeeds | GetUserFeedsReq / Resp | getuserfeedslogic.go | Gateway | MySQL（无缓存） |
| Comment(9004) | CreateComment | CreateCommentReq / Resp | createCommentLogic.go | Gateway | MySQL(comments 事务)、Feed.GetFeed、Redis 计数、RocketMQ(comment-event) |
| Comment | DeleteComment | DeleteCommentReq / Resp | deleteCommentLogic.go | Gateway | MySQL(软删)、Redis、RocketMQ(comment-event) |
| Comment | ListComments | ListCommentsReq / Resp | listCommentsLogic.go | Gateway | MySQL + Redis 计数 + User.BatchGetUsers |
| Comment | ListReplies | ListRepliesReq / Resp | listRepliesLogic.go | Gateway | MySQL(游标) + User.BatchGetUsers |
| Comment | GetCommentCount | GetCommentCountReq / Resp | getCommentCountLogic.go | （网关未直接调用） | Redis(`comment_count:{fid}` 1h) + MySQL |
| Comment | BatchGetCommentCount | BatchGetCommentCountReq / Resp | batchGetCommentCountLogic.go | （网关未直接调用，FeedCard 用镜像计数） | 同上 |
| Comment | GetHotComments | GetHotCommentsReq / Resp | getHotCommentsLogic.go | Gateway(评论列表第 1 页) | Redis(`comment_hot:{fid}` 5min) + MySQL |
| Interaction(9005) | LikeFeed | LikeFeedReq / LikeFeedResp | likeFeedLogic.go | Gateway | Redis Lua 先行 + RocketMQ(interaction-event) |
| Interaction | UnlikeFeed | UnlikeFeedReq / Resp | unlikeFeedLogic.go | Gateway | 同上 |
| Interaction | CollectFeed | CollectFeedReq / Resp | collectFeedLogic.go | Gateway | 同上 |
| Interaction | UncollectFeed | UncollectFeedReq / Resp | uncollectFeedLogic.go | Gateway | 同上 |
| Interaction | GetFeedStats | GetFeedStatsReq / Resp | getFeedStatsLogic.go | Gateway(详情/写后回查) | Redis(`feed:stats:{fid}` Hash 1h) + MySQL COUNT |
| Interaction | BatchGetFeedStats | BatchGetFeedStatsReq / Resp | （批量 stats logic） | Gateway(FeedCard 聚合) | 同上 |
| Interaction | GetUserInteractionStatus | Req / Resp | getUserInteractionStatusLogic.go | Gateway(详情) | Redis Set + MySQL 回源 |
| Interaction | BatchGetUserInteractionStatus | Req / Resp | （批量 status logic） | Gateway(FeedCard 聚合) | 同上 |
| Interaction | GetUserLikedFeeds | Req / Resp | getUserLikedFeedsLogic.go | Gateway(我的点赞) | Redis ZSet(游标) + MySQL 重建 |
| Interaction | GetUserCollectedFeeds | Req / Resp | （收藏列表 logic） | Gateway(我的收藏) | 同上 |

> proto 依据：`api/proto/user/user.proto:81-87`、`api/proto/relation/relation.proto:66-73`、`api/proto/feed/feed.proto:173`、`api/proto/comment/comment.proto:137-158`、`api/proto/interaction/interaction.proto:127-143`。

## RocketMQ 事件总览

所有事件 JSON 序列化、**无 Tag**（消费端 `MessageSelector{}` 全量订阅，代码依据：`common/mq/consumer.go:30`）。

| 事件 | Topic | Tag | 消息结构 | 生产服务 / 位置 | 消费服务 / 位置 | 数据副作用 |
|---|---|---|---|---|---|---|
| Feed 创建 | `feed-created` | 无 | `EventFeedCreate{event_id,event_type,feed_id,user_id,is_vip_feed,city_code,created_at}`（`common/event/feed/event.go:21-29`） | Feed RPC，`app/feed/rpc/internal/logic/createfeedlogic.go:102` | Feed Worker，`app/feed/rpc/internal/worker/worker.go:70,84` | 写 outbox/recommend/city ZSet；非大V 推粉丝 inbox |
| Feed 删除 | `feed-deleted` | 无 | `EventFeedDeleted{event_id,event_type,feed_id,user_id,is_vip_feed,city_code}`（`common/event/feed/event.go:43-50`） | Feed RPC，`deletefeedlogic.go:66` | Feed Worker，`worker.go:73,115` | ZREM recommend/city/outbox；非大V 清粉丝 inbox |
| 评论创建/删除 | `comment-event` | 无（action_type=CREATE/DELETE） | `Event{event_id,action_type,comment_id,feed_id,user_id,reply_user_id,parent_id,root_id,content_len,timestamp}`（`common/event/comment/event.go:36-47`） | Comment RPC，`app/comment/rpc/internal/logic/common.go:197` | Feed Worker，`worker.go:77,211` | `feeds.comment_count` 镜像列 ±1（SETNX 去重 24h） |
| 互动事件 | `interaction-event` | 无（action_type=1~4：Like/Unlike/Collect/Uncollect） | `Event{event_id,user_id,feed_id,action_type,timestamp}`（`common/event/interaction/event.go:29-35`） | Interaction RPC，`interactionHelper.go:448` | Interaction Worker，`app/interaction/rpc/internal/worker/worker.go:52` | likes/collections 表插入或状态翻转（唯一索引 + 时间戳防乱序） |

## 公共认证、错误码与响应规则

### HTTP 统一响应结构

```go
// 代码依据：common/response/response.go:25-30
Body{ Code int `json:"code"`, Message string `json:"message"`, Data interface{} `json:"data"`, RequestID string `json:"request_id"` }
```

- 成功：`response.Success` → HTTP 200，`code=0, message="success"`。代码依据：`common/response/response.go:48-55`。
- 业务失败：`response.Error` / `response.ErrorFrom` → **HTTP 仍为 200**（`httpx.OkJsonCtx`），业务结果由 `code` 区分。代码依据：`common/response/response.go:58-65`。
- `response.HTTPError` 是唯一可写非 200 状态码的入口，但当前 Handler 均未使用。代码依据：`common/response/response.go:81-90`。
- 非业务错误（RPC Internal、DB 错误等）经 `ErrorFrom`→`errorx.TryParse` 失败后统一返回 `code=1 ServerError`，原始错误只记日志不外泄。代码依据：`common/response/response.go:71-78`。
- `request_id` 从 `ctx.Value("request_id")` 提取，当前无中间件注入，**实际恒为空串**。代码依据：`common/response/response.go:34-45`。

### 业务错误码（common/errorx/errorx.go:41-92）

| 区段 | 错误码 |
|---|---|
| 通用 | 0 Success；1 ServerError；2 ParamError；3 Unauthorized；4 Forbidden；5 TooManyReq |
| User 10xxx | 10001 用户名已存在；10002 用户名或密码错误；10003 用户不存在；10004 密码格式不符（**代码中从未使用**）；10005 用户已禁用；10006 获取上传凭证失败 |
| Relation 11xxx | 11001 不能关注自己；11002 已经关注（**未使用**，重复关注返回成功）；11003 未关注（**未使用**）；11004 目标用户不存在（**未使用**） |
| Feed 12xxx | 12001 帖子不存在；12002 无权限操作该帖子；12003 内容为空；12004 媒体为空；12005 不支持的类型；12006 IP 定位失败 |
| Comment 13xxx | 13001 评论不存在；13002 帖子不存在；13003 无权限删除评论；13004 内容为空；13005 内容超长；13006 父评论不存在 |
| Interaction 14xxx | 14001 帖子不存在（**未使用**）；14002 操作过于频繁（**未使用**） |

### gRPC 错误转换规则

- 服务端：`errorx.ToGRPCError` 将 `CodeError` 序列化为 `codes.Unknown` + message=`[bizerror]{"code":N,"msg":"..."}`。**gRPC status code 恒为 Unknown，业务码只在 message 文本中**。代码依据：`common/errorx/grpc.go:16-22`。
- 转换由各服务 `ErrorInterceptor` 完成，user/relation/comment/interaction 四服务已注册（`user.go:38`、`relation.go:45`、`comment.go:47`、`interaction.go:59`）；**Feed 服务未注册**（`app/feed/rpc/feed.go` 全文无 `AddUnaryInterceptors`），Feed logic 返回的 `CodeError` 经 zrpc 默认包装为 `codes.Internal/Unknown` 但**无 `[bizerror]` 前缀**，Gateway `TryParse` 失败后一律映射为 `ServerError(1)`。见风险 R-P1-1。
- 网关侧：Handler 统一 `writeError`→`response.ErrorFrom`→`errorx.TryParse`（先类型断言 `*CodeError`，再 `FromGRPCError` 解析 `[bizerror]` 前缀）。代码依据：`app/gateway/internal/handler/helper.go:13`、`common/errorx/grpc.go:26-58`。

### JWT 认证规则

- 算法 HS256；Claims：`user_id`（**字符串形式的 int64**，`json:"user_id,string"`，防 JSON float64 精度丢失）、`username`、标准 exp/iat/nbf。代码依据：`common/jwtx/jwtx.go:15-22,50`。
- 签发：仅 User RPC 在 Register/Login 时签发，过期时间 = 配置 `expireHour`（user.yaml 720h）。代码依据：`app/user/rpc/internal/logic/registerLogic.go:79`、`loginLogic.go:59`。
- 校验：Gateway 用 go-zero 内置 `rest.WithJwt(AccessSecret)`；**JWT 缺失/无效/过期统一返回 HTTP 401，无响应体业务码**（go-zero 内置行为，未走 `response.Body`）。代码依据：`app/gateway/internal/handler/routes.go:58,95,127,164,196`。
- 用户 ID 读取：`middleware.MustGetUserID(ctx)` 兼容读取 JWT claim `"user_id"`（string/float64/int64/int 四种类型）。代码依据：`app/gateway/internal/middleware/auth.go:14-39`。
- secret：`gateway.yaml:55` 与 `user.yaml:33` 硬编码 `"feed-user-service-jwt-secret-CHANGE-ME"`，必须两侧一致；违反 secrets env-only 原则（见风险 R-P1-10）。

### 其他公共规则

| 规则 | 当前实际行为 | 代码依据 |
|---|---|---|
| ID 生成 | Snowflake（1+41+10+12 位），机器 ID 由 `Init(machineID)` 传入，未 Init 时兜底 =1；事件 event_id 用 UUID | `common/idgen/idgen.go:29-47` |
| ID 的 JSON 表示 | HTTP 响应中 ID 为 int64 数字（types.go 无 string tag）；JWT claim 中为字符串。**前端 JS 精度风险未统一处理** | `app/gateway/internal/types/types.go`、`common/jwtx/jwtx.go:17` |
| 时间字段 | RPC/HTTP 均为毫秒时间戳 int64（`created_at`）；Redis ZSet score 用秒级时间戳 | `api/proto/feed/feed.proto:32-50`、`worker.go:91` |
| 分页口径 **不一致** | timeline/comment/interaction 列表用 `aggregate.ClampPageSize`（默认 10 或 20，最大 50）；relation 列表用 `helper.clampPage`（最大 50）；RPC 侧 Comment 最大 100、Relation 最大 100、Feed 最大 50、BatchGetFeeds 上限 100 截断 | `aggregate/feedcard.go:213`、`relation/helper.go:90`、`app/comment/rpc/internal/logic/common.go:34-37` |
| 游标 | Gateway 页码型游标 `PageFromCursor/NextPageCursor`（recommend/city/userFeeds）；Feed 关注流 `base64(score:id)`；Comment 回复 `created_at+id` 组合游标；Interaction `base64(score:feed_id)` | `aggregate/feedcard.go:225-242`、`feed_follow.go:51`、`listRepliesLogic.go:45`、`interactionHelper.go:530` |
| 空数组 vs null | RPC/Gateway 均以 `make([]T,0,n)` 构造列表，序列化为 `[]` 非 null（如 `listRepliesLogic.go:74`）；未逐一验证全部接口，**待确认**个别接口是否返回 null |
| 资源不存在 | 读接口返回业务码（12001/13001/10003）+HTTP 200；BatchGetFeeds/BatchGetUsers 静默跳过不存在 ID | 各 logic |
| 重复操作 | 点赞/收藏/关注/取关重复操作**一律幂等返回成功**，不返回 11002/11003 等错误码 | `likeFeedLogic.go:44-47`、`followLogic.go:51` |
| 删除语义 | Feed 软删（status=2）、Comment 软删（status=2）、Like/Collect 软删（status=2 墓碑）、Relation **物理删除** | `feedsModel.go:166`、`commentsModel.go:142`、`interaction.sql:15`、`unfollowLogic.go` |
| 缓存删除失败 | 一律仅记日志不阻塞主流程（Feed 详情 Del、Relation ZSet、Comment 计数） | `feedsModel.go:173-178`、`followLogic.go:78-79` |
| Trace/日志 | logx.WithContext 携带 go-zero trace；`request_id` 响应字段未落地（恒空） | `common/response/response.go:34-45` |

## User 接口测试基线

### HTTP POST /api/v1/users/register（用户注册）

**基本信息**：模块 user；Content-Type application/json；Handler `app/gateway/internal/handler/registerHandler.go`，函数 `RegisterHandler`；Logic `app/gateway/internal/logic/registerLogic.go`，函数 `Register`；调用 RPC `User.Register`。

**认证与权限**：无需 JWT（`user.api` 免认证组）。无额外权限。

**请求参数**（Body，依据 `app/gateway/api/user.api` + `types.go` + RPC 校验）：

| 字段 | Go 类型 | 必填 | Gateway 校验 | RPC 二次校验 |
|---|---|---|---|---|
| username | string | 是 | 仅 httpx.Parse 结构绑定，**Gateway 无长度校验** | 非空、长度 2–32（`registerLogic.go:36-42`） |
| password | string | 是 | 无 | 非空、长度 6–64；**无强度/字符集校验**，10004 未使用 |
| nickname | string | 否 | 无 | 空则默认 = username；**无长度上限校验**（DB varchar(64)，超长报 DB 错误）契约缺口 |

**成功响应**：HTTP 200，`code=0`，data=`{token, expires_in(秒), user{user_id,username,nickname,avatar,bio,is_vip,fans_count,follow_count}}`。token 由 RPC 侧 jwtx 签发（720h）。代码依据：`registerLogic.go(gateway)`、`app/user/rpc/internal/logic/registerLogogic.go`→`registerLogic.go:60-90`。

**业务规则**：① RPC 校验参数 → ② `FindOneByUsername` 查重，存在返回 10001 → ③ bcrypt（cost=10，worker 池并发限制）加密 → ④ Snowflake 生成 user_id → ⑤ Insert users → ⑥ 生成 JWT → 返回。代码依据：`app/user/rpc/internal/logic/registerLogic.go:44-90`。

**数据副作用**：INSERT `users`（唯一索引 `uk_username`，`deploy/sql/user.sql`）；无 Redis 写入；无 MQ。

**异常场景**：

| 触发条件 | 产生位置 | HTTP | code | 已写库 |
|---|---|---|---|---|
| 用户名/密码长度非法 | RPC logic | 200 | 2 ParamError | 否 |
| 用户名已存在（先查命中） | RPC logic | 200 | 10001 | 否 |
| **并发注册同名**（先查未命中、Insert 撞 uk_username 1062） | model | 200 | **1 ServerError（1062 未兜底转 10001）** | 否（唯一索引拦截） |
| bcrypt/DB/etcd 故障 | RPC | 200 | 1 | 视阶段 |

代码依据：`registerLogic.go:52-58`（仅处理 ErrNotFound 之外的错，Insert 错误直接上抛，无 `mysql.MySQLError 1062` 判断）。

**幂等与并发**：靠 `uk_username` 保证不重复写入；但并发时错误码退化为 1 而非 10001（风险 R-P1-2）。

**测试用例**：

| 用例编号 | 测试层级 | 测试场景 | 前置条件 | 输入 | Mock 或真实依赖 | 预期响应 | 预期数据库变化 | 预期缓存变化 | 预期消息 | 代码依据 |
|---|---|---|---|---|---|---|---|---|---|---|
| U-REG-01 | RPC Logic 单元测试 | 正常注册 | username 不存在 | 合法 username/password | model stub | code=0，token 非空，user_id>0 | INSERT users（stub 断言） | 无 | 无 | registerLogic.go:44-90 |
| U-REG-02 | RPC Logic 单元测试 | 用户名已存在 | 同名用户已在 | 重复 username | model stub 返回已有行 | code=10001 | 无 | 无 | 无 | registerLogic.go:52-56 |
| U-REG-03 | RPC Logic 单元测试 | 参数边界 | - | username 1/33 字符、password 5/65 字符 | model stub | code=2 | 无 | 无 | 无 | registerLogic.go:36-42 |
| U-REG-04 | Model 集成测试 | 唯一索引冲突 | 真实 MySQL 已有同名行 | 直接 Insert 同名 | 真实 MySQL | Error 1062 | 不新增行 | 无 | 无 | deploy/sql/user.sql uk_username |
| U-REG-05 | 并发测试 | 并发同名注册 | 空表 | 20 goroutine 同 username | 真实 MySQL | 恰 1 个成功；其余当前返回 code=1（记录基线） | users 仅 1 行 | 无 | 无 | registerLogic.go:52-58 |
| U-REG-06 | HTTP API 自动化测试 | 端到端注册+token 可用 | 网关+user 服务运行 | POST register 后携 token 调 /users/me | 真实环境 | 200/code=0；me 返回同 user_id | users +1 | 无 | 无 | routes.go:32-58 |

### HTTP POST /api/v1/users/login（用户登录）

**基本信息**：Handler `loginHandler.go`；Logic `logic/loginLogic.go`；RPC `User.Login`。免 JWT。

**请求参数**：Body `username`(string 必填)、`password`(string 必填)。RPC 侧仅校验非空（`app/user/rpc/internal/logic/loginLogic.go:34-37`），无长度校验。

**成功响应**：同注册结构（token+user）。

**业务规则**：① 非空校验 → ② `FindOneByUsername`，NotFound 返回 **10002**（不区分用户不存在与密码错误，防枚举）→ ③ `status!=1` 返回 10005 → ④ bcrypt CompareHashAndPassword 失败返回 10002 → ⑤ 签发 JWT。代码依据：`loginLogic.go:39-66`。

**数据副作用**：无写库、无缓存、无 MQ（**无登录态存储，登出/吊销机制不存在**）。

**异常场景**：用户不存在→10002；密码错误→10002；账号禁用→10005；DB 错→1。均 HTTP 200。

**幂等与并发**：只读，天然幂等。

**测试用例**：

| 用例编号 | 测试层级 | 测试场景 | 前置条件 | 输入 | Mock 或真实依赖 | 预期响应 | 预期数据库变化 | 预期缓存变化 | 预期消息 | 代码依据 |
|---|---|---|---|---|---|---|---|---|---|---|
| U-LOG-01 | RPC Logic 单元测试 | 正确密码登录 | 已注册用户 | 正确凭据 | model stub（bcrypt 真实哈希） | code=0，token 可被 jwtx.Parse 解出 user_id | 无 | 无 | 无 | loginLogic.go:39-66 |
| U-LOG-02 | RPC Logic 单元测试 | 密码错误/用户不存在 | 同上 | 错密码；不存在用户名 | model stub | 均 code=10002，消息一致 | 无 | 无 | 无 | loginLogic.go:43-58 |
| U-LOG-03 | RPC Logic 单元测试 | 禁用账号 | status=0 用户 | 正确凭据 | model stub | code=10005 | 无 | 无 | 无 | loginLogic.go:49 |
| U-LOG-04 | HTTP API 自动化测试 | JWT 过期/伪造 | 过期 token、错 secret token | GET /users/me | 真实网关 | **HTTP 401 空体** | 无 | 无 | 无 | routes.go:95 rest.WithJwt |

### HTTP POST /api/v1/upload/token（上传凭证，占位）

**基本信息**：Handler `uploadTokenHandler.go`；Logic `uploadTokenLogic.go`；不调用任何 RPC。需 JWT。

**当前行为**：无论输入恒返回 `code=10006 获取上传凭证失败`。代码依据：`app/gateway/internal/logic/uploadTokenLogic.go:27-29`。

**测试用例**：

| 用例编号 | 测试层级 | 测试场景 | 前置条件 | 输入 | Mock 或真实依赖 | 预期响应 | 预期数据库变化 | 预期缓存变化 | 预期消息 | 代码依据 |
|---|---|---|---|---|---|---|---|---|---|---|
| U-UPT-01 | Gateway Logic 单元测试 | 占位行为回归 | 合法 JWT | 任意 body | 无依赖 | code=10006 | 无 | 无 | 无 | uploadTokenLogic.go:27-29 |

待确认：当前代码未定义上传凭证的真实实现方案（对象存储类型、凭证格式）。

### HTTP GET /api/v1/users/:userId（他人主页）

**基本信息**：Handler `getUserHandler.go`；Logic `getUserLogic.go`；RPC：`User.GetUser` → 并行 `Relation.GetFollows(page=1,size=1)`、`Relation.GetFans(page=1,size=1)`（取 Total 作计数）、`Relation.IsFollow`。需 JWT。

**请求参数**：Path `userId` int64。**Gateway 与 RPC 均未校验 `userId>0`**（`getUserLogic.go`、`app/user/rpc/internal/logic/getUserLogic.go` 直接查库），0/负数走 DB 查询返回 10003。契约缺口。

**成功响应**：`code=0`，data=UserProfile{user 基础字段, fans_count, follow_count, is_following}。**fans_count/follow_count 来自 Relation 列表接口的 Total 字段**，而 Total 在 Redis 命中时=ZCard、回源时=len(当前页)，存在不准（见 Relation 节）。

**业务规则**：GetUser 失败立即返回；关注数/粉丝数/is_following 三个调用失败时**降级为 0/false，不报错**（`getUserLogic.go`，errgroup 内吞错记日志）。

**异常场景**：目标用户不存在→10003；User RPC 挂→1；Relation RPC 挂→降级 0。

**测试用例**：

| 用例编号 | 测试层级 | 测试场景 | 前置条件 | 输入 | Mock 或真实依赖 | 预期响应 | 预期数据库变化 | 预期缓存变化 | 预期消息 | 代码依据 |
|---|---|---|---|---|---|---|---|---|---|---|
| U-GET-01 | Gateway Logic 单元测试 | 正常聚合 | mock 4 个 RPC 正常 | userId=目标 | gomock | code=0，计数与 is_following 正确 | 无 | 无 | 无 | getUserLogic.go |
| U-GET-02 | Gateway Logic 单元测试 | Relation 降级 | Relation mock 全错 | 同上 | gomock | code=0，计数=0、is_following=false | 无 | 无 | 无 | getUserLogic.go |
| U-GET-03 | Gateway Logic 单元测试 | 用户不存在 | GetUser 返回 10003 | 任意 | gomock | code=10003 | 无 | 无 | 无 | getUserLogic.go |
| U-GET-04 | HTTP API 自动化测试 | userId=0/负数/非数字 | 真实环境 | /users/0、/users/abc | 真实 | 0→code=10003；abc→httpx.Parse 失败 code=2（记录基线） | 无 | 无 | 无 | 契约缺口 |

### HTTP GET /api/v1/users/me（本人信息）

**基本信息**：Handler `getMeHandler.go`；Logic `getMeLogic.go`；RPC `User.GetUser(userID from JWT)`。需 JWT。

**业务规则与风险**：`middleware.MustGetUserID` 取不到 user_id 时 logic 返回 `(nil, nil)` —— handler 会以 `data=null, code=0` 成功返回（**异常静默**，风险 R-P2-1）。代码依据：`getMeLogic.go`（uid==0 分支）、`middleware/auth.go:14-39`。

**测试用例**：

| 用例编号 | 测试层级 | 测试场景 | 前置条件 | 输入 | Mock 或真实依赖 | 预期响应 | 预期数据库变化 | 预期缓存变化 | 预期消息 | 代码依据 |
|---|---|---|---|---|---|---|---|---|---|---|
| U-ME-01 | Gateway Logic 单元测试 | 正常返回本人 | ctx 注入 user_id | - | gomock GetUser | code=0，user_id=JWT 值 | 无 | 无 | 无 | getMeLogic.go |
| U-ME-02 | Gateway Logic 单元测试 | ctx 无 user_id | ctx 空 | - | 无 | 当前返回 (nil,nil)→data=null code=0（基线，建议改 3） | 无 | 无 | 无 | getMeLogic.go |
| U-ME-03 | Gateway Handler 测试 | claim 类型兼容 | claim 为 string/float64 | - | httptest | MustGetUserID 均解析成功 | 无 | 无 | 无 | auth.go:22-35 |

### HTTP PATCH /api/v1/users/me（更新资料）

**基本信息**：Handler `updateMeHandler.go`；Logic `updateMeLogic.go`；RPC `User.UpdateUser`。需 JWT。

**请求参数**：Body `nickname`/`avatar`/`bio` 均 optional string。**Gateway 与 RPC 均无长度校验**；RPC 侧空字符串视为"不更新"（`app/user/rpc/internal/logic/updateUserLogic.go`，逐字段非空判断），**因此无法将字段清空为空串**——契约缺口/待确认。DB 上限 nickname varchar(64)、avatar varchar(255)、bio varchar(256)，超长报 DB 错误→code=1。

**数据副作用**：UPDATE `users`（goctl 缓存自动失效行缓存 `cache:users:id:*`）；`user:brief:{id}` 快照缓存**不失效，最长 600s 脏读**（`batchGetUsersLogic.go` 写入，更新路径无删除）——风险 R-P1-3 关联。

**测试用例**：

| 用例编号 | 测试层级 | 测试场景 | 前置条件 | 输入 | Mock 或真实依赖 | 预期响应 | 预期数据库变化 | 预期缓存变化 | 预期消息 | 代码依据 |
|---|---|---|---|---|---|---|---|---|---|---|
| U-UPD-01 | RPC Logic 单元测试 | 部分字段更新 | 已有用户 | 仅 nickname | model stub | code=0，仅 nickname 变更 | UPDATE users | goctl 行缓存删除 | 无 | updateUserLogic.go |
| U-UPD-02 | RPC Logic 单元测试 | 空串不更新语义 | 已有 bio | bio="" | model stub | bio 保持原值（基线） | 无变更 | 无 | 无 | updateUserLogic.go |
| U-UPD-03 | 跨服务集成测试 | brief 缓存脏读窗口 | 先 BatchGetUsers 预热 brief | 更新 nickname 后立即 BatchGetUsers | 真实 Redis+MySQL | 600s 内返回旧昵称（记录基线） | - | user:brief 未失效 | 无 | batchGetUsersLogic.go |
| U-UPD-04 | HTTP API 自动化测试 | 超长字段 | 真实环境 | bio 500 字符 | 真实 | code=1（DB 错误，记录基线） | 无 | 无 | 无 | 契约缺口 |

### RPC User.Register / Login / GetUser / UpdateUser / BatchGetUsers

| 项 | Register | Login | GetUser | UpdateUser | BatchGetUsers |
|---|---|---|---|---|---|
| proto | user.proto:81-87（各方法请求/响应 message 同名） | 同左 | GetUserReq{user_id} | UpdateUserReq{user_id,nickname,avatar,bio} | BatchGetUsersReq{user_ids[]} |
| Server→Logic | server/userServer.go → logic/registerLogic.go | loginLogic.go | getUserLogic.go | updateUserLogic.go | batchGetUsersLogic.go |
| 调用方 | Gateway register | Gateway login | Gateway getMe/getUser | Gateway updateMe | Gateway 聚合、Comment fillUserInfos（`app/comment/rpc/internal/logic/common.go:140-170`） |
| 字段约束 | username 2-32、password 6-64 | 非空 | 无（缺 user_id>0 校验） | user_id 必填，其余可选 | ids 去重；**无数量上限**——契约缺口 |
| 依赖 | MySQL+bcrypt+jwtx | 同左 | MySQL(goctl 行缓存) | MySQL | MySQL + Redis `user:brief:{id}` TTL 600s，MGET→miss 回源→回填 |
| 错误 | 2/10001/1 | 2/10002/10005/1 | 10003/1 | 10003/1 | 缺失 ID 静默跳过 |
| gRPC 错误转换 | ErrorInterceptor→codes.Unknown+`[bizerror]`（`app/user/rpc/user.go:38`） | 同 | 同 | 同 | 同 |
| 幂等 | uk_username；1062 未兜底 | 只读 | 只读 | 重复更新幂等 | 只读 |
| 已有测试 | **无** | **无** | **无** | **无** | **无** |
| 建议层级 | RPC Logic 单元 + Model 集成 + 并发 | RPC Logic 单元 | RPC Logic 单元 | RPC Logic 单元 + 跨服务集成（缓存） | RPC Logic 单元（miniredis 验证回填与 TTL） |

## Feed 接口测试基线

### HTTP POST /api/v1/feeds（发布帖子）

**基本信息**：Handler `feed/createFeedHandler.go`；Logic `logic/feed/createFeedLogic.go`；RPC `Feed.CreateFeed`。需 JWT。Content-Type application/json。

**认证与权限**：user_id 来自 JWT（MustGetUserID），uid==0 返回 Unauthorized(3)。RPC 层直接信任请求中的 user_id（风险 R-P0-1）。

**请求参数**（`app/gateway/api/feed.api` + `types.go` + RPC 校验 `app/feed/rpc/internal/logic/createfeedlogic.go:40-66`）：

| 字段 | 类型 | 必填 | Gateway 校验 | RPC 校验 |
|---|---|---|---|---|
| type | int32 | 是 | 无 | 必须 1(视频)/2(图文)，否则 12005 |
| title | string | 否 | 无 | **无长度校验**（DB varchar(128)，超长→DB 错→1）契约缺口 |
| content | string | 条件 | 无 | 图文(2)必填否则 12003；≤5000 否则 2；视频可空 |
| media_urls | []string | 条件 | 无 | 非空否则 12004；≤9 个否则 2；**未校验 URL 格式/域名** |
| cover_url | string | 否 | 无 | 无校验 |
| city_code | string | 否 | 无 | 无校验（空则不进城市流） |

Gateway 额外通过 `clientip` 中间件取客户端 IP 传入 RPC（`middleware/clientip.go`：X-Forwarded-For 第一段→X-Real-Ip→RemoteAddr），当前 RPC 侧未用 IP 做定位（12006 未触发路径）。

**成功响应**：`code=0`，data=FeedDetail（feed_id、author 降级可空、统计 0 值）。

**业务规则**（RPC 侧）：① 参数校验 → ② 调 `Relation.IsVip(user_id)` 判定大V（**失败降级 is_vip=false，仅日志**，`createfeedlogic.go:70-76`）→ ③ Snowflake 生成 feed_id → ④ INSERT feeds（status=1，各计数 0）→ ⑤ 构造 `EventFeedCreate`（event_id=UUID）经 `Producer.SendSync` 发 `feed-created`（**实际为 SendAsync+回调仅记日志**，`common/mq/producer.go:32-38`）→ 返回。**MQ 在 DB 写入之后、无本地消息表/补偿**（风险 R-P0-2）。

**数据副作用**：INSERT `feeds`（主键 id，无业务唯一索引）；无直接 Redis 写（由 Worker 异步写 outbox/recommend/city/inbox）；发 `feed-created` 消息。

**异常场景**：

| 触发条件 | 位置 | code | 已写库 | 已发消息 |
|---|---|---|---|---|
| type 非法/内容空/媒体空/超限 | RPC 校验 | **1**（Feed 服务无 ErrorInterceptor，12005/12003/12004/2 被吞为 ServerError，R-P1-1） | 否 | 否 |
| IsVip 失败 | RPC | 成功（降级非大V，**大V 帖可能误走 fanout**） | 是 | 是 |
| DB Insert 失败 | model | 1 | 否 | 否 |
| MQ 发送失败 | producer 回调 | **成功返回 code=0**，帖子进 DB 但永不进任何时间线 | 是 | 否 |

**幂等与并发**：**无幂等设计**——无客户端幂等键，重复提交产生多条 feeds（风险 R-P1-4）。

**测试用例**：

| 用例编号 | 测试层级 | 测试场景 | 前置条件 | 输入 | Mock 或真实依赖 | 预期响应 | 预期数据库变化 | 预期缓存变化 | 预期消息 | 代码依据 |
|---|---|---|---|---|---|---|---|---|---|---|
| F-CRT-01 | RPC Logic 单元测试 | 图文正常发布 | mock IsVip=false | type=2 全字段合法 | model stub+producer stub | feed_id>0 | INSERT feeds status=1 | 无 | feed-created 1 条，is_vip_feed=false | createfeedlogic.go:40-102 |
| F-CRT-02 | RPC Logic 单元测试 | 参数矩阵 | - | type=3；content 5001；media 10 个；图文空 content | stub | 直连 RPC 得 12005/2/2/12003；**经网关则均为 1**（基线） | 无 | 无 | 无 | createfeedlogic.go:40-66、grpc.go:26 |
| F-CRT-03 | RPC Logic 单元测试 | IsVip 降级 | IsVip mock 报错 | 合法大V发帖 | stub | code=0 且事件 is_vip_feed=false（记录基线风险） | INSERT | 无 | 1 条 | createfeedlogic.go:70-76 |
| F-CRT-04 | RPC Logic 单元测试 | MQ 失败不回滚 | producer stub 报错 | 合法 | stub | code=0（基线：主流程成功） | INSERT 保留 | 无 | 0 条 | producer.go:32-38 |
| F-CRT-05 | RocketMQ 消费测试 | 事件驱动扩散 | 真实 MQ+Redis | 发布非大V帖 | 真实 | - | - | outbox/recommend/city ZSet 各 +1；粉丝 inbox +1 | 消费成功 | worker.go:84-108 |
| F-CRT-06 | 端到端测试 | 发布后可见性 | 粉丝已关注作者 | 发布→粉丝拉关注流 | 真实全链路 | 新帖出现在粉丝 timeline | feeds+1 | inbox 更新 | feed-created | 全链路 |
| F-CRT-07 | 并发测试 | 重复提交 | 同一 body | 5 并发 | 真实 | 5 条不同 feed_id（记录无幂等基线） | feeds+5 | - | 5 条 | 无幂等键 |

### HTTP DELETE /api/v1/feeds/:feedId（删除帖子）

**基本信息**：Handler `feed/deleteFeedHandler.go`；Logic `logic/feed/deleteFeedLogic.go`；RPC：先 `Feed.GetFeed` 预检、再 `Feed.DeleteFeed`。需 JWT。

**认证与权限（双层所有者校验）**：
- Gateway 层：GetFeed 后比对 `feed.AuthorId != uid` → 返回 `FeedNoPermission(12002)`。代码依据：`app/gateway/internal/logic/feed/deleteFeedLogic.go`。
- RPC 层：DeleteFeed 内 `FindOne` 后比对 `feed.UserId != in.UserId` → 12002。代码依据：`app/feed/rpc/internal/logic/deletefeedlogic.go:47-52`。
- RPC 层校验依赖请求 user_id 可信，直连 RPC 可伪造 user_id 删除任意帖（R-P0-1）。

**请求参数**：Path `feedId` int64；Gateway 校验 `feedId<=0`→ParamError(2)。

**成功响应**：`code=0, data=null`。

**业务规则**（RPC）：① FindOne，NotFound→12001 → ② 已删（status=2）→**幂等成功**（`deletefeedlogic.go:44`）→ ③ 所有者比对 → ④ UPDATE status=2（软删，`app/feed/model/feedsModel.go:166`）→ ⑤ DEL Redis `feed:{feedID}` 详情缓存（失败仅日志）→ ⑥ 发 `feed-deleted` 事件（DB 后，异步，失败仅日志）。

**数据副作用**：UPDATE feeds.status=2；DEL `feed:{id}`；MQ feed-deleted → Worker ZREM recommend/city/outbox/粉丝 inbox。

**异常场景**：feed 不存在→经网关 12001（网关 GetFeed 预检返回，Feed 无拦截器但网关自身预检的 GetFeed 错误同样被吞为 1——实测基线：GetFeed NotFound 经网关为 **1**，R-P1-1）；非本人→12002（网关层判定，可正常返回）；MQ 失败→DB 已删但时间线 ZSet 残留（读端 status 过滤兜底，但 ZSet 泄漏）。

**幂等与并发**：重复删除幂等成功；并发删除靠 UPDATE 天然幂等；删除与消费竞态由读端 status=1 过滤兜底。

**测试用例**：

| 用例编号 | 测试层级 | 测试场景 | 前置条件 | 输入 | Mock 或真实依赖 | 预期响应 | 预期数据库变化 | 预期缓存变化 | 预期消息 | 代码依据 |
|---|---|---|---|---|---|---|---|---|---|---|
| F-DEL-01 | Gateway Logic 单元测试 | 非所有者删除 | GetFeed mock 返回他人帖 | feedId 合法 | gomock | code=12002，且 **DeleteFeed 未被调用** | 无 | 无 | 无 | gateway deleteFeedLogic.go（已有测试 deleteFeedLogic_test.go） |
| F-DEL-02 | RPC Logic 单元测试 | 所有者软删 | 本人帖 status=1 | 匹配 user_id | model stub+miniredis | code=0 | status→2 | feed:{id} 被 DEL | feed-deleted 1 条 | deletefeedlogic.go:44-70 |
| F-DEL-03 | RPC Logic 单元测试 | 重复删除幂等 | status=2 | 再删 | stub | code=0，无第二次 UPDATE/消息 | 无 | 无 | 0 | deletefeedlogic.go:44 |
| F-DEL-04 | RPC 集成测试 | 直连 RPC 伪造 user_id | 真实 feed 服务 | 他人 user_id | 真实 | 当前**可删除成功**（P0 基线记录） | status→2 | - | 1 条 | R-P0-1 |
| F-DEL-05 | RocketMQ 消费测试 | 删除扩散 | 已在各 ZSet | 删除大V/非大V帖 | 真实 MQ+Redis | - | - | recommend/city/outbox ZREM；非大V粉丝 inbox ZREM | 消费成功 | worker.go:115-160 |
| F-DEL-06 | 端到端测试 | 删除后不可见 | 帖在 timeline | 删除→拉详情/timeline | 真实 | 详情 12001→网关 1（基线）；timeline 不含该帖 | - | - | - | 全链路 |

### HTTP GET /api/v1/feeds/:feedId（帖子详情）

**基本信息**：Handler `feed/getFeedDetailHandler.go`；Logic `logic/feed/getFeedDetailLogic.go`。RPC 编排：`Feed.GetFeed`（主）→ 并行（errgroup）`User.GetUser`、`Relation.IsFollow`、`Interaction.GetFeedStats`、`Interaction.GetUserInteractionStatus`。需 JWT。

**业务规则**：GetFeed 失败立即返回错误；其余 4 个聚合调用**失败降级**（author 可为 null、统计 0、状态 false，仅日志）。统计数优先取 Interaction 实时值，失败回退 feeds 表镜像计数。代码依据：`app/gateway/internal/logic/feed/getFeedDetailLogic.go`。

**异常场景**：feedId<=0→2；帖子不存在/已删→RPC 12001→网关 code=1（R-P1-1 基线）；聚合失败→code=0 部分字段缺省。

**测试用例**：

| 用例编号 | 测试层级 | 测试场景 | 前置条件 | 输入 | Mock 或真实依赖 | 预期响应 | 预期数据库变化 | 预期缓存变化 | 预期消息 | 代码依据 |
|---|---|---|---|---|---|---|---|---|---|---|
| F-GET-01 | Gateway Logic 单元测试 | 全量聚合成功 | 5 个 RPC mock 正常 | feedId | gomock | code=0，author/stats/status 齐全 | 无 | 无 | 无 | getFeedDetailLogic.go |
| F-GET-02 | Gateway Logic 单元测试 | 聚合部分失败降级 | User/Interaction mock 报错 | feedId | gomock | code=0，author=null、统计回退镜像值 | 无 | 无 | 无 | getFeedDetailLogic.go |
| F-GET-03 | RPC Logic 单元测试 | 缓存命中/未命中/已删 | miniredis 预置 Hash | GetFeed | miniredis+stub | 命中不查库；miss 回源并回填 TTL 30d；status=2→12001 | 无 | feed:{id} 回填 | 无 | getfeedlogic.go |
| F-GET-04 | HTTP API 自动化测试 | 不存在的 feed | 真实环境 | 随机 feedId | 真实 | code=1（基线，期望 12001） | 无 | 无 | 无 | R-P1-1 |

### HTTP GET /api/v1/feeds/timeline（时间线）

**基本信息**：Handler `feed/timelineHandler.go`；Logic `logic/feed/timelineLogic.go`。需 JWT。

**请求参数**（Query）：`type` string，可选，默认 recommend，合法值 recommend/follow/city（非法→2）；`cursor` string 可选；`page_size` int 可选（ClampPageSize：≤0→10，>50→50，`aggregate/feedcard.go:213`）；`city_code` string，type=city 时必填否则 2。代码依据：`timelineLogic.go`。

**业务规则**：按 type 分派 `GetRecommendTimeline` / `GetFollowTimeline` / `GetCityTimeline` → 得 feed 列表后调 `aggregate.BuildFeedCards` 聚合：并行（errgroup）`User.BatchGetUsers` + `Interaction.BatchGetFeedStats` + `Interaction.BatchGetUserInteractionStatus`；**三路聚合失败均降级**（作者 null / 统计回退 feeds 镜像计数 / 状态 false）；**评论数不调 Comment RPC，直接用 feeds.comment_count 镜像列**。代码依据：`app/gateway/internal/logic/aggregate/feedcard.go:60-160`。

- recommend/city：cursor 为页码型（`PageFromCursor`），RPC 侧 Redis ZSet `feed:recommend`/`feed:city:{code}` REVRANGE 分页，city 缓存 miss 回源 MySQL；recommend **miss 不回源**（返回空，待确认是否预期）。
- follow：cursor 为 `base64(score:id)`；RPC 侧合并 inbox（普通关注）+ 在线拉取大V outbox（`GetFollowTimeline` 调 Relation.GetFollows+IsVip），归并排序后截断。代码依据：`app/feed/rpc/internal/logic/getfollowtimelinelogic.go`。

**成功响应**：`code=0`，data={list:[FeedCard], cursor, has_more}。

**测试用例**：

| 用例编号 | 测试层级 | 测试场景 | 前置条件 | 输入 | Mock 或真实依赖 | 预期响应 | 预期数据库变化 | 预期缓存变化 | 预期消息 | 代码依据 |
|---|---|---|---|---|---|---|---|---|---|---|
| F-TL-01 | Gateway Logic 单元测试 | type 分派与非法值 | mock 3 个 timeline RPC | type=recommend/follow/city/xxx | gomock | 前三者调对应 RPC；xxx→code=2 | 无 | 无 | 无 | timelineLogic.go（已有测试） |
| F-TL-02 | Gateway Logic 单元测试 | city 缺 city_code | - | type=city 无 city_code | gomock | code=2 | 无 | 无 | 无 | timelineLogic.go |
| F-TL-03 | Gateway Logic 单元测试 | FeedCard 聚合降级 | BatchGetUsers/Stats mock 报错 | 正常列表 | gomock | code=0，author=null、统计=镜像值（comment_count 恒镜像列） | 无 | 无 | 无 | feedcard.go:60-160（已有测试） |
| F-TL-04 | RPC Logic 单元测试 | 关注流 inbox+大V outbox 归并 | miniredis 预置 inbox/outbox | 游标翻页 | miniredis+mock Relation | 排序正确、无重复、游标推进 | 无 | 无 | 无 | getfollowtimelinelogic.go（已有测试） |
| F-TL-05 | 跨服务集成测试 | 关注流端到端排序 | 真实 feed+relation+redis | 大V/普通混合关注 | 真实 | 时间倒序、已删帖被过滤 | 无 | 无 | 无 | 全链路 |
| F-TL-06 | 并发测试 | 翻页与新发布并发 | 持续发帖 | 同时翻页 | 真实 | 页码型游标出现重复/漏帖（记录基线，待确认验收标准） | - | - | - | feedcard.go:225-242 |

### HTTP GET /api/v1/users/:userId/feeds（用户帖子列表）

**基本信息**：Handler `feed/userFeedsHandler.go`；Logic `logic/feed/userFeedsLogic.go`；RPC `Feed.GetUserFeeds`（MySQL 直查 status=1 倒序分页，无缓存）+ BuildFeedCards 聚合。需 JWT。Path `userId`，Query `cursor`（页码型）、`page_size`。userId<=0→2。

**测试用例**：

| 用例编号 | 测试层级 | 测试场景 | 前置条件 | 输入 | Mock 或真实依赖 | 预期响应 | 预期数据库变化 | 预期缓存变化 | 预期消息 | 代码依据 |
|---|---|---|---|---|---|---|---|---|---|---|
| F-UF-01 | RPC Logic 单元测试 | 分页与软删过滤 | sqlmock/stub 数据含 status=2 | page 翻页 | stub | 仅 status=1，倒序，has_more 正确 | 无 | 无 | 无 | getuserfeedslogic.go（已有测试） |
| F-UF-02 | HTTP API 自动化测试 | 查看他人帖子列表 | 真实环境 | 他人 userId | 真实 | code=0（**任何登录用户可查任何人**，基线） | 无 | 无 | 无 | userFeedsLogic.go |

### RPC Feed 服务方法基线（8 个）

Feed 服务 Redis Key（`app/feed/rpc/internal/keys/keys.go`）：

| Key | 结构 | TTL | 用途 |
|---|---|---|---|
| `inbox:{userID}` | ZSet(score=发布秒级时间戳, member=feedID) | 无 TTL，收件箱容量截断（worker 修剪） | 关注流收件箱 |
| `outbox:{userID}` | ZSet | 无 TTL | 作者发件箱（大V 拉模式源） |
| `feed:recommend` | ZSet | 无 TTL | 推荐池 |
| `feed:city:{cityCode}` | ZSet | 无 TTL | 同城流 |
| `feed:{feedID}` | Hash（帖子全字段） | **30 天**（`keys.go` FeedDetailTTL） | 详情缓存 |
| `comment_event:{eventID}` | String(SETNX) | 24h | comment-event 消费去重 |

| 方法 | 关键行为与依据 | 错误码 | 幂等/并发 | 已有测试 | 建议层级 |
|---|---|---|---|---|---|
| CreateFeed | 见 HTTP 节；IsVip 降级；MQ 后置无补偿（createfeedlogic.go:70-102） | 2/12003/12004/12005/1 | 无幂等键 | **无** | RPC Logic 单元 + RocketMQ 消费 + 并发 |
| DeleteFeed | 软删+DEL 详情缓存+feed-deleted（deletefeedlogic.go:44-70） | 12001/12002/1 | 重复删幂等 | **无** | RPC Logic 单元 + RPC 集成 |
| GetFeed | cache-aside `feed:{id}` Hash 30d；status=2→12001；**无空值缓存**（getfeedlogic.go） | 2/12001/1 | 只读 | 有（getfeed_logic_test.go） | 已覆盖，补穿透场景 |
| BatchGetFeeds | MySQL IN 查询；>100 截断；缺失/已删静默跳过；**不读不写缓存**（batchgetfeedslogic.go） | 2/1 | 只读 | 有 | 已覆盖 |
| GetRecommendTimeline | ZREVRANGE `feed:recommend` 页码分页→BatchGet 详情→status 过滤；**Redis miss 返回空不回源**（getrecommendtimelinelogic.go） | 2/1 | 只读 | 有 | 补 Redis 故障场景 |
| GetFollowTimeline | inbox + 大V outbox 在线归并（调 Relation.GetFollows/IsVip）；`base64(score:id)` 游标；Relation 失败降级仅 inbox（getfollowtimelinelogic.go） | 2/1 | 只读 | 有（含游标/归并） | 补 Relation 故障降级 |
| GetCityTimeline | `feed:city:{code}` ZSet，miss 回源 MySQL 按 city_code 查并重建（getcitytimelinelogic.go） | 2/1 | 只读 | 有 | 已覆盖 |
| GetUserFeeds | MySQL 直查分页（getuserfeedslogic.go） | 2/1 | 只读 | 有 | 已覆盖 |

**Feed 服务专属风险**：无 ErrorInterceptor（R-P1-1）；`feeds` 表镜像计数列 `like_count/collect_count` **全仓库无更新来源**（仅 comment_count 由 worker 维护），聚合降级时统计恒为 0（R-P1-5）。

## Comment 接口测试基线

### HTTP POST /api/v1/feeds/:feedId/comments（发表评论）

**基本信息**：Handler `comment/createCommentHandler.go`；Logic `logic/comment/createCommentLogic.go`；RPC `Comment.CreateComment`，成功后调 `User.GetUser` 补发评人昵称头像（失败降级空值）。需 JWT。

**请求参数**：Path `feedId`；Body `content`(string 必填)、`reply_comment_id`(int64 可选，回复某条评论时传)。Gateway 校验 feedId>0、content 非空；RPC 二次校验（`app/comment/rpc/internal/logic/createCommentLogic.go`）：content trim 后非空→13004，UTF-8 长度≤500→13005；reply_comment_id>0 时查父评论。

**业务规则**（RPC）：① 参数校验 → ② `Feed.GetFeed` 校验帖子存在且正常，失败→13002 → ③ 回复场景：FindOne 父评论，不存在/已删→13006，据父评论推导 root_id（父为根则 root_id=父 id；父为子回复则继承其 root_id，实现两级楼中楼展平）→ ④ **事务**：INSERT comments + 父评论（根）`reply_count+1`（`createCommentLogic.go`，`commentsModel` Trans）→ ⑤ Redis `comment_count:{feedId}` 存在则 INCR（不存在不建，防止与 DB 不一致）→ ⑥ 发 `comment-event`(CREATE) 消息（DB 后，异步，失败仅日志，`common.go:197`）。

**成功响应**：`code=0`，data=CommentInfo（含 comment_id、临时昵称）。

**数据副作用**：INSERT `comments`（无业务唯一索引，普通索引 idx_feed_root、idx_root）；根评论 reply_count+1（同事务）；Redis comment_count 条件 INCR；MQ comment-event(CREATE) → Feed Worker 更新 `feeds.comment_count` 镜像列（SETNX `comment_event:{eventID}` 24h 去重）。

**异常场景**：帖子不存在→13002；父评论不存在→13006；内容空/超长→13004/13005；事务失败→1 无副作用；Redis INCR 失败→仅日志（计数缓存与 DB 短暂不一致，TTL 1h 自愈）；MQ 失败→`feeds.comment_count` 镜像永久少 1（无补偿）。

**幂等与并发**：**无幂等**——重复提交产生多条评论（无去重键）；并发对同一根评论 reply_count 依赖 DB 行锁，安全。

**测试用例**：

| 用例编号 | 测试层级 | 测试场景 | 前置条件 | 输入 | Mock 或真实依赖 | 预期响应 | 预期数据库变化 | 预期缓存变化 | 预期消息 | 代码依据 |
|---|---|---|---|---|---|---|---|---|---|---|
| C-CRT-01 | RPC Logic 单元测试 | 一级评论成功 | FeedRpc mock 正常 | 合法 content | model stub+miniredis+producer stub | code=0 | INSERT comments root_id=0 | count 存在时 +1 | CREATE 事件 1 条 | createCommentLogic.go |
| C-CRT-02 | RPC Logic 单元测试 | 楼中楼 root 推导 | 父为根/父为子回复 | reply_comment_id | stub | root_id 分别=父 id/父.root_id；根 reply_count+1 | 事务两写 | - | 1 条 | createCommentLogic.go |
| C-CRT-03 | RPC Logic 单元测试 | 校验矩阵 | - | 空串/501 字/帖子不存在/父评论已删 | stub | 13004/13005/13002/13006 | 无 | 无 | 无 | createCommentLogic.go |
| C-CRT-04 | RPC Logic 单元测试 | MQ 失败主流程成功 | producer 报错 | 合法 | stub | code=0（基线） | INSERT 保留 | +1 | 0 条 | common.go:197 |
| C-CRT-05 | Model 集成测试 | 事务原子性 | 真实 MySQL | 强制第二步失败 | 真实 | 回滚，无评论行 | comments 无新增 | - | - | commentsModel Trans |
| C-CRT-06 | RocketMQ 消费测试 | 镜像计数与去重 | 真实 MQ+Redis+MySQL | 同一 event_id 投两次 | 真实 | - | feeds.comment_count 仅 +1 | comment_event:{id} SETNX | 消费成功 | worker.go:211-260 |

### HTTP GET /api/v1/feeds/:feedId/comments（评论列表）

**基本信息**：Handler `comment/listCommentsHandler.go`；Logic `logic/comment/listCommentsLogic.go`。RPC：`Comment.ListComments` 与（仅第 1 页时）`Comment.GetHotComments` **并行调用**（errgroup）；热门失败降级为空列表。需 JWT。

**请求参数**：Path feedId>0；Query `page`(默认 1)、`page_size`（Gateway Clamp≤50；RPC 侧默认 20、最大 100——**两层上限不一致**，实际生效 50）。

**业务规则**（RPC ListComments）：页码分页查一级评论（status=1，时间倒序）→ 每条根评论**预览前 2 条子回复**（`FindTopRepliesGrouped`）→ total 来自 `comment_count` 缓存（cache-aside，miss 时 COUNT 回源并 SETEX 1h）→ `fillUserInfos` 批量调 `User.BatchGetUsers` 补昵称头像（失败降级空值，`common.go:140-170`）。

**测试用例**：

| 用例编号 | 测试层级 | 测试场景 | 前置条件 | 输入 | Mock 或真实依赖 | 预期响应 | 预期数据库变化 | 预期缓存变化 | 预期消息 | 代码依据 |
|---|---|---|---|---|---|---|---|---|---|---|
| C-LST-01 | Gateway Logic 单元测试 | 第 1 页并行热门+普通 | mock 两 RPC | page=1 | gomock | hot_comments 与 list 均返回；hot 失败→空数组仍 code=0 | 无 | 无 | 无 | listCommentsLogic.go（已有测试） |
| C-LST-02 | RPC Logic 单元测试 | 回复预览与用户填充 | stub 数据两条子回复以上 | page=1 | stub+mock UserRpc | 每根评论 replies≤2；BatchGetUsers 失败昵称为空 | 无 | 无 | 无 | listCommentsLogic.go(rpc)、common.go:140 |
| C-LST-03 | RPC Logic 单元测试 | 计数缓存 miss 回源 | miniredis 空 | 任意 | miniredis+stub | total=COUNT 值 | 无 | comment_count SETEX 1h | 无 | common.go:109-130 |
| C-LST-04 | 跨服务集成测试 | 删评后列表一致性 | 真实环境 | 删除后拉列表 | 真实 | 已删评论及其子回复不可见 | - | - | - | 集成已有部分覆盖 |

### HTTP GET /api/v1/comments/:rootId/replies（回复列表）

**基本信息**：Handler `comment/listRepliesHandler.go`；Logic `logic/comment/listRepliesLogic.go`；RPC `Comment.ListReplies`。需 JWT。

**业务规则**（RPC，`app/comment/rpc/internal/logic/listRepliesLogic.go:35-95`）：rootId>0；page_size 默认 20/最大 100；游标 `created_at+id` 组合（防漏防重），解码失败→2；根评论必须存在、status=1 且 root_id=0，否则 13001；时间**正序**多取 1 条判 has_more；Total=root.ReplyCount。

**测试用例**：

| 用例编号 | 测试层级 | 测试场景 | 前置条件 | 输入 | Mock 或真实依赖 | 预期响应 | 预期数据库变化 | 预期缓存变化 | 预期消息 | 代码依据 |
|---|---|---|---|---|---|---|---|---|---|---|
| C-RPL-01 | RPC Logic 单元测试 | 游标翻页完整性 | stub 25 条同秒回复 | 连续翻页 | stub | 无重复无遗漏、正序、cursor 推进 | 无 | 无 | 无 | listRepliesLogic.go:45-84 |
| C-RPL-02 | RPC Logic 单元测试 | root 非法矩阵 | root 已删/root 是子回复/不存在 | rootId | stub | 均 13001 | 无 | 无 | 无 | listRepliesLogic.go:51-60 |
| C-RPL-03 | RPC Logic 单元测试 | 坏游标 | - | cursor="!!!" | stub | code=2 | 无 | 无 | 无 | listRepliesLogic.go:45-48 |

### HTTP DELETE /api/v1/comments/:commentId（删除评论）

**基本信息**：Handler `comment/deleteCommentHandler.go`；Logic `logic/comment/deleteCommentLogic.go`；RPC `Comment.DeleteComment`。需 JWT。

**认证与权限**：所有者校验**仅在 RPC 层**：`comment.UserId != in.UserId` → 13003（`app/comment/rpc/internal/logic/deleteCommentLogic.go`）。**评论作者本人可删，帖子作者不能删他人评论**（代码未实现帖主删评，待确认是否产品预期）。直连 RPC 可伪造 user_id（R-P0-1）。

**业务规则**（RPC）：① FindOne，不存在→13001；已删（status=2）→**幂等成功** → ② 所有者校验 → ③ 事务：本条 status=2 软删；若为根评论，**级联软删全部子回复**（UPDATE ... WHERE root_id=?）；若为子回复，根评论 reply_count-1 → ④ Redis `comment_count` 存在则 DECRBY（删根时=1+子回复数）→ ⑤ 发 comment-event(DELETE)，消息体 `content_len` 复用为删除数量（Worker 按该值扣减镜像计数）。

**数据副作用**：UPDATE comments（1..N 行 status=2）；根 reply_count 修正；Redis 计数 DECR；MQ DELETE 事件 → feeds.comment_count 镜像扣减。

**幂等与并发**：重复删除幂等成功不重发消息；**并发"删根评论"与"新增子回复"存在竞态**：级联删除与插入交错时可能残留 status=1 的孤儿子回复（读端 root 校验兜底不展示，但计数可能偏差）——记录基线。

**测试用例**：

| 用例编号 | 测试层级 | 测试场景 | 前置条件 | 输入 | Mock 或真实依赖 | 预期响应 | 预期数据库变化 | 预期缓存变化 | 预期消息 | 代码依据 |
|---|---|---|---|---|---|---|---|---|---|---|
| C-DEL-01 | RPC Logic 单元测试 | 删他人评论 | 评论属他人 | user_id 不匹配 | stub | 13003，无写库 | 无 | 无 | 无 | deleteCommentLogic.go |
| C-DEL-02 | RPC Logic 单元测试 | 删根级联 | 根+3 子回复 | 删根 | stub+miniredis | code=0 | 4 行 status=2 | count -4 | DELETE 事件 content_len=4 | deleteCommentLogic.go |
| C-DEL-03 | RPC Logic 单元测试 | 删子回复 | 楼中楼 | 删子 | stub | code=0，根 reply_count-1 | 1 行软删 | count-1 | 1 条 | deleteCommentLogic.go |
| C-DEL-04 | RPC Logic 单元测试 | 重复删幂等 | status=2 | 再删 | stub | code=0 无消息 | 无 | 无 | 0 | deleteCommentLogic.go |
| C-DEL-05 | 并发测试 | 删根 vs 加子回复 | 真实 MySQL | 并发执行 | 真实 | 记录孤儿回复与计数偏差基线 | - | - | - | 竞态记录 |

### HTTP POST/DELETE /api/v1/comments/:commentId/like（评论点赞/取消，占位）

**当前行为**：两个 Logic 均不调用 RPC，恒返回 `ServerError("评论点赞功能暂未开放")`。代码依据：`likeCommentLogic.go:35`、`unlikeCommentLogic.go:33`。comments 表已有 like_count 列、`comment_hot` ZSet 以 like_count 为 score，但**点赞写入路径不存在**。

| 用例编号 | 测试层级 | 测试场景 | 前置条件 | 输入 | Mock 或真实依赖 | 预期响应 | 预期数据库变化 | 预期缓存变化 | 预期消息 | 代码依据 |
|---|---|---|---|---|---|---|---|---|---|---|
| C-LK-01 | Gateway Logic 单元测试 | 占位回归 | JWT 合法 | 任意 commentId | 无 | code=1，消息"评论点赞功能暂未开放" | 无 | 无 | 无 | likeCommentLogic.go:35 |

### RPC Comment 服务方法基线（7 个）

| 方法 | 关键行为 | 错误码 | 幂等 | 已有测试 | 建议层级 |
|---|---|---|---|---|---|
| CreateComment | 见 HTTP 节；事务写入+条件 INCR+CREATE 事件 | 2/13002/13004/13005/13006/1 | 无 | 有（createCommentLogic_test.go，含 MQ 失败降级） | 补 Model 事务集成 |
| DeleteComment | 软删/级联/reply_count 修正/DELETE 事件 | 2/13001/13003/1 | 重复删幂等 | 有（deleteCommentLogic_test.go） | 补并发竞态 |
| ListComments | 页码分页+预览 2 条+计数缓存+用户填充 | 2/1 | 只读 | 有 | 已覆盖 |
| ListReplies | created_at+id 游标正序 | 2/13001/1 | 只读 | 有（listRepliesLogic_test.go） | 已覆盖 |
| GetCommentCount | cache-aside `comment_count:{fid}` 1h（common.go:109-130） | 2/1 | 只读 | 有 | 已覆盖 |
| BatchGetCommentCount | 循环复用单个查询逻辑（**无网关调用方**，FeedCard 用镜像列） | 2/1 | 只读 | 有 | 已覆盖 |
| GetHotComments | `comment_hot:{fid}` ZSet 5min，miss 按 like_count 回源重建；Redis 异常降级直查 MySQL（getHotCommentsLogic.go:49-76） | 2/1 | 只读 | 有 | 已覆盖 |

Comment 已注册 ErrorInterceptor（`app/comment/rpc/comment.go:47`），13xxx 错误码可正确透传网关。

## Interaction 接口测试基线

Interaction 采用「**Redis 先行 + MQ 异步落库**」削峰模型：写接口只改 Redis 并发事件，DB 由 Worker 异步写入。Redis Key（`app/interaction/rpc/internal/keys/keys.go`）：

| Key | 结构 | TTL | 用途 |
|---|---|---|---|
| `like:feed:{feedID}` | Set(member=userID) | 7d | 帖子点赞用户集合 |
| `collect:feed:{feedID}` | Set | 7d | 帖子收藏用户集合 |
| `user:likes:{userID}` | ZSet(score=毫秒时间戳, member=feedID) | 30d | 用户点赞列表 |
| `user:collects:{userID}` | ZSet | 30d | 用户收藏列表 |
| `feed:stats:{feedID}` | Hash(like_count, collect_count) | 1h | 统计缓存 |

### HTTP POST/DELETE /api/v1/feeds/:feedId/like（点赞/取消点赞）

**基本信息**：Handler `interaction/likeFeedHandler.go`、`unlikeFeedHandler.go`；Logic `logic/interaction/likeFeedLogic.go`、`unlikeFeedLogic.go`；RPC `Interaction.LikeFeed`/`UnlikeFeed`，成功后 Gateway 再调 `GetFeedStats` 回查最新计数（失败降级 0）。需 JWT。

**请求参数**：Path feedId>0（Gateway 校验→2；RPC 二次校验 user_id/feed_id>0→2）。**不校验 feed 是否真实存在**（14001 未使用）——对任意 feedId 都可点赞成功，契约缺口/待确认。

**业务规则**（RPC `likeFeedLogic.go` + `interactionHelper.go`）：① 执行 **Lua 原子脚本**：SADD `like:feed:{fid}` + ZADD `user:likes:{uid}` + `feed:stats` 存在则 HINCRBY like_count +1 + 三 key 续期；脚本返回是否"首次点赞"→ ② **仅状态翻转时**发 `interaction-event`(action=1 Like)（重复点赞不发消息）→ ③ 返回成功。取消点赞对称（SREM/ZREM/HINCRBY -1，action=2）。

**成功响应**：`code=0`，data 含回查的 like_count/collect_count 与状态。

**数据副作用**：Redis 三 key 原子更新；MQ interaction-event → Worker upsert `likes` 表（唯一索引 `uk_user_feed(user_id,feed_id)`，status 1/2 翻转，`UpdateStatusIfNewer` 按事件时间戳防乱序）；**likes 表写入完全依赖消息成功投递**。

**异常场景**：

| 触发条件 | code | 副作用状态 |
|---|---|---|
| feedId<=0 | 2 | 无 |
| Redis Lua 失败 | 1（写接口不降级） | 无 |
| MQ 发送失败（异步回调仅日志） | **0 成功** | Redis 已改、**DB 永久缺记录**；7d/30d TTL 过期后点赞彻底丢失（R-P0-3） |
| 重复点赞 | 0 成功（幂等） | 无新副作用、不发消息 |
| 取消未点赞 | 0 成功 | 无 |

**幂等与并发**：Redis Set 天然幂等；Lua 保证单实例原子；DB 层唯一索引 + 时间戳防乱序。并发点赞/取消交错时以 Redis 结果为准，DB 靠 `UpdateStatusIfNewer` 收敛（毫秒同刻乱序仍可能错序，P2）。

**测试用例**：

| 用例编号 | 测试层级 | 测试场景 | 前置条件 | 输入 | Mock 或真实依赖 | 预期响应 | 预期数据库变化 | 预期缓存变化 | 预期消息 | 代码依据 |
|---|---|---|---|---|---|---|---|---|---|---|
| I-LK-01 | RPC Logic 单元测试 | 首次点赞 | miniredis 空 | like | miniredis+producer stub | code=0 | 无（异步） | Set/ZSet +1，stats 条件 +1 | Like 事件 1 条 | likeFeedLogic.go |
| I-LK-02 | RPC Logic 单元测试 | 重复点赞幂等 | 已点赞 | like | miniredis | code=0 | 无 | 无变化 | **0 条** | interactionHelper.go Lua |
| I-LK-03 | RPC Logic 单元测试 | 取消未点赞 | 空 | unlike | miniredis | code=0 | 无 | 无 | 0 条 | unlikeFeedLogic.go |
| I-LK-04 | RocketMQ 消费测试 | 落库+防乱序 | 真实 MQ+MySQL | Like 后 Unlike（乱序投递） | 真实 | - | likes 最终 status=2（时间戳新者胜） | - | 消费成功 | worker.go UpdateStatusIfNewer |
| I-LK-05 | 并发测试 | 100 goroutine 同帖点赞 | 真实 Redis | 并发 like | 真实 | SCARD=去重用户数；stats=SCARD | - | 一致 | 每用户 1 条 | 已有 concurrency_cache_test 参考 |
| I-LK-06 | 跨服务集成测试 | MQ 停机点赞 | broker 停 | like | 真实 | code=0，Redis 有、DB 无（P0 基线记录） | likes 缺行 | 已写 | 丢失 | R-P0-3 |

### HTTP POST/DELETE /api/v1/feeds/:feedId/collect（收藏/取消收藏）

与点赞完全对称：`collect:feed:{fid}`/`user:collects:{uid}`、collections 表（唯一索引 uk_user_feed）、action=3/4。Logic：`collectFeedLogic.go`/`uncollectFeedLogic.go`（gateway 与 rpc 同名文件）。测试用例参照 I-LK-01~06 平移（编号 I-CO-01~06），代码依据：`app/interaction/rpc/internal/logic/collectFeedLogic.go`、`worker.go`。

### HTTP GET /api/v1/users/me/likes、/api/v1/users/me/collects（我的点赞/收藏）

**基本信息**：Logic `myLikesLogic.go`/`myCollectsLogic.go`；RPC `Interaction.GetUserLikedFeeds`/`GetUserCollectedFeeds` → `Feed.BatchGetFeeds` → `aggregate.BuildFeedCards`。需 JWT。仅能查本人（user_id 取自 JWT）。

**业务规则**（RPC）：读 `user:likes:{uid}` ZSet 游标分页（`base64(score:feed_id)`）；**缓存 miss 时从 likes 表回源重建 ZSet（上限 1000 条）**——超过 1000 的历史点赞列表不完整（P2）；已删 feed 在 BatchGetFeeds 中被静默过滤，**列表可能短于 page_size 但 has_more 仍按 ZSet 计算**（记录基线）。

**测试用例**：

| 用例编号 | 测试层级 | 测试场景 | 前置条件 | 输入 | Mock 或真实依赖 | 预期响应 | 预期数据库变化 | 预期缓存变化 | 预期消息 | 代码依据 |
|---|---|---|---|---|---|---|---|---|---|---|
| I-ML-01 | RPC Logic 单元测试 | ZSet 游标翻页 | miniredis 预置 25 条 | 翻 3 页 | miniredis | 倒序、无重漏 | 无 | 无 | 无 | getUserLikedFeedsLogic.go（已有测试） |
| I-ML-02 | RPC Logic 单元测试 | miss 回源重建 | Redis 空、DB 有 status=1 行 | 第 1 页 | miniredis+stub | 返回 DB 数据 | 无 | ZSet 重建（≤1000） | 无 | 同上 |
| I-ML-03 | Gateway Logic 单元测试 | 已删帖过滤 | BatchGetFeeds 缺某 id | - | gomock | 列表跳过该帖仍 code=0 | 无 | 无 | 无 | myLikesLogic.go（已有测试） |

### RPC Interaction 服务方法基线（10 个）

| 方法 | 关键行为 | 错误码 | 幂等 | 已有测试 | 建议层级 |
|---|---|---|---|---|---|
| LikeFeed / UnlikeFeed | Lua 原子 + 翻转发事件 | 2/1 | Redis Set 幂等 | 有（likeFeedLogic_test.go 等） | 补 MQ 停机场景 |
| CollectFeed / UncollectFeed | 对称 | 2/1 | 同上 | 有（UncollectFeed 仅冒烟） | 补 Uncollect 细化断言 |
| GetFeedStats | `feed:stats:{fid}` Hash 1h，miss 时 SCARD 或 DB COUNT 回源重建 | 2/1 | 只读 | 有 | 已覆盖 |
| BatchGetFeedStats | 循环/管道复用单查 | 2/1 | 只读 | 有 | 补大批量断言 |
| GetUserInteractionStatus | SISMEMBER，key 不存在回源 likes/collections 表重建 Set | 2/1 | 只读 | 有 | 已覆盖 |
| BatchGetUserInteractionStatus | 批量状态 | 2/1 | 只读 | 有（未断言回填细节，弱测试） | 加强断言 |
| GetUserLikedFeeds / GetUserCollectedFeeds | ZSet 游标 + 1000 条回源上限 | 2/1 | 只读 | 有 | 补 >1000 边界 |

Interaction 已注册 ErrorInterceptor（`app/interaction/rpc/interaction.go:59`）。Worker：订阅 `interaction-event`（集群模式，无 Tag），按 action upsert likes/collections，处理 1062 为幂等更新，消费失败返回 error→`ConsumeRetryLater`（`common/mq/consumer.go:38`）；**去重依赖 uk_user_feed 唯一索引 + 时间戳条件更新，无 event_id 去重表**——重复消费安全（状态收敛），乱序毫秒同刻有微小窗口。

## Relation 接口测试基线

Relation Redis Key（logic 内定义，`app/relation/rpc/internal/logic/*.go`）：`user:follow:{uid}` ZSet(score=关注时间)、`user:fans:{uid}` ZSet、`user:fans_count:{uid}` String、`user:vip_users` Set（粉丝数≥阈值进入）。

### HTTP POST /api/v1/relations/follow（关注）

**基本信息**：Handler `relation/followHandler.go`；Logic `logic/relation/followLogic.go`；RPC `Relation.Follow`，成功后调 `GetFans(page=1,size=1)` 回查粉丝数（失败降级 0）。需 JWT。Body：`target_user_id` int64 必填（>0，且 Gateway 校验 ≠ 本人→11001 提前拦截）。

**业务规则**（RPC `followLogic.go`）：① user_id/target>0，自关→11001 → ② `FindOneByFollowerFollowed` 先查，已存在→**幂等成功** → ③ INSERT relations（唯一索引 `uk_follower_followed`；**捕获 1062 转幂等成功**，代码依据 `followLogic.go:51` 附近 MySQLError 判断）→ ④ **异步 goroutine** 更新 Redis：ZADD `user:follow:{uid}`/`user:fans:{tid}`、INCR `user:fans_count:{tid}`、达 VIP 阈值 SADD `user:vip_users`（失败仅日志）。**不校验目标用户是否存在**（11004 未使用）——可关注不存在的 user_id，契约缺口/待确认。

**数据副作用**：INSERT relations（物理行）；Redis 双 ZSet + 计数 + vip Set 异步更新；**无 MQ**。

**幂等与并发**：DB 层由唯一索引兜底不重复写；但**并发双击关注时**：两请求都可能走到"先查未命中"，一个 Insert 成功、另一个 1062 幂等成功——两者都可能已触发或将触发 Redis INCR？实际代码仅 Insert 成功分支执行 Redis 更新，1062 分支直接返回成功不再 INCR，**计数安全**；但"先查已存在"分支与 Unfollow 并发交错时 ZSet/计数与 DB 可能短暂不一致（异步 goroutine 无顺序保证，R-P1-6）。

**测试用例**：

| 用例编号 | 测试层级 | 测试场景 | 前置条件 | 输入 | Mock 或真实依赖 | 预期响应 | 预期数据库变化 | 预期缓存变化 | 预期消息 | 代码依据 |
|---|---|---|---|---|---|---|---|---|---|---|
| R-FL-01 | RPC Logic 单元测试 | 首次关注 | 无关系行 | 合法 target | model stub+miniredis | code=0 | INSERT relations | 双 ZSet+计数+1（等待异步收敛） | 无 | followLogic.go（已有测试） |
| R-FL-02 | RPC Logic 单元测试 | 重复关注幂等 | 已关注 | 同 target | stub | code=0，无二次 INSERT/INCR | 无 | 无 | 无 | followLogic.go |
| R-FL-03 | RPC Logic 单元测试 | 自关注 | - | target=self | stub | 11001 | 无 | 无 | 无 | followLogic.go |
| R-FL-04 | Model 集成测试 | 唯一索引 | 真实 MySQL | 双写同关系 | 真实 | 第二次 1062 | 仅 1 行 | - | - | deploy/sql/relation.sql uk_follower_followed |
| R-FL-05 | 并发测试 | 50 并发关注同一人 | 真实 MySQL+Redis | 并发 Follow | 真实 | 全部 code=0 | relations 1 行 | fans_count 最终=1（验证无虚高） | 无 | followLogic.go+集成已有 |
| R-FL-06 | RPC 集成测试 | 关注不存在用户 | target 无 users 行 | Follow | 真实 | code=0（基线记录，待确认） | INSERT 脏关系 | 计数+1 | 无 | 11004 未使用 |

### HTTP DELETE /api/v1/relations/follow（取消关注）

**基本信息**：Logic `logic/relation/unfollowLogic.go`；RPC `Relation.Unfollow`。Body `target_user_id`。

**业务规则**（RPC `unfollowLogic.go`）：先查关系，不存在→**幂等成功**（11003 未使用）；存在则 **物理 DELETE** relations 行；异步 Redis：ZREM 双 ZSet、DECR fans_count（**有下限保护逻辑请以代码为准：当前为直接 DECR，可能减至负数——待确认**）、粉丝数跌破阈值 SREM vip_users。

**测试用例**：

| 用例编号 | 测试层级 | 测试场景 | 前置条件 | 输入 | Mock 或真实依赖 | 预期响应 | 预期数据库变化 | 预期缓存变化 | 预期消息 | 代码依据 |
|---|---|---|---|---|---|---|---|---|---|---|
| R-UF-01 | RPC Logic 单元测试 | 正常取关 | 已关注 | target | stub+miniredis | code=0 | DELETE 1 行 | ZREM+DECR | 无 | unfollowLogic.go |
| R-UF-02 | RPC Logic 单元测试 | 取关未关注 | 无关系 | target | stub | code=0 幂等，无 DECR | 无 | 无 | 无 | unfollowLogic.go |
| R-UF-03 | 并发测试 | Follow/Unfollow 交错 | 真实环境 | 交替并发各 20 次 | 真实 | 终态 DB 与 Redis ZSet/计数一致（收敛窗口内轮询断言） | 一致 | 一致 | 无 | R-P1-6 |

### HTTP GET /api/v1/relations/following、/followers（关注/粉丝列表）

**基本信息**：Logic `followingListLogic.go`/`followerListLogic.go`；RPC `GetFollows`/`GetFans` + `User.BatchGetUsers` + `IsFollow`（回查我与列表成员关系）。Query：`user_id`（可选，默认本人）、`page`、`page_size`（clampPage 最大 50，`relation/helper.go:90`）。**任何登录用户可查任何人的关注/粉丝列表**（无隐私控制，待确认）。

**业务规则**（RPC）：优先 ZREVRANGE `user:follow:{uid}`/`user:fans:{uid}`，**Total=ZCard；缓存 miss 回源 MySQL 分页并重建 ZSet（重建有条数上限），回源分支 Total=len(本页)** ——Total 语义不一致（R-P1-7）。用户信息填充失败降级空对象；IsFollow 逐条查 DB（N+1，P2）。

**测试用例**：

| 用例编号 | 测试层级 | 测试场景 | 前置条件 | 输入 | Mock 或真实依赖 | 预期响应 | 预期数据库变化 | 预期缓存变化 | 预期消息 | 代码依据 |
|---|---|---|---|---|---|---|---|---|---|---|
| R-LS-01 | RPC Logic 单元测试 | 缓存命中分页 | miniredis 预置 ZSet | page=1,2 | miniredis+stub | 倒序、Total=ZCard | 无 | 无 | 无 | getFollowsLogic.go |
| R-LS-02 | RPC Logic 单元测试 | miss 回源重建 | Redis 空 | page=1 | miniredis+真实 stub | 数据正确；**Total=len(页)（基线缺陷记录）** | 无 | ZSet 重建 | 无 | R-P1-7 |
| R-LS-03 | Gateway Logic 单元测试 | is_following 回查+降级 | IsFollow mock 失败 | - | gomock | code=0，is_following=false | 无 | 无 | 无 | followingListLogic.go（已有测试） |

### HTTP GET /api/v1/relations/is-following（是否关注）

**基本信息**：Logic `isFollowingLogic.go`；RPC `Relation.IsFollow(user_id,[target])`。Query `target_user_id` 必填>0。RPC 侧逐 target 查 MySQL `FindOneByFollowerFollowed`（不走 Redis）。返回 `{is_following: bool}`。

| 用例编号 | 测试层级 | 测试场景 | 前置条件 | 输入 | Mock 或真实依赖 | 预期响应 | 预期数据库变化 | 预期缓存变化 | 预期消息 | 代码依据 |
|---|---|---|---|---|---|---|---|---|---|---|
| R-IF-01 | RPC Logic 单元测试 | 已关注/未关注/自己 | stub 关系行 | 三种 target | stub | true/false/false（自己是否算关注——以代码为准记录基线） | 无 | 无 | 无 | isFollowLogic.go |

### RPC Relation.IsVip

Feed 服务调用（CreateFeed 判大V、关注流拉 outbox）。逻辑：SISMEMBER `user:vip_users` → miss 时读 `user:fans_count` → 再 miss COUNT relations 回源并重建；粉丝数≥阈值（配置/常量，见 `isVipLogic.go`）即 VIP。已有集成测试覆盖（relation tests）。风险：fans_count 缓存漂移会导致大V判定抖动，间接影响 Feed 扩散模式（fanout vs 拉取）。

## RocketMQ 异步事件测试基线

### 公共基础设施行为

- **Producer**（`common/mq/producer.go`）：`SendSync` 方法名下**实际调用 `producer.SendAsync`**，回调中错误**仅 logx.Errorf 记录，不重试、不落补偿表、不影响调用方返回值**（`producer.go:32-38`）。全部业务事件均在 **DB 写入成功之后** 发送，无本地消息表/事务消息 → 任一事件都存在"DB 已变更但消息丢失"窗口（R-P0-2/R-P0-3）。
- **Consumer**（`common/mq/consumer.go`）：PushConsumer 集群模式；`MessageSelector{}` **无 Tag 过滤**；回调返回 error → `consumer.ConsumeRetryLater`（RocketMQ 默认最多 16 次后进 DLQ，重试次数未在代码显式配置——待确认 DLQ 监控方案）；返回 nil → `ConsumeSuccess`（`consumer.go:30-40`）。

### 事件 1：feed.created（Topic `feed-created`）

| 项 | 内容 |
|---|---|
| 消息结构 | `EventFeedCreate{event_id(UUID), event_type, feed_id, user_id, is_vip_feed, city_code, created_at(秒)}`，JSON（common/event/feed/event.go:21-29） |
| 业务唯一标识 | feed_id（event_id 未用于消费去重） |
| 生产方/时机 | Feed RPC CreateFeed，INSERT feeds 之后（createfeedlogic.go:102） |
| 消费方 | Feed Worker `handleFeedCreated`（worker.go:84-108） |
| 消费逻辑 | ZADD `outbox:{uid}`；ZADD `feed:recommend`；city_code 非空 ZADD `feed:city:{code}`；非大V：调 `Relation.GetFans` 分页拉全量粉丝，逐个 ZADD `inbox:{fanID}`（收件箱容量修剪）；大V 不推 inbox |
| 失败处理 | handler 返回 error→RetryLater；**JSON 反序列化失败返回 nil 直接吞消息**（worker.go:87 附近，P2）；GetFans 某页失败返回 error 整条重试 |
| 重复消费 | **无去重**。ZADD 幂等（同 member 覆盖 score）→ 副作用收敛，安全；但重试期间粉丝 inbox 部分写入+整条重试会重复拉取 GetFans（放大读） |
| 乱序风险 | 与 feed.deleted 乱序：先删后建时间线残留已删 feed_id，读端 status 过滤兜底 |
| 丢失风险 | **高**：发送即忘（R-P0-2），丢失后帖子永不进入任何时间线，且无对账 |

**测试用例**：

| 用例编号 | 测试层级 | 测试场景 | 前置条件 | 输入 | Mock 或真实依赖 | 预期响应 | 预期数据库变化 | 预期缓存变化 | 预期消息 | 代码依据 |
|---|---|---|---|---|---|---|---|---|---|---|
| E-FC-01 | RocketMQ 消费测试 | 非大V扩散 | 3 粉丝 | 消费 create 事件 | 真实 MQ+Redis+mock Relation | - | 无 | outbox/recommend/city +1，3 个 inbox +1 | Success | worker.go:84-108 |
| E-FC-02 | RocketMQ 消费测试 | 大V不推 inbox | is_vip_feed=true | 消费 | 同上 | - | 无 | inbox 不变 | Success | worker.go |
| E-FC-03 | RocketMQ 消费测试 | 重复消费收敛 | 同事件投 2 次 | 消费 | 真实 | - | 无 | ZSet 无重复 member | Success×2 | ZADD 幂等 |
| E-FC-04 | RocketMQ 消费测试 | 坏消息被吞 | body 非 JSON | 消费 | 真实 | - | 无 | 无 | **Success（吞掉，基线 P2）** | worker.go 反序列化分支 |
| E-FC-05 | RocketMQ 消费测试 | GetFans 失败重试 | Relation stub 报错 | 消费 | stub | - | 无 | 部分写入后整条 RetryLater | RetryLater | worker.go |

### 事件 2：feed.deleted（Topic `feed-deleted`）

结构 `EventFeedDeleted{event_id,event_type,feed_id,user_id,is_vip_feed,city_code}`（event.go:43-50）；生产于 DeleteFeed 软删后（deletefeedlogic.go:66）；消费 `handleFeedDeleted`（worker.go:115-160）：ZREM recommend/city/outbox；非大V 拉粉丝逐个 ZREM inbox。无去重（ZREM 幂等安全）；丢失后 ZSet 残留已删 id，读端过滤兜底但集合泄漏（P1 级清理缺失）。测试用例对称 E-FD-01~04（删除扩散/大V/重复/丢失残留验证）。

### 事件 3：comment-event（Topic `comment-event`，action=CREATE/DELETE）

| 项 | 内容 |
|---|---|
| 消息结构 | `Event{event_id(UUID), action_type, comment_id, feed_id, user_id, reply_user_id, parent_id, root_id, content_len, timestamp}`（common/event/comment/event.go:36-47）；DELETE 时 content_len=被删数量 |
| 生产方/时机 | Comment RPC 事务提交后（common.go:197），异步发送失败仅日志 |
| 消费方 | Feed Worker `handleCommentEvent`（worker.go:211-260） |
| 消费逻辑 | ① `SETNX comment_event:{event_id}` EX 24h 去重，已存在→Success 跳过 → ② CREATE：`feeds.comment_count+1`；DELETE：`-content_len`（UPDATE 带 GREATEST/下限保护以代码为准） |
| 幂等缺陷 | **SETNX 与 DB 更新非原子**：SETNX 成功后 DB 更新失败返回 error 重试，但重试时 SETNX 已存在→跳过 → **计数永久少加**（R-P1-8）；且 SETNX 本身失败时代码继续执行 → Redis 抖动时可能重复加 |
| 乱序/丢失 | CREATE/DELETE 乱序导致计数临时为负（有无下限保护待确认）；丢失→镜像计数永久偏差，无对账任务 |

**测试用例**：

| 用例编号 | 测试层级 | 测试场景 | 前置条件 | 输入 | Mock 或真实依赖 | 预期响应 | 预期数据库变化 | 预期缓存变化 | 预期消息 | 代码依据 |
|---|---|---|---|---|---|---|---|---|---|---|
| E-CE-01 | RocketMQ 消费测试 | CREATE +1 | feed 存在 | CREATE 事件 | 真实 MySQL+Redis | - | comment_count+1 | SETNX key 存在 | Success | worker.go:211-260 |
| E-CE-02 | RocketMQ 消费测试 | 同 event_id 去重 | 已消费 | 重投 | 真实 | - | 不再 +1 | - | Success | SETNX 去重 |
| E-CE-03 | RocketMQ 消费测试 | SETNX 后 DB 失败 | 模拟 DB 挂 | CREATE | 真实 Redis+故障注入 | - | **重试被去重跳过，计数丢失（P1 基线）** | key 已置 | RetryLater→Success | R-P1-8 |
| E-CE-04 | RocketMQ 消费测试 | DELETE 批量扣减 | count=5 | DELETE content_len=4 | 真实 | - | comment_count=1 | - | Success | worker.go |

### 事件 4：interaction-event（Topic `interaction-event`，action=1~4）

结构 `Event{event_id, user_id, feed_id, action_type, timestamp(毫秒)}`（common/event/interaction/event.go:29-35）；生产于 Redis Lua 状态翻转后（interactionHelper.go:448）；消费 Interaction Worker（worker.go:52 起）：Like/Collect→INSERT likes/collections（1062→按 `UpdateStatusIfNewer` 更新 status/时间戳）；Unlike/Uncollect→status=2 墓碑（同样带时间戳条件）。重复消费安全（唯一索引+条件更新）；毫秒同刻乱序有终态不确定窗口（P2）；**丢失=点赞/收藏永久缺失 DB 记录，Redis TTL 过期后数据蒸发**（R-P0-3）。

**测试用例**：E-IE-01 落库、E-IE-02 重复消费收敛、E-IE-03 乱序（旧时间戳事件后到不覆盖新状态）、E-IE-04 消息丢失后 TTL 过期数据蒸发演练（基线记录）。层级均为 `RocketMQ 消费测试`，代码依据 `app/interaction/rpc/internal/worker/worker.go`。

## 核心跨服务链路测试基线

### 链路 1：用户注册

入口 `POST /users/register` → Gateway registerLogic → User.Register → bcrypt → INSERT users（uk_username）→ jwtx 签发 → 返回 token。无 Redis/MQ。失败节点：参数(2)/重名(10001)/并发 1062(退化为 1)/DB(1)。无部分成功。测试：RPC Logic 单元（U-REG-01~03）+ Model 集成（U-REG-04）+ 并发（U-REG-05）+ 端到端（U-REG-06）。

### 链路 2：用户登录

入口 `POST /users/login` → User.Login → FindOneByUsername → status 检查 → bcrypt 比对 → JWT。只读无副作用。失败节点：10002/10005/1。测试：U-LOG-01~04。

### 链路 3：Feed 发布

入口 `POST /feeds` → Feed.CreateFeed → Relation.IsVip（降级）→ INSERT feeds → 发 feed-created → Worker：outbox/recommend/city/inbox ZADD。
- 数据库变化：feeds +1；Redis：4 类 ZSet；事件：feed-created。
- 失败节点：校验(经网关退化为 1)/IsVip 降级(大V误判)/DB(1)/MQ 丢失(帖子不可见)/Worker GetFans 失败(重试)。
- **部分成功存在**：DB 成功+MQ 丢失；Worker 部分粉丝 inbox 写入后重试。
- 测试：F-CRT 系列 + E-FC 系列 + 端到端 F-CRT-06。

### 链路 4：Feed 删除

入口 `DELETE /feeds/:feedId` → Gateway GetFeed 预检+属主校验(12002) → Feed.DeleteFeed（RPC 再校验）→ status=2 → DEL feed:{id} → 发 feed-deleted → Worker ZREM 各时间线。部分成功：软删成功+消息丢失→ZSet 残留（读端过滤兜底）。测试：F-DEL 系列 + E-FD 系列。

### 链路 5：时间线查询

入口 `GET /feeds/timeline` → Feed.Get{Recommend|Follow|City}Timeline（follow 流内部调 Relation.GetFollows/IsVip）→ Gateway BuildFeedCards：并行 User.BatchGetUsers + Interaction.BatchGetFeedStats + Interaction.BatchGetUserInteractionStatus（**Comment RPC 不参与**，评论数用 feeds 镜像列）→ FeedCard 列表。
- 失败节点：timeline RPC 失败→整体错误；三路聚合失败→各自降级（author=null/统计回退镜像/状态 false）。
- **允许部分数据缺失**（聚合降级是明确设计）。
- 测试：F-TL 系列 + 跨服务集成（真实三服务）+ 并发翻页 F-TL-06。

### 链路 6：评论发布与删除

入口 `POST /feeds/:feedId/comments`、`DELETE /comments/:id` → Comment RPC → Feed.GetFeed 存在性校验 → comments 事务写/级联软删 → Redis comment_count 条件增减 → comment-event → Feed Worker 更新 feeds.comment_count 镜像。
- 部分成功：事务成功+MQ 丢失→镜像计数偏差；SETNX 去重缺陷→重试丢计数。
- 测试：C-CRT/C-DEL 系列 + E-CE 系列 + 跨服务集成（评论后拉 timeline 验证 comment_count）。

### 链路 7：点赞/取消点赞

入口 `POST|DELETE /feeds/:feedId/like` → Interaction.LikeFeed/UnlikeFeed → Redis Lua（Set+ZSet+stats）→ 翻转时发 interaction-event → Interaction Worker upsert likes 表。
- **写路径 DB 最终一致完全依赖 MQ**；Redis 为唯一实时真源。
- 失败节点：Lua 失败(1)/MQ 丢失(DB 缺失，P0)/Worker 1062(幂等收敛)。
- 测试：I-LK 系列 + E-IE 系列 + 并发 I-LK-05 + 跨服务集成 I-LK-06。

### 链路 8：收藏/取消收藏

与链路 7 对称（collections 表、collect:feed/user:collects key、action 3/4）。测试 I-CO 系列。

### 链路 9：关注/取消关注

入口 `POST|DELETE /relations/follow` → Relation.Follow/Unfollow → relations 表 INSERT/DELETE（uk 兜底幂等）→ 异步 goroutine 更新 user:follow/user:fans ZSet + fans_count + vip_users。
- **无 MQ**；Redis 更新为进程内异步，服务崩溃即丢失（无重建对账，读路径 miss 回源部分兜底）。
- 关注数变化影响 IsVip → 影响 Feed 扩散模式（跨链路耦合）。
- 测试：R-FL/R-UF 系列 + 并发 R-UF-03 + 跨服务集成（关注后发帖验证 inbox 收到）。

## 现有测试覆盖审计

### 测试资产总量

约 34 个 `_test.go` 文件、约 140+ 测试函数。技术栈：testify 断言 + 自写 model stub / 接口替身 + miniredis + sqlmock（feed model）+ 真实基础设施集成测试（`app/*/rpc/tests/`，依赖 `etc/*-test.yaml`）。

### 按服务审计

| 服务 | 测试文件 | 依赖方式 | 覆盖情况 | 主要问题 |
|---|---|---|---|---|
| user | **无任何测试** | - | 0/5 RPC 方法 | 注册/登录核心链路零覆盖（P0 缺口） |
| relation | `internal/logic/followLogic_test.go`、`tests/integration_test.go`、`tests/concurrency_cache_test.go` | 单测：stub+miniredis；集成：真实 MySQL/Redis/etcd（relation-test.yaml） | Follow 单测（自关/首次/重复/1062）；集成覆盖 Follow/Unfollow/IsVip/GetFans 缓存一致性；并发测试验证计数收敛 | GetFollows/GetFans/IsFollow 无 Logic 单测；集成测试**无环境不可用 Skip 机制**，`go test ./...` 在无基础设施时失败；共享固定测试库 feed_relation_test |
| feed | `internal/logic/{getfeed,batchgetfeeds,getcitytimeline,getfollowtimeline,getrecommendtimeline,getuserfeeds}_logic_test.go`、`feed_logic_test.go`、`internal/worker/worker_test.go`、`model/feedsModel_test.go` | miniredis + model stub + sqlmock | 6/8 读方法覆盖较好（缓存命中/miss/游标/归并/软删过滤）；worker 测试覆盖 comment-event 计数与 SETNX 去重；model 测试用 sqlmock 验证 SQL | **CreateFeed / DeleteFeed Logic 无单测**（核心写路径零覆盖）；worker 对 feed-created/deleted 扩散逻辑覆盖弱；sqlmock 不验证真实索引行为 |
| comment | logic 下 7 个方法各有 `*_test.go`（stub+miniredis，覆盖参数矩阵、楼中楼推导、级联删、MQ 失败降级）、`tests/integration_test.go`（真实环境端到端） | stub+miniredis+真实集成 | 7/7 方法均有单测+集成 | 集成测试依赖本地固定环境；部分用例仅断言 err==nil 未验证 Redis 副作用（弱断言） |
| interaction | `internal/logic/*_test.go`（like/collect/stats/status/likedfeeds）、`worker/worker_test.go`、`tests/integration_test.go`、`tests/concurrency_cache_test.go` | miniredis+producer stub+真实集成 | 10 方法基本触达；并发测试（多 goroutine 点赞验证 SCARD 与 stats 一致）；worker 测试覆盖 1062 幂等 | UncollectFeed 仅冒烟；BatchGetUserInteractionStatus 未断言逐项回填（弱测试）；MQ 停机场景无测试 |
| gateway | `logic/aggregate/feedcard_test.go`、`logic/comment/listCommentsLogic_test.go`、`logic/feed/timelineLogic_test.go`、`logic/feed/deleteFeedLogic_test.go`、`logic/interaction/interactionlogic_test.go`、`logic/relation/relationlogic_test.go` | 自写 fake RPC client | 覆盖 FeedCard 聚合降级、timeline 分派、deleteFeed 属主校验、like/collect/mylikes、follow/列表/isfollowing | register/login/getUser/getMe/updateMe/uploadToken、createFeed、getFeedDetail、userFeeds、listReplies 等 **Gateway Logic 无测试**；无 Handler 层 httptest 测试（401 行为无回归） |
| common | jwtx/errorx/mq/response 无独立测试 | - | - | `[bizerror]` 编解码无回归测试 |

### 现有测试覆盖矩阵

| 服务 | 接口或方法 | 单元测试 | Model 测试 | 集成测试 | 并发测试 | 端到端测试 | 缺失场景 |
|---|---|---|---|---|---|---|---|
| user | Register/Login/GetUser/UpdateUser/BatchGetUsers | ✗ | ✗ | ✗ | ✗ | ✗ | 全部（含并发注册、brief 缓存） |
| relation | Follow | ✓ | ✗ | ✓ | ✓ | ✗ | 目标不存在场景 |
| relation | Unfollow | ✗ | ✗ | ✓ | ✓ | ✗ | 计数负数保护 |
| relation | GetFollows/GetFans | ✗ | ✗ | ✓(缓存一致性) | ✓ | ✗ | Total 语义、回源上限 |
| relation | IsFollow | ✗ | ✗ | ✗ | ✗ | ✗ | 全部 |
| relation | IsVip | ✗ | ✗ | ✓ | ✗ | ✗ | 阈值边界 |
| feed | CreateFeed | ✗ | ✗ | ✗ | ✗ | ✗ | 全部（校验矩阵/IsVip 降级/MQ 失败） |
| feed | DeleteFeed | ✗ | ✗ | ✗ | ✗ | ✗ | 全部（属主/幂等/缓存失效） |
| feed | GetFeed/BatchGetFeeds/4 个 Timeline | ✓ | ✓(sqlmock) | ✗ | ✗ | ✗ | Redis 故障降级、真实索引 |
| feed | Worker(comment-event) | ✓ | - | ✗ | ✗ | ✗ | feed-created/deleted 扩散、去重缺陷复现 |
| comment | 全部 7 方法 | ✓ | ✗ | ✓ | ✗ | 部分 | 删根 vs 加回复并发 |
| interaction | Like/Unlike/Collect/Stats/Status/LikedFeeds | ✓ | ✗ | ✓ | ✓ | ✗ | MQ 停机、>1000 回源 |
| interaction | UncollectFeed/BatchGetUserInteractionStatus | 冒烟/弱 | ✗ | ✓ | ✗ | ✗ | 细化断言 |
| interaction | Worker | ✓ | ✗ | ✓ | ✗ | ✗ | 乱序时间戳 |
| gateway | deleteFeed/timeline/聚合/comment/interaction/relation logic | ✓ | - | ✗ | ✗ | ✗ | 其余约 14 个 Logic 无测试；无 401 Handler 测试 |

**统计**：36 个 RPC 方法中约 26 个有测试触达；**完全无测试的约 10 个**：User 全部 5 个、Feed CreateFeed/DeleteFeed、Relation GetFollows/GetFans（单测）/IsFollow。

### 测试质量问题

1. **弱测试**：`BatchGetUserInteractionStatus` 未逐项断言回填；comment 集成部分用例只断言 err==nil 不查 Redis 副作用。
2. **环境耦合**：relation/interaction/comment 的 `tests/` 集成测试依赖本地 MySQL(3306)/Redis(6379)/etcd(2479)，多数缺少"不可用即 Skip"保护，`go test ./...` 无基础设施时失败。
3. **共享状态**：集成测试共用固定测试库与 Redis DB，靠 TestMain 清理，中途失败会污染后续执行，不可安全并行。
4. **命名与内容基本一致**，未发现明显不符；但 `feed_logic_test.go` 为多方法混合文件，可维护性差。

## 测试环境与测试数据设计

### 环境矩阵

| 层级 | 环境 | 说明 |
|---|---|---|
| 单元测试 | 无外部依赖 | model stub + miniredis + producer stub；`go test -race ./...` 必须全绿 |
| Model 集成 | 独立测试库 `feed_<svc>_test`（MySQL 8.0，docker-compose） | 执行 `deploy/sql/*.sql` 建表；每用例 TRUNCATE 隔离 |
| RPC/跨服务集成 | docker-compose 全套（MySQL/Redis/etcd:2479/RocketMQ 9876）+ `<svc>-test.yaml` | Redis 独立 DB 或 key 前缀；建议统一加"环境探活失败即 t.Skip"守护 |
| RocketMQ 消费测试 | 真实 namesrv+broker（autoCreateTopicEnable=true） | 独立 Topic 前缀/消费组隔离；副作用断言用轮询+超时 |
| 端到端 | 网关 8080 + 5 个 RPC 全启 | 真实 JWT；随机化测试数据防冲突 |

### 测试数据设计要点

- 用户：随机后缀 username（防 uk_username 冲突）；预置 status=0 禁用账号测 10005。
- Feed：type=1/2、city_code 有/无、大V 作者（粉丝数≥阈值）与普通作者各若干。
- 评论：三层结构（根→子→对子回复的回复）验证 root_id 展平；预置 status=2 数据验证过滤。
- 互动：同一 (user,feed) 的 Like→Unlike→Like 序列验证墓碑翻转与时间戳条件更新。
- 关系：互关/单向/自关注样本；VIP 粉丝数阈值边界（阈值-1、阈值）。
- 异步断言：MQ/异步 goroutine 副作用一律「轮询 + 最长收敛窗口（建议 5s）」，禁止固定 sleep。

## 高风险问题汇总

| 编号 | 等级 | 服务 | 涉及接口 | 风险描述 | 触发条件 | 可能后果 | 代码依据 | 建议测试 | 需改设计 |
|---|---|---|---|---|---|---|---|---|---|
| R-P0-1 | P0 | 全部 RPC | 所有写 RPC | RPC 服务无调用方认证，完全信任入参 user_id；权限校验仅存于 Gateway（或依赖入参比对） | 内网直连 9001-9005 端口伪造 user_id | 越权删帖/删评论/伪造关注点赞 | 各 RPC logic 无鉴权代码；`deletefeedlogic.go:47-52` 仅比对入参 | F-DEL-04 等 RPC 集成测试固化基线 | 是（mTLS/内网 ACL/网关签名透传） |
| R-P0-2 | P0 | feed | CreateFeed/DeleteFeed | DB 写后异步发消息，回调错误仅日志，无补偿/对账 | broker 抖动或停机 | 帖子永不进入时间线；删除后时间线残留 | `common/mq/producer.go:32-38`、`createfeedlogic.go:102` | F-CRT-04、E-FC 系列、MQ 停机演练 | 是（本地消息表/事务消息/对账任务） |
| R-P0-3 | P0 | interaction | Like/Collect 全部写接口 | Redis 先行成功即返回，DB 落库完全依赖消息；消息丢失后 Redis TTL(7d/30d) 过期数据蒸发 | MQ 发送失败 | 点赞/收藏永久丢失、统计错误 | `interactionHelper.go:448`、keys TTL 定义 | I-LK-06、E-IE-04 | 是（补偿对账） |
| R-P0-4 | P0 | user | Register | 并发同名注册时 Insert 1062 未兜底转 10001，且注册链路零测试 | 并发提交同名 | 错误码退化；核心链路无回归保障 | `registerLogic.go:52-58` | U-REG-04/05 | 建议兜底 1062 |
| R-P1-1 | P1 | feed | 全部经网关的 Feed 接口 | Feed 服务未注册 ErrorInterceptor，12xxx 业务码经网关全部退化为 code=1 | 任何 Feed 业务错误 | 客户端无法区分"帖子不存在/无权限/参数错"；与其余 4 服务行为不一致 | `app/feed/rpc/feed.go`（无 AddUnaryInterceptors）对比 `comment.go:47` | F-CRT-02、F-GET-04 固化基线 | 是（补拦截器） |
| R-P1-2 | P1 | user | Register | 同 R-P0-4 的错误码维度（并发下返回 1 而非 10001） | 并发注册 | 客户端重试逻辑失效 | registerLogic.go:52-58 | U-REG-05 | 是 |
| R-P1-3 | P1 | user | UpdateUser + BatchGetUsers | `user:brief:{id}` 快照 600s 不随资料更新失效 | 更新资料后 10 分钟内 | 时间线/评论区昵称头像陈旧 | batchGetUsersLogic.go（写入）；updateUserLogic.go（无删除） | U-UPD-03 | 是（更新时删 brief） |
| R-P1-4 | P1 | feed/comment | CreateFeed/CreateComment | 无幂等键，客户端重试产生重复内容 | 网络重试/双击 | 重复帖子/评论 | 无幂等代码 | F-CRT-07 | 待产品确认 |
| R-P1-5 | P1 | feed/interaction | Timeline 聚合 | feeds.like_count/collect_count 镜像列无任何更新来源；聚合降级时统计恒 0 | Interaction RPC 故障 | 列表页点赞收藏数错误显示 0 | feeds 表列 vs 全仓库无 UPDATE like_count 语句；`feedcard.go` 降级分支 | F-TL-03 | 是（镜像同步或去掉降级列） |
| R-P1-6 | P1 | relation | Follow/Unfollow | Redis 更新为进程内异步 goroutine，无顺序/持久化保证；崩溃即丢 | 高并发关注取关交错、服务重启 | ZSet/fans_count 与 DB 漂移，影响 IsVip 判定与 Feed 扩散模式 | followLogic.go 异步分支 | R-UF-03 并发收敛测试 | 是 |
| R-P1-7 | P1 | relation | GetFollows/GetFans | Total 语义分裂：缓存命中=ZCard，回源=len(当前页)；Gateway 用其当粉丝数 | 缓存 miss | 主页粉丝数/关注数错误 | getFansLogic.go 回源分支；gateway getUserLogic.go | R-LS-02 | 是 |
| R-P1-8 | P1 | feed worker | comment-event 消费 | SETNX 去重先于 DB 更新且非原子：DB 失败重试被去重跳过→计数永久少加 | 消费时 DB 抖动 | comment_count 镜像永久偏差 | worker.go:211-260 | E-CE-03 | 是（去重放 DB 成功后或用事务表） |
| R-P1-9 | P1 | comment | DeleteComment | 删根级联与新增子回复并发竞态，孤儿回复与计数偏差 | 并发删根+回复 | 计数不准 | deleteCommentLogic.go 事务范围 | C-DEL-05 | 待评估 |
| R-P1-10 | P1 | gateway/user | JWT | AccessSecret 硬编码于 yaml 且两处需手工同步，违反 secrets env-only | 配置泄漏/不同步 | 认证被伪造或全线 401 | gateway.yaml:55、user.yaml:33 | 配置审计用例 | 是（环境变量注入） |
| R-P2-1 | P2 | gateway | GET /users/me | ctx 无 user_id 时返回 (nil,nil)→code=0,data=null 静默成功 | 中间件异常 | 客户端拿到空用户 | getMeLogic.go | U-ME-02 | 建议返回 3 |
| R-P2-2 | P2 | feed worker | feed-created 消费 | JSON 反序列化失败返回 nil 吞消息，无死信记录 | 脏消息 | 静默丢事件 | worker.go 反序列化分支 | E-FC-04 | 建议告警 |
| R-P2-3 | P2 | gateway | 全部 | request_id 响应字段恒为空（无注入中间件） | 始终 | 排障困难 | response.go:34-45 | 冒烟断言 | 是 |
| R-P2-4 | P2 | interaction | GetUserLiked/CollectedFeeds | 缓存回源重建上限 1000 条，超出历史不可见 | 重度用户 | 列表不完整 | getUserLikedFeedsLogic.go 回源上限 | I-ML 补边界 | 待确认 |
| R-P2-5 | P2 | relation/gateway | IsFollow、列表 | IsFollow 逐条查 DB（N+1），列表页放大 | 大 page_size | 延迟升高 | isFollowLogic.go | 性能基准 | 优化项 |
| R-P2-6 | P2 | 多服务 | 分页 | Gateway(50)/RPC(100) 上限不一致、页码与游标混用 | 直连 RPC 或极端参数 | 行为不一致 | feedcard.go:213、common.go:34-37 | 参数矩阵用例 | 文档统一 |

**汇总：P0 4 项、P1 10 项、P2 6 项。**

## 待确认事项

| # | 涉及接口 | 代码当前行为 | 需确认问题 | 影响的测试用例 |
|---|---|---|---|---|
| Q1 | POST /feeds/:id/like、collect | 不校验 feed 存在性（14001 未使用），任意 feedId 可点赞 | 是否需要存在性校验？ | I-LK-01/06、E-IE 系列预期 |
| Q2 | POST /relations/follow | 不校验目标用户存在（11004 未使用） | 关注不存在用户应成功还是 11004？ | R-FL-06 |
| Q3 | 重复点赞/收藏/关注/取关 | 一律幂等 code=0（11002/11003 未使用） | 幂等成功是否为最终产品语义？ | I-LK-02/03、R-FL-02、R-UF-02 |
| Q4 | DELETE /feeds/:id、/comments/:id | 软删幂等成功 | 删除不存在资源（非已删）返回 12001/13001 还是幂等成功？当前：不存在→错误、已删→成功 | F-DEL-03、C-DEL-04 |
| Q5 | PATCH /users/me | 空字符串=不更新，无法清空字段；nickname/bio 无长度校验 | 清空语义与长度上限？ | U-UPD-02/04 |
| Q6 | title/nickname 等文本字段 | 待确认：当前代码未定义业务层最大长度（仅 DB varchar 兜底报 code=1） | 各字段业务上限？ | U-REG-03、F-CRT-02 |
| Q7 | GET /feeds/timeline (recommend) | Redis miss 返回空列表不回源 MySQL | 推荐池为空是否允许空响应（冷启动）？ | F-TL 系列预期 |
| Q8 | 读接口 Redis 故障 | GetHotComments 降级直查 DB；GetRecommendTimeline 返回错误/空——各接口行为不一 | Redis 故障时读接口统一降级策略？ | 各读接口故障注入用例 |
| Q9 | 全部写接口 | MQ 发送失败主业务仍成功 | 是否可接受最终不一致？补偿方案？ | F-CRT-04、I-LK-06、E-CE-03 |
| Q10 | comment-event DELETE | 计数扣减是否带下限保护待确认（乱序可能负数） | comment_count 允许负数吗？ | E-CE-04 |
| Q11 | GET /feeds/timeline | 页码型游标（recommend/city）在持续写入下重复/漏数据 | 时间线排序稳定性与重复容忍度？ | F-TL-06 |
| Q12 | user:fans DECR | Unfollow 直接 DECR，可能负数（待确认代码有无下限） | 计数负数保护？ | R-UF-01 |
| Q13 | GET /relations/following 等 | 任何登录用户可查任何人关注/粉丝/帖子列表 | 是否需要隐私控制？ | R-LS、F-UF-02 |
| Q14 | POST /upload/token | 恒 10006 占位 | 上传方案（对象存储/凭证格式）？ | U-UPT-01 |
| Q15 | RocketMQ 消费 | 重试次数未显式配置（默认 16 次后 DLQ），无 DLQ 监控 | DLQ 处理策略？ | E-* 重试用例 |
| Q16 | DELETE /comments/:id | 帖子作者不能删除他人评论 | 是否需要帖主删评权限？ | C-DEL-01 |

## 测试实施优先级

### P0（第一优先，本迭代必须）

1. **User 服务全套测试从零补齐**：`app/user/rpc/internal/logic/registerLogic_test.go`、`loginLogic_test.go`、`getUserLogic_test.go`、`updateUserLogic_test.go`、`batchGetUsersLogic_test.go`（stub+miniredis）；`app/user/rpc/tests/integration_test.go`（真实 MySQL，含并发同名注册 U-REG-05）。
2. **Feed 写路径单测**：`app/feed/rpc/internal/logic/createfeedlogic_test.go`（校验矩阵/IsVip 降级/MQ 失败）、`deletefeedlogic_test.go`（属主/幂等/缓存 DEL/事件）。
3. **JWT/认证回归**：`app/gateway/internal/handler/auth_handler_test.go`（httptest：无 token/过期/伪造→401；合法→user_id 透传）；`common/jwtx/jwtx_test.go`。
4. **RPC 越权基线固化**：`app/feed/rpc/tests/authz_baseline_test.go`（直连伪造 user_id 删帖，F-DEL-04，作为 R-P0-1 的红线记录）。
5. **消息一致性演练**：`app/interaction/rpc/tests/mq_outage_test.go`（I-LK-06）、`app/feed/rpc/tests/mq_outage_test.go`（F-CRT-04 集成版）；`common/errorx/grpc_test.go`（`[bizerror]` 编解码回归）。
6. **唯一索引 Model 集成**：users(uk_username)、relations(uk_follower_followed)、likes/collections(uk_user_feed) 的 1062 行为测试。

### P1（第二优先）

1. Gateway 缺失 Logic 单测：register/login/getUser/getMe/updateMe、createFeed、getFeedDetail、userFeeds、listReplies。
2. FeedCard 聚合补充：统计降级=镜像 0 值（R-P1-5 基线）、comment_count 数据源断言。
3. comment-event 去重缺陷复现：`app/feed/rpc/internal/worker/worker_dedup_test.go`（E-CE-03）。
4. Relation Total 语义与回源：`getFansLogic_test.go`（R-LS-02）；Follow/Unfollow 交错并发收敛（R-UF-03）。
5. Interaction 弱测试加强：BatchGetUserInteractionStatus 逐项断言、UncollectFeed 细化、>1000 回源边界。
6. 集成测试加环境探活 Skip 守护，使 `go test ./...` 在无基础设施时可全绿。

### P2（第三优先）

1. 占位接口回归（uploadToken、评论点赞）与空数组/null 格式冒烟。
2. 分页参数矩阵（两层上限不一致基线）、坏游标用例。
3. IsFollow N+1 与时间线性能基准（ghz/hey 脚本扩展）。
4. request_id 空值、日志字段冒烟；文档差异核对用例。

## 后续自动化测试实施建议

1. **分层流水线**：PR 门禁跑单元层（`go test -race` 全部 stub/miniredis，目标 <2min）；合并后跑 docker-compose 集成层（Model/RPC/MQ 消费）；夜间跑端到端+并发+MQ 停机演练。
2. **统一测试基建**：抽 `testutil` 包提供：环境探活 Skip、随机数据工厂、异步轮询断言（waitFor）、docker-compose 生命周期辅助，消除现有集成测试的固定环境耦合与共享状态。
3. **契约测试**：基于 `.api`/`.proto` 生成请求样例做参数矩阵回归，锁住 Gateway(50)/RPC(100) 分页上限、错误码映射（尤其 Feed 服务补拦截器前后的 code 变化）。
4. **一致性对账测试**：定期比对 Redis（like Set/fans ZSet/comment_count）与 MySQL 终态，作为 R-P0-2/3、R-P1-8 的长期监控手段。
5. **HTTP 自动化**：在 `scripts/` 增加基于真实网关的 API 冒烟集（注册→登录→发帖→点赞→评论→关注→timeline→删除），产出可重复执行的回归基线。

（以上仅为建议清单，本次未创建任何测试文件。）






