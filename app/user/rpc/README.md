# User RPC 服务

用户注册/登录/信息管理的 gRPC 服务，是 Feed 系统的基础身份服务，其他服务（Relation/Feed/
Comment/Interaction）通过 `BatchGetUsers` 获取用户简要信息用于展示，不直接访问 users 表。

> 完整的设计背景见 `../../../docs/design/service-design.md`（服务职责划分）和 `../../../docs/design/data-model.md`
> （数据模型），本文档只总结**这个服务实现层面用到的技术点**，供后续开发同类服务参照。

## 1. 服务信息

| 项 | 值 |
|---|---|
| 端口 | 9001 |
| 契约 | `api/proto/user/user.proto` |
| 建表脚本 | `deploy/sql/user.sql` |
| 配置文件 | `etc/user.yaml` |

## 2. 目录结构与职责

```
app/user/
├── model/                          # MySQL 数据访问层
│   ├── usersmodel_gen.go           # goctl 生成，禁止手动改
│   ├── usersmodel.go               # 手写扩展点：补充了 FindByIds 批量查询
│   └── vars.go                     # ErrNotFound 定义
└── rpc/
    ├── etc/user.yaml               # 运行配置：端口/MySQL/Redis/JWT
    ├── internal/
    │   ├── config/config.go        # 配置结构体，手动扩展了 Mysql/CacheRedis/Auth
    │   ├── svc/serviceContext.go   # 依赖注入：挂载 UserModel/Redis/JwtManager
    │   ├── logic/                  # 业务逻辑（本服务核心）
    │   │   ├── registerLogic.go
    │   │   ├── loginLogic.go
    │   │   ├── getUserLogic.go
    │   │   ├── updateUserLogic.go
    │   │   └── batchGetUsersLogic.go
    │   └── server/userServer.go    # goctl 生成，转发到 logic，不手动改
    ├── user/                       # protoc 生成的 pb 代码，不手动改
    ├── userClient/                 # 供其他服务调用本服务的 gRPC 客户端封装
    └── user.go                     # 服务启动入口
```

## 3. 各接口实现要点

### 3.1 Register（注册）

- **查重**：`UserModel.FindOneByUsername` 判断用户名是否已存在，命中直接返回
  `errorx.UserExists`，避免把"用户已存在"误判成系统错误。
- **密码加密**：`bcrypt.GenerateFromPassword`（`DefaultCost`），不存明文/MD5。
- **ID 生成**：`idgen.Next()`（Snowflake），不用 MySQL 自增 —— 详见
  `deploy/sql/user.sql` 头部注释，为将来分库分表铺路。
- **注册即登录**：成功后直接 `JwtManager.Generate` 签发 token 一起返回，不需要
  用户再单独调用一次登录。

### 3.2 Login（登录）

- **防用户名枚举**：用户名不存在和密码错误返回**同一个错误码**
  （`errorx.UserPasswordWrong`），避免攻击者通过错误信息差异判断哪些用户名已注册。
- **密码校验**：`bcrypt.CompareHashAndPassword`。
- **账号状态检查**：`status != 1`（被禁用）直接拒绝登录。

### 3.3 GetUser（查询用户）

- **直接复用 model 层内置缓存**：goctl 用 `-c` 参数生成的 `UserModel.FindOne` 内部
  已经实现了完整的 Cache-Aside（查Redis未命中查MySQL再回写），logic 层**不需要**
  再手写一层业务缓存，直接调用即可。这是本服务里最容易被误解、多写一层的地方。

### 3.4 UpdateUser（更新资料）

- **部分字段更新**：proto 约定空字符串表示"不更新该字段"，逐字段判断后覆盖。
- **缓存失效**：`UserModel.Update` 内部会自动清理该记录在各唯一索引维度上的缓存
  key（先写库后删缓存），logic 层不需要手动 `DEL`。

### 3.5 BatchGetUsers（批量查询，避免 N+1）

这是专门为跨服务聚合场景设计的接口，技术点最集中：

- **批量读缓存**：`Redis.MgetCtx` 一次 `MGET` 查所有 key，不循环 `GET`。
- **批量查库兜底**：缓存未命中的 ID 收集后调用自定义的 `UserModel.FindByIds`
  （内部是一条 `WHERE id IN (?,?,...)`），**绝不**在 for 循环里调 `FindOne`。
- **业务级缓存独立于 model 层内置缓存**：`user:brief:{id}` 这个 key 是本方法自己
  维护的（10分钟 TTL），和 model 层按主键维护的缓存是两套不同的缓存，因为批量场景
  命中率、更新频率、数据形态（完整 Users vs 精简 UserBrief）都不一样，没有直接复用。
- **按需回写缓存失败不影响主流程**：Redis 读写失败只记日志降级处理，不阻断整个
  RPC 调用返回结果。

## 4. 依赖注入设计（ServiceContext）

```go
type ServiceContext struct {
    Config     config.Config
    UserModel  model.UsersModel   // MySQL CRUD + 内置缓存
    Redis      *redis.Redis       // 供 logic 层手写业务级缓存（如批量场景）
    JwtManager *jwtx.Manager      // 签发/校验登录 token
}
```

新增一个依赖（比如以后接 RocketMQ 生产者）的标准步骤：
1. `config.go` 加配置字段
2. `serviceContext.go` 的 `NewServiceContext` 里用配置初始化
3. 挂到 `ServiceContext` 结构体字段上
4. logic 里通过 `l.svcCtx.XXX` 访问

## 5. 错误处理

所有业务错误统一走 `common/errorx`，不用裸 `errors.New`。当前 User 服务用到的错误码
（10000~10999 段）：

| 错误码 | 场景 |
|---|---|
| `UserExists` | 注册时用户名已存在 |
| `UserNotFound` | 查询/更新时用户不存在 |
| `UserPasswordWrong` | 登录时用户名不存在或密码错误（统一返回，防枚举） |
| `UserDisabled` | 账号被禁用 |

`model.ErrNotFound` 用于区分"查询未找到"和"数据库真的出错"，logic 层用
`errors.Is(err, model.ErrNotFound)` 判断，转换成对应业务码，不当系统错误处理。

## 6. 已知的设计取舍 / 待办

- `RegisterResp`/`LoginResp` 里的 `CreatedAt` 字段：MySQL `DEFAULT CURRENT_TIMESTAMP`
  生成的时间不会被 `Insert` 回填到 Go 结构体，Register 场景用 `time.Now()` 近似代替，
  没有为此多发一次查询。Login/GetUser/UpdateUser 场景因为是先 `FindOne` 出完整记录，
  时间是准确的库内值。
- `email`/`phone` 字段当前只建了表和唯一索引，Register/Login 均未启用，为后续
  手机号登录/找回密码预留。
- 尚未接入 RocketMQ：按 `../../../docs/design/service-design.md` 的规划，用户注册成功后应该发一条
  `user.registered` 事件（供推荐系统冷启动、风控等下游订阅），本服务当前版本未实现。

## 7. 本地运行

```bash
# 1. 建库建表（先改好 etc/user.yaml 里的 DSN）
mysql -h127.0.0.1 -uroot -p < ../../../deploy/sql/user.sql

# 2. 启动服务
go run user.go -f etc/user.yaml
```
