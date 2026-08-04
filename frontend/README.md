# feed-web

基于 `backend-contract/` 后端契约生成的 Feed 前端（React 18 + TypeScript + Vite + axios + Zustand）。

## 快速开始

```bash
npm install
npm run dev        # http://localhost:5173
```

## 联调配置

- Base URL 固定 `/api/v1`，开发环境由 Vite dev server 代理到网关（无需后端开 CORS）。
- 修改网关地址：编辑 `.env` 中的 `VITE_GATEWAY_TARGET`（默认 `http://127.0.0.1:8080`），改完重启 dev server。

```
VITE_GATEWAY_TARGET=http://<网关IP>:8080
```

## 对接约定（实现依据：`*.api` 为权威契约）

| 项 | 实现 |
|----|------|
| 统一响应 | `{ code, message, data, request_id }`；`code === 0` 取 `data`，非 0 用 `message` toast |
| HTTP 401 | 响应体为空，单独拦截：清 token → 跳 `/login?redirect=...` |
| 鉴权 | 除 `/users/register`、`/users/login` 外均带 `Authorization: Bearer <token>` |
| 分页 | 信息流/评论为 cursor 分页（`cursor`/`page_size`/`list`/`next_cursor`/`has_more`）；关注/粉丝为 offset 分页（`page`/`page_size`/`total`），均按 `*.api` 实现 |
| 时间 | 毫秒级 unix 时间戳，`new Date(ms)` 本地化（`src/utils/time.ts`） |
| 上传 | 两步分离：`POST /upload/token` 拿凭证 → 前端直传 → URL 随业务接口提交（`src/utils/upload.ts`） |
| 类型 | TS 接口字段名与 `*.api` json tag 一致（snake_case），见 `src/types/` |

## 目录结构

```
src/
├── types/            # 与 *.api 类型 1:1 对应的 TS 接口
├── api/              # request.ts（拦截器）+ user/relation/feed/comment/interaction
├── store/auth.ts     # token/用户信息持久化（zustand persist）
├── hooks/useCursorList.ts  # cursor 无限滚动
├── utils/            # time.ts / upload.ts / toast.ts
├── components/       # Layout / FeedCardGrid / Avatar
└── pages/            # 登录/注册/首页信息流/帖子详情/个人主页/关注粉丝/我的赞收藏/发布
```

## 页面路由

| 路由 | 页面 |
|------|------|
| `/login`、`/register` | 登录 / 注册（公开） |
| `/` | 首页信息流（推荐/关注/同城） |
| `/feeds/:feedId` | 帖子详情 + 评论/回复/点赞/收藏 |
| `/users/:userId` | 个人主页（帖子列表、关注操作） |
| `/users/:userId/relations/following|followers` | 关注 / 粉丝列表 |
| `/me/likes`、`/me/collects` | 我的赞 / 我的收藏 |
| `/publish` | 发布帖子（上传 + 提交） |

## 安全说明

- token 存 localStorage；全站渲染只走 React 文本节点/`textContent`，不使用 `dangerouslySetInnerHTML`，规避 XSS 窃取。
- `.env` 只放公开配置（网关地址），无任何密钥；上传使用后端下发的临时凭证。

## 待确认与联调清单

见交付说明中的「待后端确认清单」与「前端联调 Checklist」。
