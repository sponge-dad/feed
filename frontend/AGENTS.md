# AGENTS.md — 给 AI 协作者的编码指南

本文件供 CodeBuddy / AI agent 在本仓库工作时参考。人类开发者请同时阅读 `README.md` 与 `DESIGN.md`。

## 项目一句话

`feed-web` 是一个 **React 18 + TypeScript + Vite** 的 Feed（信息流）前端，后端契约（`*.api`）是权威来源，前端据此生成类型与接口。

## 技术栈（不要随意升级主版本）

- 构建：Vite 5 + `@vitejs/plugin-react`
- UI：React 18（`BrowserRouter`、`zustand@5` 状态管理、`react-router-dom@6`）
- 请求：`axios@1` + `json-bigint`（关键，见下）
- 上传：`cos-js-sdk-v5`（腾讯云对象存储直传）
- 类型与运行时均为 TS 严格模式（`tsconfig.app.json`）

## 常用命令

```bash
npm install            # 安装依赖（需要 Node.js，当前环境若缺失需先安装）
npm run dev            # 启动 dev server：http://localhost:5173
npm run build          # tsc -b && vite build（类型检查 + 打包）
npm run preview        # 预览产物
```

> 注意：当前环境缺少 npm / Node.js，运行任何命令前需先安装 Node.js（见 README 顶部提示）。不要臆造环境。

## 联调与代理

- 所有请求 baseURL 固定 `/api/v1`，开发环境由 Vite dev server 代理转发到网关，**不要在前端处理 CORS**。
- 网关地址：`vite.config.ts` 取 `VITE_GATEWAY_TARGET`，默认 `http://127.0.0.1:8080`，也可用 `.env` 覆盖。
- 前端自测：`VITE_USE_MOCK=true` 时由 `src/mock/index.ts` 的 axios adapter 直接返回契约数据，无需后端。

## 必须严格遵守的硬约束（违反即出 bug）

### 1. snowflake ID 大整数保护（最高优先级）

后端 ID 为 int64（≈1.8e18），**超出 JS 安全整数 2^53**。务必：

- 响应解析统一走 `src/api/request.ts` 的 `transformResponse`（`json-bigint` + `storeAsString=true`），**不要**绕开 `request`/`http` 自行 `fetch`/`JSON.parse`。
- 超出安全范围的大整数会被解析为**字符串 ID**，可直接用于 `===` 比较与拼接 URL（如 `/users/${userId}`）。
- **不要**把 ID 当 number 做加减或存回需数值比较的地方。

### 2. 绝对禁止 XSS

- 全程使用 React 文本节点 / `textContent` 渲染，服务端字符串**绝不**进入 `dangerouslySetInnerHTML`，**绝不** `eval`。
- toast 已用 `textContent` 实现（`src/utils/toast.ts`），沿用即可。

### 3. 统一定义与拦截

- 所有接口返回体被响应拦截器拆成 `{ code, message, data, request_id }`：`code===0` 时调用方拿到的是 **`data`**（不是外层包裹），非 0 已统一 `toast` 并 `reject`。
- HTTP 401（响应体为空）被单独拦截：清 token → 跳 `/login?redirect=...`，**不要再在业务层处理登录失效**。
- token 存 localStorage（`zustand persist`，key=`feed-auth`），仅持久化 token，user 每次启动由 `/users/me` 刷新（见 `src/store/auth.ts` 的 `merge`/`partialize` 注释）。

## 目录约定（新增代码按此落地）

```
src/
├── types/      # 与 *.api 类型 1:1 对应，字段名用 snake_case（与服务端 json tag 一致）
├── api/        # request.ts（唯一入口）+ 按领域拆分（user/feed/comment/relation/interaction）
├── store/      # 仅 auth.ts（token + user）
├── hooks/      # useCursorList.ts（cursor 无限滚动）
├── utils/      # time / upload / signUrl / toast
├── components/ # Layout / FeedCardGrid / Avatar（纯展示，无路由）
├── pages/      # 页面（与路由一一对应）
└── mock/       # 自测用 axios adapter
```

## 新增一个 API 的步骤

1. 在 `src/types/` 对应领域文件加 TS 接口（snake_case，与服务端 json tag 对齐）。
2. 在 `src/api/` 对应文件用 `http.get/post/patch/delete` 声明函数，返回类型写 `data` 的形状。
3. 在页面/组件中调用，错误提示已由拦截器统一处理，**业务层只 catch 静默忽略或做状态回滚**即可。
4. 若用 mock 自测，在 `src/mock/index.ts` 补一条响应。

## 新增一个页面的步骤

1. 在 `src/pages/` 建 `<Name>Page.tsx`。
2. 在 `src/App.tsx` 注册路由；需登录的页面用 `<RequireAuth>` 包裹。
3. 列表页优先复用 `useCursorList`（cursor 分页）或 offset 分页（关注/粉丝）。

## 分页两种形态（务必区分）

- **cursor 分页**（信息流/评论/用户帖子）：`{ list, next_cursor, has_more }`，首屏 cursor 传空串，下一页传 `next_cursor`。用 `useCursorList`。
- **offset 分页**（关注/粉丝）：`{ list, page, page_size, total }`，按 `page` 自增。

## 时间处理

后端给毫秒级 unix 时间戳（int64）。本地化用 `src/utils/time.ts`（`formatTime` / `formatRelative`），**不要**在前端做时区换算假定。

## 上传与签名（COS）

- 两步分离：`POST /upload/token` 拿临时凭证 → 前端 `cos-js-sdk-v5` 直传 → URL（file_key/file_url）随业务接口提交。见 `src/utils/upload.ts`。
- 私有 COS 资源用 `src/utils/signUrl.ts` 的 `getSignedUrl` 换临时可访问 URL（按 file_key 缓存，已签名地址直接复用）。

## 命名与风格

- 路径别名 `@` → `./src`（见 `vite.config.ts`），import 优先用 `@/...`。
- 组件 PascalCase，hook `useXxx`，工具/常量 camelCase。
- 保持函数式组件 + hooks；状态优先放 `zustand` 或组件内 `useState`，列表分页状态放 `useCursorList`。

## 提交前自检

- `npm run build` 通过（含 TS 严格类型检查）。
- 不引入 `dangerouslySetInnerHTML` / `eval`。
- 不新增对 `*.api` 契约之外的字段假设；TS 字段名与 json tag 一致。
- 不在 `.env` 放任何密钥（仅网关地址）；密钥一律后端下发临时凭证。

---

## 前端设计约束（必须遵守）

> 视觉与交互规范以 `DESIGN.md` 为唯一入口。以下为红线摘要：

- **Design Token 唯一来源**：`src/styles.css` 的 `:root`。颜色 / 字号 / 间距 / 圆角 / 边框 / 阴影 / z-index 必须引用 `--token`，**禁止业务代码硬编码十六进制 / 任意数值**。
- **复用优先**：按钮用 `.btn`、输入用 `.input`/`.textarea`、卡片用 `.card`、标签用 `.tabs`、标题用 `.section-title`、文字色用 `.text-brand/.text-secondary/.text-muted`。新增页面前先搜索这些 class 与 `components/` 公共组件。
- **交互状态完整**：按钮/链接须有 hover / active / focus-visible / disabled；异步按钮 disabled + 文案切换；危险操作二次确认。
- **三态齐全**：每个列表/数据页必须有加载中、空数据、错误（网络异常由拦截器 toast，局部需重试入口）。
- **响应式**：至少验证桌面与 ≤560px 移动端；feed 网格在 ≤560px 转单列。
- **可访问性**：用 `<button>/<a>` 而非 `<span onClick>`；图片 `alt`；禁止仅用颜色表达错误；支持 `prefers-reduced-motion`。
- **禁止**：引入新 UI 库 / 新样式方案 / 混用多套 Token；为单页改全局样式；大量 `!important`；无语义 `z-index`。

## AI 编码流程（实现任何前端页面 / 组件之前）

1. 阅读 `AGENTS.md`。
2. 阅读 `DESIGN.md` 与相关 Design Token。
3. 检查 `components/` 与 `styles.css` 现有公共组件 / class。
4. 检查现有 Design Token 是否覆盖需求。
5. 检查相邻页面的实现方式（保持一致性）。
6. 确认**不会重复实现**已有组件 / class。
7. 确认**不会引入新的视觉规则**（新字号/间距/圆角/颜色须先在 `DESIGN.md` 与 `styles.css` 登记 Token）。
8. 确认**不会修改无关业务代码**。

## AI 编码流程（完成编码之后）

1. 执行格式化（`npm run format`）。
2. 执行 ESLint（`npm run lint`）。
3. 执行 TypeScript 类型检查（`npm run typecheck`）。
4. 执行项目构建（`npm run build`）。
5. 执行已有测试（如有）。
6. 检查桌面端与移动端（≤560px）布局。
7. 检查加载 / 空数据 / 错误 / 禁用状态。
8. 检查是否出现新的硬编码颜色、字号、间距、圆角。
9. 输出本次修改的文件清单。
10. 说明是否新增了设计规则、Token 或公共组件。

## 修改边界（最小范围原则）

- 优先进行**最小范围修改**，不得为完成一个页面而大规模重构项目。
- 不得删除现有功能、不得改变现有接口结构、不得修改无关路由。
- 不得擅自更换 UI 组件库、不得擅自升级核心依赖、不得同时混用多种样式方案。
- **新增规则 / Token / 公共组件必须同步更新 `DESIGN.md`**，且公共组件须有明确复用场景（登记到 DESIGN.md 组件清单）。
