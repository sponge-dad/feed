# 头像上传后数据库未更新

> 浏览器修改头像并保存成功，但 MySQL `users.avatar` 字段未变化；根因为 COS 桶 CORS 未放行浏览器直传（B 类配置问题）。

---

## 1. 基本信息

- 编号：BUG-20260731-001
- 日期：2026-07-31
- 报告人：开发者
- 状态：已定位（B 类，控制台配置即可修复，无需改代码）
- 影响范围：所有通过前端改头像 / 发带媒体帖子的用户（本地开发 + 潜在生产）；不影响服务端直传（`uptest`）。
- 类别：B 配置问题（CORS）

## 2. 现象与复现

- 现象：前端「编辑资料」上传头像，页面提示保存成功，但刷新后头像未变；直接查库 `users.avatar` 为空或仍为旧值。
- 触发条件：浏览器（Origin `http://localhost:5173` 或 `http://127.0.0.1:5173`）直传 COS。
- 环境：本地开发，gateway 运行于 `8080`，前端 vite dev 于 `5173`。
- 复现步骤：
  1. 浏览器打开 `http://localhost:5173`，进入编辑资料。
  2. 选择头像图片 → 上传 → 保存。
  3. 查 `feed_user.users.avatar`，发现未更新。

## 3. 排查过程（时间线）

- **假设 1**：后端 `updateMe` 落库逻辑有问题。
  - 验证：读 `app/gateway/internal/logic/updateMeLogic.go`、`uploadTokenLogic.go`、`cosGuard.go`、`app/user/rpc/internal/logic/updateUserLogic.go` 及 user model 的 `Update`。字段映射 `Avatar`、路由 `patch /users/me`、归属校验 `CanonicalizeCosRef` 均匹配，未见落库缺陷。
- **假设 2**：前端未把 `avatar` 带回 `updateMe`。
  - 验证：读 `feed-web/src/api/user.ts`（`updateMe` 发 PATCH 带 `avatar`）、`feed-web/src/pages/EditProfilePage.tsx`（`setAvatar(res.file_url)` 仅在直传成功后执行，保存时才带 `avatar`）。逻辑上若直传成功则应带回。
- **假设 3**：浏览器直传 COS 被 CORS 拦截。
  - 实证：写临时程序（服务端用永久密钥直传 COS 成功），注册用户 → 真实 PUT 对象 → 调 `PATCH /users/me` 带回 `file_url` → 查 MySQL，`avatar` 已成功写入完整 URL：
    `https://feed-1250000000-1317318750.cos.ap-guangzhou.myqcloud.com/dev/avatar/<uid>/20260731/<snowflake>.png`
    → 证明后端链路完全正常，问题在浏览器直传环节。
  - **关键区分**：`uptest` 是服务端用永久密钥直传，不经过浏览器，故成功不代表前端浏览器直传成功；二者不可相提并论。

## 4. 根因

- **直接原因**：浏览器对 `*.myqcloud.com` 发起 PUT 预检被 COS 桶 CORS 策略拒绝，直传失败 → `uploadFile` 抛错 → 弹「头像上传失败」→ `avatar` 保持旧值 → 保存时不带 `avatar` 字段 → 库不动。
- **根本原因**：COS 桶「跨域访问 CORS 设置」未对前端 Origin 放行 PUT 方法及必要请求头；认知偏差在于把服务端 `uptest` 直传成功误认为前端也能成功。
- **证据**：
  - 端到端实证：服务端直传 + `updateMe` 后 `users.avatar` 成功写入（见上方 URL）。
  - 前端链路：`EditProfilePage.tsx` 中 `setAvatar(res.file_url)` 仅在直传成功后执行。
  - 网关：`updateMeLogic.go` 经 `CanonicalizeCosRef` 校验 `file_url` 归属后落库（逻辑正常）。

## 5. 处置方案（B 类：配置清单，不改代码）

控制台路径：**对象存储 COS → 存储桶 `feed-1250000000-1317318750` → 安全管理 → 跨域访问 CORS 设置 → 添加规则**：

- [ ] **来源 Origin**：`http://localhost:5173` 和 `http://127.0.0.1:5173`（二者都加，取决于浏览器地址栏；生产替换为正式域名）
- [ ] **操作 Methods**：`PUT`、`POST`、`GET`、`HEAD`
- [ ] **Allow-Headers**：`*`（开发期；SDK 会带 `Authorization` / `x-cos-*` / `Content-Type`）
- [ ] **Expose-Headers**：`ETag`、`x-cos-request-id`
- [ ] **超时 Max-Age**：`600`

保存即时生效，**无需重启任何服务**。

> 生产建议：Allow-Headers 收紧为 `Authorization,Content-Type,Content-Length,x-cos-*`；Origin 仅留正式域名。

## 6. 验证与收敛

- **验证方法**：
  - 浏览器 F12 → Network，筛选到 `*.myqcloud.com` 的 PUT 请求：成功应为 200/204；被拦则标红 `(failed)` 且 Console 报 `blocked by CORS policy`。
  - 确认不再弹「头像上传失败」；保存后查 `users.avatar` 已更新。
- **回归关注点**：
  - 切换 `localhost` ↔ `127.0.0.1` 打开页面时，CORS Origin 需覆盖两者。
  - 生产部署时 CORS Origin 必须改为正式域名，否则前端直传再次失败。
  - 发带图 / 视频帖子同样走此浏览器直传链路，受同一 CORS 约束。

## 关联文档

- [Bug 总结 SOP](../agent/bug-summary-sop.md)
- [OSS 设计总览](../design/oss/00-overview.md)
- 相关代码：`app/gateway/internal/logic/updateMeLogic.go`、`app/gateway/internal/logic/uploadTokenLogic.go`、`app/gateway/internal/pkg/cos/cos.go`、`feed-web/src/pages/EditProfilePage.tsx`、`feed-web/src/utils/upload.ts`
