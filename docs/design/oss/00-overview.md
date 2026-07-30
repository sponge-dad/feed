# 对象存储（腾讯云 COS）设计

> 本文档定义 Feed 项目静态资源（头像、图文、视频、封面）的对象存储方案与实现指南，涵盖 Bucket 规划、key 命名、STS 临时凭证签发、私有桶签名访问、CDN 策略与安全防护。

---

## 1. 概述与定位

Feed 是类抖音/小红书的 UGC 内容平台，静态资源（用户头像、帖子图文/视频、视频封面）具有**体量大、读多写少、视频文件大**的特点。这类资源不适合存进 MySQL/Redis，统一存放在对象存储。

本项目采用**腾讯云对象存储 COS（Cloud Object Storage）**，并通过**客户端直传 + STS 临时凭证**模式：客户端先向后端换取临时上传凭证，再直传 COS，最后把文件 URL 回写给业务接口。

边界：

- 对象存储只存"文件本身"与"可访问 URL"，不存业务元数据（宽高、时长等）。
- 后端（API Gateway）不代理文件字节流，视频大文件由客户端直传，不占网关带宽。
- 封面图/视频转码由 COS 数据万象（CI）或异步任务处理，不在本文档主流程（见 §8 演进）。

## 2. 架构与职责

```
 ┌─────────┐  1. POST /upload/token (file_type,file_ext)   ┌──────────────┐
 │ Client  │ ────────────────────────────────────────────>│ API Gateway  │
 └─────────┘                                               │ (UploadToken)│
      │                                                     └──────┬───────┘
      │ 2. STS 临时凭证 (tmpSecretId/Key,sessionToken,fileKey)    │ 3. AssumeRole
      │<──────────────────────────────────────────────────────────┘
      │                                                                  │
      │ 4. 直传 PUT object (带临时凭证)                                    │ 4'. STS 服务
      │ ─────────────────────────────────────────────────────────────> ┌──────────────┐
      │                                                                  │ 腾讯云 STS  │
      │ 5. 拿到 file_url                                                   │ (CAM)        │
      │<───────────────────────────────────────────────────────────────  └──────────────┘
      │
      │ 6. PATCH /users/me 或 POST /feeds (body 带 file_url)
      │ ──────────────────────────────────────────────────────────────> 业务 RPC
      │
      │ 后续读取：客户端用 签名 URL / CDN 域名 访问 COS 私有文件
```

组件职责：

- **Client**：APP/Web 前端，负责拿凭证、直传、回写 URL。
- **API Gateway**：唯一与 COS 凭证相关的后端入口；签发 STS 临时凭证、生成 `file_key`、返回 `file_url`；不触碰文件字节。
- **腾讯云 STS（CAM）**：根据后端主账号密钥，签发限定前缀的临时访问凭证。
- **COS Bucket**：私有桶，存原始文件；通过签名 URL 对外提供读访问。
- **业务 RPC（User/Feed）**：只持久化 URL 字符串（`users.avatar`、`feeds.media_urls`、`feeds.cover_url`），不感知存储细节。

## 3. 数据模型与 Key 规范

### 3.1 file_key 命名规范

统一格式：

```
{env}/{biz}/{uid}/{yyyyMMdd}/{snowflake}.{ext}
```

| 段 | 含义 | 取值 |
|----|------|------|
| `env` | 环境 | `dev` / `test` / `prod` |
| `biz` | 业务类型 | `avatar` / `feed-image` / `feed-video` / `feed-cover` |
| `uid` | 用户 ID（Snowflake） | 由网关从 JWT 解析，不可信客户端传入 |
| `yyyyMMdd` | 上传日期 | 服务端按 UTC+8 生成 |
| `snowflake` | 文件唯一 ID | `common/idgen` 生成，避免碰撞与遍历 |
| `ext` | 扩展名 | 来自 `file_ext`，小写、白名单校验 |

示例：`prod/avatar/10001/20260730/1893726490032648193.jpg`

约定：

- 业务类型与扩展名必须白名单校验（见 §6.6），防止任意路径写入。
- `uid` 取自登录态，**客户端传入的 key 不可信**，必须由网关统一生成 `file_key` 后回传。
- 按 `uid` + 日期分目录，便于按用户/时间维度清理与审计。

### 3.2 URL 落库约定

| 字段 | 表 | 类型 | 说明 |
|------|----|------|------|
| `avatar` | `users` | VARCHAR(512) | 头像 URL（COS 签名 URL 或 CDN 地址） |
| `media_urls` | `feeds` | JSON | 图文/视频 URL 数组 `["url1","url2"]` |
| `cover_url` | `feeds` | VARCHAR(512) | 视频封面 URL；图文可为空 |

业务服务只存 URL 字符串，不存 key；下载时由网关/客户端按私有桶策略构造签名 URL（见 §6.5）。

## 4. 接口与契约

对外 REST（详见 [api-spec/user.md](../api-spec/user.md) §6）：

```
POST /api/v1/upload/token
```

请求：

```json
{ "file_type": "image", "file_ext": "jpg" }
```

- `file_type`：`image` | `video`，用于选择 `biz` 前缀与大小限制。
- `file_ext`：扩展名，需白名单校验。

响应：

```json
{
  "upload_url": "https://feed-1250000000.cos.ap-guangzhou.myqcloud.com",
  "credentials": {
    "tmp_secret_id": "...",
    "tmp_secret_key": "...",
    "session_token": "...",
    "expired_time": 1750000000
  },
  "file_key": "prod/avatar/10001/20260730/1893726490032648193.jpg",
  "file_url": "https://feed-1250000000.cos.ap-guangzhou.myqcloud.com/prod/avatar/10001/20260730/1893726490032648193.jpg"
}
```

字段说明：

- `upload_url`：Bucket 访问域名，客户端上传目标。
- `credentials`：STS 临时密钥；有效期短（默认 1h，视频大文件可放宽到 2h）。
- `file_key`：网关生成的对象键，客户端上传时必须使用，且不可更改为其他前缀。
- `file_url`：上传成功后对象的可访问地址（私有桶需再签名为临时 URL 才能访问，见 §6.5）。

当前状态：`UploadTokenLogic` 目前为占位实现（`app/gateway/internal/logic/uploadTokenLogic.go` 返回 `errorx.UploadTokenFail`），需按 §6.4 落地。

## 5. 错误码

| 码 | 含义 | 触发 |
|----|------|------|
| `10006` | 获取上传凭证失败 | STS 调用异常 / 配置缺失（见 `common/errorx`） |

客户端拿到该错误应提示"上传凭证获取失败，请重试"，不应暴露底层异常。

## 6. 访问策略与实现指南

### 6.1 总体策略：私有桶 + 签名 URL

- Bucket 设为**私有读写**，禁止公有读。
- 上传：客户端持 STS 临时凭证直传（临时凭证已限定到 `{env}/{biz}/{uid}/` 前缀，无法越权写他人目录）。
- 下载/展示：所有读访问走**临时签名 URL**（COS 预签名），URL 带过期时间（如图片 10 分钟、视频按播放会话 1 小时），避免永久链接被爬取。

### 6.2 配置项

密钥**只允许来自环境变量/配置中心，禁止硬编码**（见 [AGENTS.md](../../AGENTS.md) §6.7）。在 `app/gateway/internal/config/config.go` 的 `Config` 中新增：

```go
// Cos 腾讯云对象存储配置。
Cos struct {
    Bucket       string `json:",optional"` // 桶名，如 feed-1250000000
    Region       string `json:",optional"` // ap-guangzhou
    SecretId     string `json:",optional"` // 主账号/子账号 SecretId（来自环境变量）
    SecretKey    string `json:",optional"` // 主账号/子账号 SecretKey（来自环境变量）
    Env          string `json:",optional"` // 环境标识 dev/test/prod，用于 file_key 前缀
    StsDuration  int64  `json:",optional"` // STS 临时凭证有效期(秒)，默认 3600
    SignDuration int64  `json:",optional"` // 下载签名 URL 有效期(秒)，默认 600
    BaseURL      string `json:",optional"` // 对外访问域名（私有桶用 bucket 域名或 CDN 域名）
} `json:",optional"`
```

> 生产环境 `SecretId/SecretKey` 通过环境变量注入（如 `COS_SECRET_ID` / `COS_SECRET_KEY`），YAML 中不写明文。

### 6.3 依赖与 SDK

- STS 临时凭证：`github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/sts/v20180813`（调用 `GetFederationToken` / `AssumeRole`）。
- 签名 URL：`github.com/tencentyun/cos-go-sdk-v5` 的 `ObjectGetSignature` / `GetPresignedURL`。

### 6.4 Gateway 实现（UploadTokenLogic）

落地步骤：

1. 从 `ctx` 解析 `user_id`（JWT 已校验），拒绝未登录。
2. 校验 `file_type` / `file_ext` 白名单（`image`→jpg/jpeg/png/webp/gif；`video`→mp4/mov/webm），不合规返回 `errorx.ParamError`。
3. 生成 `file_key`：`{env}/{biz}/{uid}/{yyyyMMdd}/{snowflake}.{ext}`，其中 `env` 来自配置，`snowflake` 用 `common/idgen`。
4. 调用 STS 申请**限定前缀**的临时凭证（policy 限定 `cos:PutObject` 到该 `file_key` 前缀）。
5. 组装 `UploadTokenResp` 返回。

示意（需补全错误处理与日志）：

```go
func (l *UploadTokenLogic) UploadToken(req *types.UploadTokenReq) (*types.UploadTokenResp, error) {
    uid := jwtx.ParseUID(l.ctx) // 实际取自 jwtx 解析结果，不信任客户端入参
    if !allowedExt(req.FileType, req.FileExt) {
        return nil, errorx.New(errorx.ParamError)
    }
    biz := bizOf(req.FileType) // avatar / feed-image / feed-video / feed-cover
    day := time.Now().In(cst).Format("20060102")
    key := fmt.Sprintf("%s/%s/%d/%s/%d.%s",
        l.svcCtx.Config.Cos.Env, biz, uid, day, idgen.Gen(), strings.ToLower(req.FileExt))

    cred, err := sts.New(l.svcCtx.Config.Cos).Issue(key) // 限定前缀的临时凭证
    if err != nil {
        logx.Errorf("sts issue fail: %v", err)
        return nil, errorx.NewWithMsg(errorx.UploadTokenFail, "获取上传凭证失败")
    }
    base := l.svcCtx.Config.Cos.BaseURL
    return &types.UploadTokenResp{
        UploadURL: base,
        Credentials: types.UploadCredentials{
            TmpSecretID:  cred.TmpSecretId,
            TmpSecretKey: cred.TmpSecretKey,
            SessionToken: cred.SessionToken,
            ExpiredTime:  cred.ExpiredTime,
        },
        FileKey: key,
        FileURL: base + "/" + key,
    }, nil
}
```

### 6.5 下载签名 URL 生成

读路径（如头像、视频播放）由网关提供独立接口（或聚合层）生成临时签名 URL，避免把私有桶文件直接暴露：

```go
// 使用 cos-go-sdk-v5 生成 GET 预签名 URL
presigned, err := cosClient.ObjectGetSignature(ctx, key, time.Duration(signDur)*time.Second)
```

> 高频读场景（如头像）可结合 CDN：将 CDN 回源到私有桶，CDN 开启「时间戳防盗链」，前端用带鉴权参数的 CDN 地址访问；详略见 §8。

### 6.6 安全防护清单

- 桶私有，禁止 `public-read`。
- STS 临时凭证必须**限定 resource 前缀**（用户只能写自己的目录），失效时间短。
- `file_key` 由服务端生成，客户端不可指定；`uid` 取自登录态而非入参。
- 扩展名/文件类型白名单；视频限制大小上限（如 200MB），图片限制（如 20MB）。
- 密钥仅来自环境变量；含密钥的 YAML 不入库（`.gitignore` 排除）。
- 上传后建议做**服务端校验**：可选 COS 上传回调，或在业务写入时校验 URL 归属前缀，防止客户端伪造他人 URL。
- 防刷：对 `/upload/token` 做用户级限流（网关已有限流能力）。

## 7. 缓存与一致性

- COS 对象本身无需缓存；下载通过**签名 URL 有效期**天然控频。
- 若启用 CDN，缓存以 `Cache-Control` / CDN 节点 TTL 为准；更新头像等场景通过**变更 URL（新 snowflake key）**而非覆盖旧文件来规避缓存陈旧。
- 业务表里的 URL 是"写后不变"的字符串，无缓存一致性问题；删除资源时仅逻辑删除 URL，物理文件可异步清理。

## 8. 演进与 TODO

- [ ] `UploadTokenLogic` 占位实现需按 §6.4 落地（接 STS SDK）。
- [ ] 新增下载签名 URL 接口（头像/视频播放）。
- [ ] 视频/大图转码、缩略图、截图：引入 COS 数据万象（CI），上传完成后异步处理，输出多分辨率/多尺寸 URL。
- [ ] 高频读场景接入 CDN + 时间戳防盗链，降低签名 URL 计算开销。
- [ ] 孤儿文件清理：定期扫描 COS 前缀，清理未被任何业务记录引用的文件。
- [ ] 修正 `architecture.md` 中 "Object Storage (MinIO)" 的描述，统一为腾讯云 COS（见 §关联文档）。

## 关联文档

- [架构设计](../architecture.md)
- [数据模型](../data-model.md)
- [API 契约总览](../api-spec/README.md)
- [用户 API 契约](../api-spec/user.md)
- [Feed API 契约](../api-spec/feed.md)
- [项目总规范](../../AGENTS.md)
