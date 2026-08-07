# DESIGN.md — feed-web 前端视觉与交互规范（唯一入口）

> 本文件是 `feed-web` 项目**唯一的前端视觉与交互规范入口**。所有页面、组件、样式变更都必须遵循本文档。AI 协作者在执行任何前端任务前必须先阅读 `AGENTS.md` 与本文档。
>
> 阅读顺序：`README.md`（业务/接口契约） → `AGENTS.md`（编码流程与约束） → 本文档（视觉/交互规范） → `styles.css`（Design Token 实现）。

---

## 1. 项目定位与设计方向

- **项目类型**：Feed（图文/视频信息流）Web 前端，C 端内容社区。
- **目标用户**：普通内容消费者与创作者；以移动端与窄屏桌面为主场景。
- **整体视觉方向**：小红书风格（Xiaohongshu-like）——图片优先的瀑布流信息流、白底卡片 + 浅灰页面底 + 品牌红 `#ff2442` 强调；卡片轻阴影、大圆角、留白克制、移动友好。
- **设计关键词**：`图片优先` · `瀑布流` · `内容优先` · `单一品牌色 #ff2442` · `轻阴影` · `移动友好`。
- **明确禁止的设计风格**：拟物/重阴影/渐变背景、多品牌色并置、暗色主题（当前未支持）、拟物按钮、装饰性动画、与现有扁平风格冲突的视觉语言。

> 现有风格已稳定，本规范以**归纳现有规律**为主，不擅自更换设计方向。

---

## 2. 技术栈与现有结构（简述）

- React 18 + TypeScript（严格模式）+ Vite 5；路由 `react-router-dom@6`；状态 `zustand@5`；请求 `axios@1` + `json-bigint`；上传 `cos-js-sdk-v5`。
- 样式：**仅一份全局普通 CSS** `src/styles.css`，无 CSS Modules / SCSS / Tailwind，无第三方 UI 组件库。所有 Design Token 以 CSS 变量（`--xxx`）定义于 `:root`。
- 关键文件：`src/api/request.ts`（网络入口 + 大整数解析 + 统一拆包 + 401）、`src/store/auth.ts`（token 持久化 + 启动自愈）、`src/App.tsx`（路由 + `RequireAuth`）、`src/hooks/useCursorList.ts`（cursor 无限滚动）。

---

## 3. Design Tokens（设计令牌）

> 所有视觉值必须引用以下令牌，禁止在业务代码硬编码十六进制 / 任意数值。完整定义在 `src/styles.css` 的 `:root`。新增令牌必须在此登记并同步 `styles.css`。

### 3.1 颜色
| Token | 值 | 用途 |
|------|----|------|
| `--color-bg-page` | `#f5f5f5` | 页面底色 |
| `--color-bg-surface` | `#ffffff` | 卡片 / 导航 / 表单底 |
| `--color-bg-subtle` | `#fafafa` | 次级悬浮 / 子回复底 |
| `--color-bg-placeholder` | `#e8e8e8` | 封面 / 图片占位 |
| `--color-bg-hover` | `#f2f2f2` | 列表行 / 导航项 hover |
| `--color-brand-subtle` | `#fef0f0` | 品牌色浅底（导航选中/hover） |
| `--color-text-primary` | `#1a1a1a` | 主文字 |
| `--color-text-secondary` | `#555555` | 次级文字 |
| `--color-text-tertiary` | `#666666` | 表单标签 |
| `--color-text-muted` | `#999999` | 辅助 / 元信息 |
| `--color-text-faint` | `#b0b0b0` | 弱文字 / 用户名下方 |
| `--color-text-disabled` | `#cccccc` | 禁用 / 无数据提示 |
| `--color-brand` | `#ff2442` | 主品牌色（链接 / 选中 / 强调） |
| `--color-brand-hover` | `#e61e3a` | 品牌色 hover |
| `--color-brand-active` | `#cc1530` | 品牌色 active |
| `--color-error` | `#ff2442` | 错误（复用品牌红） |
| `--color-success` | `#30a46c` | 成功（toast） |
| `--color-warning` | `#e8a33d` | 警告（预留） |
| `--color-info` | `#2f80ed` | 信息（预留） |
| `--color-border` | `#e0e0e0` | 输入框 / ghost 按钮默认边框 |
| `--color-border-subtle` | `#eeeeee` | 导航底 / 占位边框 |
| `--color-border-strong` | `#e5e5e5` | tab 边框 |
| `--color-border-light` | `#f2f2f2` | 评论 / 用户行分割线 |
| `--color-avatar-fallback` | `#cccccc` | 头像加载失败兜底 |
| `--color-overlay` | `rgba(0,0,0,.45)` | 遮罩（预留） |

### 3.2 字体（字号 / 字重 / 行高）
| Token | 值 | 用途 |
|------|----|------|
| `--font-sans` | 系统字体栈 | 全局字体族 |
| `--text-xs` 11px / `--text-sm` 13px / `--text-base` 14px / `--text-md` 16px / `--text-lg` 18px / `--text-xl` 22px / `--text-2xl` 28px | 见第 6 节；`--text-2xl` 用于表单/详情页大标题 |
| `--weight-regular` 400 / `--weight-medium` 500 / `--weight-semibold` 600 / `--weight-bold` 700 | 字重 |

### 3.3 间距（4px 基准）
`--space-1`4 / `--space-2`8 / `--space-3`12 / `--space-4`16 / `--space-5`20 / `--space-6`24 / `--space-8`32（px）。
页面内边距、卡片内边距用 `--space-4`；模块间距用 `--space-3`/`--space-4`；表单字段间距 14px（历史值，新代码优先用 `--space-3`/`--space-4`）。

### 3.4 圆角 / 边框 / 阴影
- 圆角：`--radius-sm`4 / `--radius-md`8 / `--radius-lg`12 / `--radius-xl`16 / `--radius-full`999（药丸形搜索框/按钮/FAB 用 full）。
- 边框：`--border-width`1px；组合 `--border-default`(1px #e0e0e0)、`--border-subtle`(1px #eee)、`--border-light`(1px #f2f2f2)。
- 阴影：卡片使用轻阴影 `--shadow-card`(0 1px 3px rgba(0,0,0,.04))，hover 态可叠加更深阴影；Modal/Drawer 用 `--shadow-modal`(0 8px 32px rgba(0,0,0,.12))；FAB 用品牌色柔光阴影。**禁止**为按钮、输入添加阴影；禁止重阴影/拟物。

### 3.5 层级 / 容器 / 断点 / 动效
- z-index：`--z-nav`100 / `--z-fab`200 / `--z-overlay`1000 / `--z-modal`1001 / `--z-toast`9999（禁止无语义数值）。
- 容器：`--content-max`1040px，`--container-padding`16px，`--sidebar-width`220px。
- 断点：`--bp-sm`560（移动端）/ `--bp-md`768（侧栏折叠为顶部横向导航）/ `--bp-lg`1024（大屏预留）。瀑布流列数断点：1200px(5列)/900px(4列)/640px(3列)/≤640px(2列)。**注意**：CSS 媒体查询无法使用 `var()`，断点处直接写对应数值。
- 动效：`--motion-fast`120ms / `--motion-base`200ms / `--ease-standard` `cubic-bezier(.4,0,.2,1)`。必须支持 `prefers-reduced-motion`。

---

## 4. 颜色系统

- **页面背景** `--color-bg-page`；**卡片/导航/表单底** `--color-bg-surface`；**悬浮底** `--color-bg-subtle` / `--color-brand-subtle`。
- **文字三级 + 弱化 + 禁用**：primary→secondary→tertiary→muted→faint→disabled，按语义取用，不要混用相近灰阶。
- **品牌色** 仅 `--color-brand` 及其 hover/active 三态；链接、选中态、强调数字统一用它。
- **状态色**：成功/错误/警告/信息见 3.1；错误当前复用品牌红，新组件若需区分错误与品牌，使用 `--color-error`。
- **遮罩** `--color-overlay` 仅用于 Modal/Drawer。
- **规则**：业务页面禁止直接书写十六进制 / rgb / hsl；一律引用 Token。

---

## 5. 字体系统

| 层级 | Token | 字号 | 字重 | 行高 | 典型场景 |
|------|-------|------|------|------|----------|
| 页面标题 | `--text-xl` | 22px | 700 | 1.3 | 表单 h1（`.form h1`） |
| 一级标题 | `--text-lg` | 18px | 700 | 1.4 | 卡片标题 / 导航品牌 / 页面区块 `.section-title` |
| 二级标题 | `--text-md` | 16px | 700 | 1.4 | 统计数字 / 用户名 |
| 卡片标题 | `--text-base` | 14px | 600 | 1.4 | feed 卡片标题（2 行截断） |
| 正文 | `--text-base` | 14px | 400 | 1.7 | 帖子描述、评论内容 |
| 辅助文字 | `--text-sm` | 13px | 400 | 1.5 | 标签、提示 `.text-muted` |
| 元信息 | `--text-xs` | 12px | 400 | 1.4 | 时间、统计标签、评论时间 |
| 按钮文字 | `--text-base` | 14px | 400 | — | 按钮内文字 |
| 表格文字 | `--text-base` | 14px | 400 | 1.5 | 当前无表格，预留 |

- 禁止页面自行新增无规范的字号/字重；需要新层级时先在此登记 Token。

---

## 6. 间距系统

- 基准 **4px**。常用组合：页面/卡片内边距 `--space-4`(16)；模块间距 `--space-3`(12)/`--space-4`(16)；卡片内边距 `--space-4`；表单字段间距 14px（历史）；按钮间距 `--space-2`(8)/`--space-3`(12)；列表项间距 `--space-3`(12)；`gap` 默认 `--space-3`(12)。
- 禁止出现无语义的任意间距值（如 `margin: 7px`、`padding: 13px`）；优先使用间距 Token。

---

## 7. 圆角、边框和阴影

- 按钮 / 输入 / 标签 → `--radius-md`(8)；卡片 / feed 卡 → `--radius-lg`(10)；表单容器 → `--radius-xl`(12)；圆角标签 / 头像 → `--radius-full`(999)；媒体缩略图小圆角 → `--radius-sm`(6)。
- 边框统一 1px；输入框/ghost 按钮/tab 用 `--border-default`，分割线用 `--border-light`。
- **阴影**：默认不使用（见 3.4）。卡片、按钮、输入一律无阴影。

---

## 8. 页面布局

- 应用骨架 `.app-shell`：flex 左右布局，左侧 `.sidebar` 固定宽 `--sidebar-width`(220px)，右侧 `.main-area` 自适应。
- 左侧导航 `.sidebar`：sticky 全高，含品牌 `.sidebar-brand`（📕 图标+名字）+ 导航 `.sidebar-nav`（`.sidebar-link` 含 `.nav-icon` 图标+文字，hover/active 用 `--color-brand-subtle`）+ 底部 `.sidebar-footer`/`.sidebar-user`（当前登录用户头像+昵称+用户名+退出按钮）。按功能分区用 `.sidebar-divider` 分组标签。窄屏（≤768px）折叠为顶部横向导航（隐藏品牌字、分组标签、底部用户卡）。
- 顶部搜索 `.topbar`：sticky + 毛玻璃（`backdrop-filter: blur(12px)` 半透明白底），含 `.searchbar`（药丸形 `.input` 圆角 full + `.btn`）。当前提交为占位提示，待接搜索接口。
- 内容区 `.content`：最大宽 `--content-max`(1040px) 水平居中，承载路由页面（`<Outlet/>`）。
- 首页信息流：瀑布流 `.waterfall`（`column-count` 5/4/3/2 随视口自适应，见 §9），卡片 `.waterfall-card`（图片主体 `.card-img-wrap` + 底部渐变遮罩 `.card-img-overlay` 显示点赞数 `.overlay-like` + 标题 `.card-title` + 作者 footer `.card-footer`/`.name`）。个人主页/赞收藏页用等宽网格 `.feed-grid`（`grid` 3 列，配 `.waterfall-card`）。
- 页面标题区：用 `.section-title`（18px/700/上下间距）。内容区直接放置卡片或列表。
- 发布入口不在导航，而是首页右下角的浮动加号 `.fab`（品牌红圆形 FAB，固定右下角，跳转 `/publish`，仅首页渲染）。

---

## 9. 响应式规则

| 维度 | 桌面（>1024px） | 中屏（768–1024px） | 移动（≤640px） |
|------|----------------|-------------------|----------------|
| 页面边距 | 16px | 16px | 12px（容器） |
| 瀑布流列数 | 5 列（>1200px）/ 4 列（900–1200px） | 3 列（640–900px） | 2 列（≤640px） |
| 侧边导航 | 左侧固定（220px） | 左侧固定 | 顶部横向导航（隐藏品牌字/分组/底部用户卡） |
| 弹窗宽度 | 最大 560px | 最大 92vw | 最大 92vw |
| 字号 | 如上 | 不变 | 不变（移动优先） |
| 图片比例 | 瀑布流交错比例（见 §14） | 同左 | 同左 |
| 操作按钮 | 行内 | 行内；必要时换行 | 行内；必要时换行 |

- 断点：中屏 `--bp-md`768（侧边栏折叠为顶部横向导航）；大屏 `--bp-lg`1024 预留；瀑布流列数断点 1200 / 900 / 640px（详见 §3.5）。
- 所有新页面必须至少验证桌面（>1024px）、平板（768–1024px）、移动（≤640px）三种宽度。

---

## 10. 组件规范

### 10.1 现有公共组件清单（必须复用，禁止页面内重复实现）

| 组件 | 位置 | 类型 | 用途 | 主要变体 | 禁止用法 |
|------|------|------|------|----------|----------|
| `Layout` | `components/Layout.tsx` | 布局 | 左侧导航栏 + 顶部搜索栏 + 内容区(`<Outlet/>`) | 骨架 class：`.app-shell/.sidebar/.sidebar-brand/.sidebar-nav/.sidebar-user/.topbar/.content` | 不要在页面内再写导航 |
| `Avatar` | `components/Avatar.tsx` | 基础 UI | 头像（自带签名 URL + 兜底） | props: `src/size/alt` | 不要用裸 `<img>` 替代 |
| `FeedCardGrid` | `components/FeedCardGrid.tsx` | 数据展示 | feed 卡片网格 + 无限滚动哨兵 | props: `items/loading/hasMore/sentinelRef/variant`（`'waterfall'｜'grid'`） | 不要在各页重写网格 |
| `Skeleton` | `components/Skeleton.tsx` | 反馈 | 加载占位骨架屏（shimmer 动效） | 变体 `card/line/circle` | 不要在页面内手写占位条 |
| `useCursorList` | `hooks/useCursorList.ts` | Hook | cursor 分页无限滚动 | 泛型 `T` | 不要对 cursor 接口手写分页 |
| `toast` | `utils/toast.ts` | 反馈 | 轻提示（textContent，安全） | `toast(msg, type?)` | 不要用 `alert` |
| `RequireAuth` | `App.tsx` | 导航守卫 | 登录保护 | — | 不要绕过守卫 |

### 10.2 必须复用而非新建的通用元件（已以 class 形式沉淀于 `styles.css`）

`Button`(`.btn` 含 `.block/.ghost/.small/.icon/.active-state`)、`Input`(`.input` 药丸形用于搜索框)、`Textarea`(`.textarea`)、`Tabs`(分类胶囊 `.category-tabs/.category-tab`，通用 `.tabs/.tab`)、`Card`(`.card`)、`Badge/文本色`(`.text-brand/.text-secondary/.text-muted`)、`SectionHeader`(`.section-title`)、`EmptyState/Loading/Error`(`.list-end` 文本态)、`FloatingActionButton`(`.fab`，品牌红圆形固定右下角，首页发布入口，跳转 `/publish`，仅首页渲染)、`Skeleton`(`components/Skeleton.tsx`，变体 `card/line/circle`)。

### 10.3 缺失组件（待建，新增前须先登记 DESIGN.md）

`Modal` / `ConfirmDialog`（当前删除用原生 `confirm()`，**已知技术债**，新代码禁止新增 `confirm()`，应改用未来的 `ConfirmDialog`）、`Table`、`Select/Checkbox/Radio/Switch`、`Tooltip/Dropdown`、`Pagination`、`Drawer`、`Badge`、`EmptyState`/`ErrorState` 组件化、`LoadingState` 组件化。

### 10.4 规则

- 新增页面前**先搜索现有组件**（`components/`、`styles.css` class）。只有现有元件无法满足时才新增公共组件，且必须有明确复用场景、在 10.3/10.1 登记。
- 禁止在页面内重复实现已有公共组件或等价 class（如再次手写 `border:1px solid #ddd;border-radius:8px` 的输入）。
- 页面组件只负责编排；展示/交互原子优先提取为公共组件。
- 以下逻辑必须提取为公共组件：跨页复用的卡片、行、空/错/加载态、确认弹窗。
- 不提取的情况：仅单页使用一次的局部结构、纯布局 wrapper，无需过度抽象。

---

## 11. 交互状态

所有可交互元素必须实现（至少）：`default` / `hover` / `active` / `focus` / `focus-visible` / `disabled` / `loading` / `error` / `selected`。

- **按钮**：hover→`--color-brand-hover`；active→`--color-brand-active`；`focus-visible`→2px 品牌色 outline；`disabled`→`opacity:.6`+`not-allowed`（已在 `.btn` 实现）。
- **链接/可点击文本**（`.text-brand`/`.link-action`）：hover 改色，focus-visible 可见。
- **异步操作**：提交/发布/上传按钮必须 `disabled`+文案切换（如「发布中…」），防重复提交。
- **删除/危险操作**：必须二次确认（未来走 `ConfirmDialog`；当前仅 `confirm()` 历史代码）。
- **空数据**：显示占位文案（`.list-end`「暂无数据」/「没有更多了」）。
- **网络异常**：由请求拦截器统一 `toast`，页面无需各自处理；局部加载失败应提供重试。
- **权限不足**：未登录访问受保护路由由 `RequireAuth` 跳转登录。

---

## 12. 表单规范

- 标签：`.form label`（次级灰、13px、下方 8px 间距）；输入框 `.input` / `.textarea`。
- 必填标记：当前无统一星标，提交时校验并 `toast`；新表单建议在标签后加 `*` 并在提交时提示。
- 帮助文字 / 占位符：用 `.text-muted` 提示（如「标题（选填）」）。
- 错误提示：错误信息显示在对应字段**附近**；**禁止仅用颜色表达错误**，须配文字（toast 或字段下文字）。
- 校验时机：提交时校验；输入类（昵称/简介长度）由 `maxLength` 限制。
- 提交状态：`submitting` 时禁用按钮并显示「保存中…/发布中…」。

---

## 13. 表格和列表规范

- 当前以**列表/卡片**为主，无表格组件；如新增表格：表头 `--text-sm` 弱色、行高 44–52px、对齐左/右、操作列右对齐、状态列用文字+（可选）色而非纯色。
- 列表：cursor 分页用 `useCursorList` + `sentinelRef` 自动加载；offset 列表（关注/粉丝）显示「加载更多」+「没有更多了」。
- 空数据 / 加载中 / 错误：统一由 `.list-end` 文案承载。
- 危险操作（删除评论/帖子）二次确认。

---

## 14. 图片和媒体规范

- 瀑布流封面：按卡片 index 注入交错比例（`3/4`、`1/1`、`4/5`、`4/3` 循环），形成高低错落的瀑布流；`object-fit:cover`，加载前用 `--color-bg-placeholder` 占位（`.card-img-wrap` 兜底 `aspect-ratio:3/4`），`onError` 隐藏破损图露出占位底色。
- 头像圆形（`--radius-full`），失败兜底首字母/兜底色；`Avatar` 组件已处理签名 URL。
- 个人主页/赞收藏页缩略图 1:1（`.feed-grid` 网格）；详情页媒体九宫格 1:1、视频占满行（`media-grid`）。
- 懒加载：`loading="lazy"`（feed 图片已用）。

---

## 15. 动画规范

- 统一时长 `--motion-fast`(120ms) / `--motion-base`(200ms)，缓动 `--ease-standard`。
- 仅用于 hover/active/focus、列表加载等**有业务价值**的反馈；禁止装饰性动画。
- 必须包裹在 `@media (prefers-reduced-motion: reduce)` 下禁用（已在 `styles.css` 处理）。

---

## 16. 可访问性规范

- 语义化 HTML：用 `<button>`/`<a>` 而非 `<span onClick>`；当前评论「回复/删除」为 `<span onClick>`（**已知技术债**，新代码不得新增，应改为 `<button>` 并加 `focus-visible`）。
- 键盘可操作：所有交互元素可 Tab 聚焦，`focus-visible` 可见。
- 表单 `label` 与控件关联；图片 `alt`（装饰图 `alt=""`，内容图给描述）。
- 按钮必须有可访问名称（可见文字或 `aria-label`）。
- 颜色对比度：文字/背景满足 WCAG AA。
- 弹窗（未来）须焦点管理（打开聚焦、Esc 关闭、焦点陷阱、回到触发元素）。
- 禁止仅用颜色传达信息（错误须配文字）。

---

## 17. 禁止事项（AI 编码红线）

- 页面中直接硬编码颜色（hex/rgb/hsl）、字号、间距、圆角（必须引用 Token）。
- 随意新增字号 / 间距 / 圆角 / 颜色（无 DESIGN.md 登记）。
- 重复实现已有公共组件或等价 `.btn/.input/.card` 等 class。
- 为单个页面修改全局样式（改 `styles.css` 须同步 DESIGN.md；页面局部用 class 而非改全局）。
- 无理由引入新 UI 库 / 新样式方案 / 混用多套 Token。
- 使用行内样式实现复杂页面（允许 `style` 仅用于动态值如 `flex:1`、`minHeight`，且颜色/字号须引用 `var(--token)`）。
- 大量使用 `!important`（仅 `prefers-reduced-motion` 复位可用）。
- 使用无语义 `z-index` 数值（须用 `--z-*`）。
- 未实现加载 / 空数据 / 错误状态。
- 未处理移动端（≤560px）。
- 未处理键盘操作与 `focus-visible`。
- 未经说明大规模改变现有视觉风格。

---

## 18. 组件目录分类说明

当前 `components/` 仅 3 个文件，按职责归为：

- **布局** `Layout`：页面框架（左侧导航栏 + 顶部搜索栏 + 内容区）。
- **基础 UI** `Avatar`：原子展示元件。
- **数据展示** `FeedCardGrid`：业务数据卡片网格。

后续新增建议落位（**不要为贴合此结构而强制重构现有文件**）：

```
components/
  ui/        基础原子：Button/Input/Avatar/Badge（当前 Button/Input 以 class 形式在 styles.css）
  layout/    布局：Layout/Container
  form/      表单元件：Input/Textarea/Select（当前 Input/Textarea 为 class）
  feedback/  反馈：Toast/Modal/ConfirmDialog/Skeleton/EmptyState/ErrorState
  data-display/ 数据展示：FeedCardGrid/Table/Card
  navigation/ 导航：Tabs/RequireAuth
  business/   业务组件（跨页复用的领域组件）
```

- **页面组件可以包含**：局部编排、调用公共组件、绑定 hooks/状态。
- **必须提取为公共组件**：跨页复用的卡片/行/空错载态/确认弹窗。
- **不应提取**：仅单页一次性的局部结构。

---

## 19. 页面完成定义（验收标准）

一个页面只有在**同时满足**以下条件时才算完成：

- [ ] 使用现有布局组件（Layout）与统一 Design Token（无硬编码颜色/字号/间距/圆角）。
- [ ] 优先复用现有公共组件（Button/Input/Card/Tabs/Avatar/FeedCardGrid…）。
- [ ] 支持目标屏幕尺寸（至少桌面与 ≤560px 移动端）。
- [ ] 包含加载状态、空数据状态、错误状态。
- [ ] 表单包含校验与错误提示（文字，非仅颜色）。
- [ ] 异步按钮包含 `loading` 与 `disabled`。
- [ ] 危险操作包含确认。
- [ ] 键盘可操作主要交互（Tab 可达、`focus-visible` 可见）。
- [ ] 不存在明显 TypeScript 错误。
- [ ] ESLint 通过、`npm run build` 通过。
- [ ] 不影响现有业务功能与路由语义。
