<!--
Sync Impact Report:
  Version: 0.1.0 → 1.0.0
  All principles newly ratified based on AGENTS.md, DESIGN.md, and existing codebase.
  No previous version existed — template was blank.
-->

# Feed 前端项目宪法 (feed-web)

> **feed-web** 是 Feed 系统的 Web 前端，基于 React 18 + TypeScript + Vite 5 构建。
> 本宪法定义前端项目不可妥协的核心原则、设计约束与治理规则。所有 UI/UX 变更、组件开发、AI 协作必须遵循本宪法。

## 一、核心原则

### I. Design Token 唯一来源 (Token-Driven Design — NON-NEGOTIABLE)

- 所有视觉值（颜色、字号、间距、圆角、边框、阴影、z-index、动效）必须以 `src/styles.css` 中 `:root` 定义的 CSS 变量为**唯一来源**。
- **绝对禁止**在业务代码中硬编码十六进制颜色（`#e5484d`）、任意像素值（`margin: 7px`）、无 Token 的字号/圆角。
- 动态样式（`style={{ flex: 1 }}`）允许，但颜色/字号必须引用 `var(--token)`。
- 新增视觉规则必须先在 `DESIGN.md` 登记 Token，再在 `styles.css` 实现，最后在业务代码引用。

### II. 组件复用优先 (Component Reuse — No Duplication)

- 新增页面/功能前，**必须先搜索** `components/` 公共组件和 `styles.css` 已有 class（`.btn` / `.input` / `.card` / `.tabs` / `.section-title` / `.text-brand` / `.fab` 等）。
- **禁止页面内重复实现**已有公共组件或等价样式 class。
- 以下逻辑**必须提取为公共组件**：跨页复用的卡片/行、空数据/错误/加载态、确认弹窗。
- 不应提取：仅单页使用一次的局部结构、纯布局 wrapper。
- 公共组件新增必须登记到 `DESIGN.md` 第 10 节，且必须有明确复用场景。

### III. 前端安全底线 (Frontend Security — NON-NEGOTIABLE)

1. **Snowflake ID 大整数保护（最高优先级）**：后端 ID 为 int64（≈1.8e18），超出 JS 安全整数 2^53。响应解析必须走 `src/api/request.ts` 的 `transformResponse`（`json-bigint` + `storeAsString=true`）。**禁止绕开 `request`/`http` 自行 `fetch`/`JSON.parse`**。
2. **XSS 防护**：全程使用 React 文本节点 / `textContent` 渲染，**禁止** `dangerouslySetInnerHTML`，**禁止** `eval`。
3. **密钥保护**：不在 `.env` 存放任何密钥（仅网关地址）；密钥一律由后端下发临时凭证。
4. **认证拦截**：HTTP 401 由请求拦截器统一处理（清 token → 跳转登录），业务层不得重复处理登录失效。
5. **用户输入**：所有用户生成内容通过 React 默认转义渲染，不在业务层手写 HTML 拼接。

### IV. 三态齐全 (State Completeness)

每个列表/数据页面**必须同时实现**三种状态，否则不算完成：

- **加载中** (Loading)：骨架或加载文案，防白屏。
- **空数据** (Empty)：明确提示「暂无数据」，非空白页。
- **错误** (Error)：网络异常由拦截器统一 toast；局部加载失败必须提供重试入口。
- 异步按钮必须 `disabled` + 文案切换（如「发布中…」），防重复提交。
- 危险操作（删除）必须二次确认（未来走 `ConfirmDialog`，当前 `confirm()` 为已知技术债）。

### V. 响应式设计 (Responsive Design)

- 所有新页面必须至少验证**桌面端（> 560px）和移动端（≤ 560px）**两种宽度。
- Feed 网格：≤ 560px 强制单列（`auto-fit minmax(220px, 1fr)` 自动适配）。
- 禁止仅适配桌面而忽略移动端布局。
- 图片比例统一：封面 4:3、头像 1:1 圆形，使用 `aspect-ratio` + `object-fit: cover`。

### VI. 可访问性 (Accessibility)

- 语义化 HTML：交互元素用 `<button>` / `<a>`，**禁止** `<span onClick>` 替代（已有代码为已知技术债，新代码不得新增）。
- 键盘可操作：所有交互元素可 Tab 聚焦，`focus-visible` 可见。
- 图片必须设置 `alt`（装饰图 `alt=""`，内容图给出描述）。
- 禁止仅用颜色表达错误：必须配合文字提示。
- 动效必须包裹 `@media (prefers-reduced-motion: reduce)` 禁用。

## 二、技术栈约束

| 类别 | 必须使用 | 禁止/限制 |
|------|---------|----------|
| 框架 | React 18 + TypeScript (严格模式) | 不混用 Vue / Angular / Svelte |
| 构建 | Vite 5 | 不引入 Webpack |
| 路由 | react-router-dom v6 | -- |
| 状态管理 | zustand v5 | 不引入 Redux / MobX |
| HTTP | axios v1 + json-bigint | 不绕开 `src/api/request.ts` 自行请求 |
| 上传 | cos-js-sdk-v5 (腾讯云 COS 直传) | 不通过后端中转大文件 |
| 样式 | **单一全局 CSS** (`src/styles.css`) | **禁止**引入 Tailwind / SCSS / CSS Modules / 第三方 UI 库 |
| 代码质量 | ESLint + Prettier + TypeScript strict | -- |
| 路径别名 | `@/` → `./src` | 不写深层相对路径 `../../..` |

- **不擅自升级核心依赖主版本**（React 19、Vite 6 等）。
- **不引入任何新 UI 组件库或样式方案**。

## 三、开发工作流

### 3.1 新增页面流程

1. 在 `src/pages/` 创建 `<Name>Page.tsx`
2. 在 `src/App.tsx` 注册路由；需登录页面用 `<RequireAuth>` 包裹
3. 列表页优先复用 `useCursorList`（cursor 分页）或 offset 分页
4. 搜索现有 `components/` 和 `styles.css` class，**不重复造轮子**

### 3.2 新增 API 流程

1. 在 `src/types/` 对应领域文件加 TS 接口（字段名用 snake_case，与服务端 json tag 一致）
2. 在 `src/api/` 用 `http.get/post/patch/delete` 声明函数
3. 业务层错误提示由拦截器统一处理，**只 catch 做静默降级或状态回滚**

### 3.3 分页规范

- **cursor 分页**（信息流/评论/用户帖子）：`{list, next_cursor, has_more}`，用 `useCursorList`
- **offset 分页**（关注/粉丝）：`{list, page, page_size, total}`，按 `page` 自增
- **两者不可混用**：同一列表只选一种分页策略

### 3.4 提交前自检

```bash
npm run build    # tsc -b && vite build（类型检查 + 打包）
npm run lint     # ESLint
npm run format   # Prettier
```

必须全部通过。`npm run build` 失败 = 提交被拒。

### 3.5 页面完成定义 (Definition of Done)

- [ ] 复用现有布局组件 + Design Token（**零硬编码**颜色/字号/间距/圆角）
- [ ] 优先复用公共组件/class
- [ ] 桌面 + 移动端（≤ 560px）布局验证
- [ ] 加载态 + 空数据态 + 错误态
- [ ] 表单：校验 + 文字错误提示 + 提交 loading
- [ ] 危险操作：二次确认
- [ ] 键盘可操作 + `focus-visible`
- [ ] ESLint 通过 + `npm run build` 通过
- [ ] 不影响现有业务功能和路由

## 四、禁止事项（AI 编码红线）

- ❌ 硬编码颜色（hex/rgb/hsl）、字号、间距、圆角
- ❌ 擅自新增无 DESIGN.md 登记的视觉规则
- ❌ 重复实现已有公共组件或 class
- ❌ 引入新 UI 库 / 新样式方案 / 混用多套 Token
- ❌ 为单页面修改全局样式
- ❌ 大量 `!important`（仅 `prefers-reduced-motion` 复位可用）
- ❌ 无语义 `z-index` 数值
- ❌ 未实现加载/空/错误三态
- ❌ 未验证 ≤ 560px 移动端
- ❌ 大规模更改现有视觉风格
- ❌ `dangerouslySetInnerHTML` / `eval`
- ❌ `<span onClick>` 替代 `<button>` 交互

## 五、治理

- 本宪法是前端项目的最高设计准则，所有 UI/UX 变更和组件开发必须符合本宪法。
- AGENTS.md 和 DESIGN.md 是本宪法的实施细则，与本宪法冲突时以本宪法为准。
- 宪法修订流程：提出修订理由 → 在 DESIGN.md 记录变更 → 更新版本号。
- 版本号语义：MAJOR（不兼容的原则变更）、MINOR（新增原则/约束）、PATCH（措辞修正）。

**Version**: 1.0.0 | **Ratified**: 2026-08-07 | **Last Amended**: 2026-08-07
