# Tasks: 小红书风格内容社区前端

**Input**: Design documents from `/data/workspace/feed/frontend/specs/001-modern-content-feed-ui/`

**Prerequisites**: plan.md ✅, spec.md ✅, research.md ✅, data-model.md ✅, quickstart.md ✅（无 `contracts/`，本功能不涉及后端接口变更）

**Tests**: 本功能未在spec 中明确要求自动化测试（现状项目无测试框架），故不生成测试任务；验证方式为 `quickstart.md` 手动场景走查 + `npm run build`/`lint` 静态检查（见 Polish 阶段）。

**Organization**: 任务按 spec.md 中的 6 个 User Story（P1/P1/P2/P2/P2/P3）分组，Setup/Foundational 阶段为所有故事的共享前置依赖。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行执行（不同文件、无依赖）
- **[Story]**: 对应 spec.md 的用户故事编号（US1~US6）
- 所有任务均给出精确文件路径

## Path Conventions

本功能位于 monorepo 的 `frontend/` 子项目，所有路径均相对 `frontend/` 目录：`src/styles.css`、`src/components/`、`src/pages/`、`DESIGN.md`。

---

## Phase 1: Setup（Design Token 基线）

**Purpose**: 建立本次改造的视觉基线（品牌色/圆角等Token 调整），是后续所有样式改动的前置依赖

- [ ] T001 按 `data-model.md` §3 更新 `frontend/src/styles.css`的 `:root` Design Token：`--color-brand`（`#e5484d`→`#ff2442`）及对应 `--color-brand-hover`/`--color-brand-active` 加深色阶、`--radius-lg`（`10px`→`12px`）、新增 `--color-bg-hover` token
- [ ] T002 [P] 同步更新 `frontend/DESIGN.md` §3.1（颜色）、§3.4（圆角）章节表格，登记 T001 中调整的 Token 新值

**Checkpoint**: Token 基线就位，后续所有样式任务均可引用新 Token

---

## Phase 2: Foundational（共享组件与布局基础设施）

**Purpose**: 构建瀑布流布局、骨架屏组件、`FeedCardGrid` 变体支持、导航视觉升级 — 这些被多个 User Story 共用，必须先完成

**⚠️ CRITICAL**: 本阶段任务全部完成前，不得开始任何 User Story 的实现

- [ ] T003 [P] 在 `frontend/src/styles.css` 新增瀑布流布局规则：`.waterfall`（`column-count` 响应式：≥1200px 5列/≥900px 4列/≥640px 3列/<640px 2列）、`.waterfall-card`（卡片容器+hover 微动效）、`.card-img-wrap`（图片容器+底部渐变遮罩+点赞数overlay）、`.card-body`/`.card-title`/`.card-footer`
- [ ] T004 [P] 在 `frontend/src/styles.css` 新增等宽网格变体规则：复用 `.waterfall-card` 卡片样式，新增 `.feed-grid`（`grid-template-columns` 等宽网格，用于个人主页/赞收藏页），响应式 ≤640px 收窄为 2 列
- [ ] T005 [P] 创建 `frontend/src/components/Skeleton.tsx`：支持 `variant: 'card' | 'line' | 'circle'`，`width`/`height` props，纯 CSS shimmer 动效实现（对应 `data-model.md` §2.2）
- [ ] T006 [P] 在 `frontend/src/styles.css` 新增 `.skeleton`骨架屏样式（渐变 shimmer 动画，遵循 `--motion-*` token，并包裹 `prefers-reduced-motion` 降级）
- [ ] T007 修改 `frontend/src/components/FeedCardGrid.tsx`：新增可选 prop `variant?: 'waterfall' | 'grid'`（默认 `'waterfall'`），根据 variant 渲染 `.waterfall`/`.feed-grid` 容器，卡片内部改为图片主体+底部点赞遮罩+标题+作者footer 的新结构（对应 `data-model.md` §2.1），保持 `items`/`loading`/`hasMore`/`sentinelRef` 现有 props 契约不变；新卡片 markup 必须包含图片 `onError` 占位兜底（加载失败时回退为 `--color-bg-placeholder` 占位块，不暴露破损图标）
- [ ] T008 [P] 重构 `frontend/src/components/Layout.tsx`：侧边导航新增图标（发现/赞收藏/我的主页分组）、`.sidebar-divider` 分组标签、底部 `.sidebar-user` 当前用户信息卡（头像+昵称+用户名+退出入口）
- [ ] T009 [P] 在 `frontend/src/styles.css` 新增/调整侧边导航与顶部搜索栏样式：`.sidebar-brand`、`.sidebar-link`（含`.nav-icon`）、`.sidebar-footer`/`.sidebar-user`、`.topbar`（毛玻璃背景）、`.searchbar .input`（药丸形搜索框），并验证 ≤768px 时侧边导航折叠为顶部横向导航的现状行为不回归
- [ ] T010 [P] 更新 `frontend/DESIGN.md` §10.1 公共组件清单，登记 `Skeleton`（新增）与 `FeedCardGrid`（variant 变更）；从 §10.3「缺失组件」列表中移除 `Skeleton`

**Checkpoint**: 瀑布流容器、骨架屏、导航基础设施就位— 所有 User Story 可以开始并行实现

---

## Phase 3: User Story 1 -浏览发现信息流（首页瀑布流） (Priority: P1) 🎯 MVP

**Goal**: 首页以瀑布流卡片网格展示内容，支持三类信息流切换与无限滚动加载，具备完整的加载/空/错误三态

**Independent Test**: 登录后进入首页（`VITE_USE_MOCK=true`），验证瀑布流布局在桌面/移动端均正确显示，滚动触发分页加载，Tab 切换生效

- [ ] T011 [US1] 修改 `frontend/src/pages/HomePage.tsx`：初次加载时使用 `Skeleton`（variant="card"）渲染 6-8 个占位卡片，替代原纯文字「加载中…」
- [ ] T012 [US1] 验证/调整 `frontend/src/pages/HomePage.tsx` 中Tab 切换的 `.category-tabs`/`.category-tab` 样式类名与 `frontend/src/styles.css` 新增分类Tab 样式（胶囊选中态、hover 反馈）保持一致
- [ ] T013 [US1] 验证 `frontend/src/pages/HomePage.tsx` 空数据态（`.list-end` 「暂无内容」）与错误态（拦截器 toast + 已加载内容不丢失）符合 spec Edge Cases
- [ ] T014 [US1] 验证 `frontend/src/pages/HomePage.tsx` 中 FAB 发布按钮在新 `.fab` 样式（阴影/hover 放大动效）下的桌面与移动端（≤640px 缩小尺寸）定位正确

**Checkpoint**: 首页瀑布流浏览体验独立可用 —此时可视为 MVP 交付

---

## Phase 4: User Story 2 - 查看帖子详情与互动 (Priority: P1)

**Goal**: 帖子详情页完整展示内容与作者信息，支持点赞/收藏/关注/删除操作的即时视觉反馈

**Independent Test**: 从首页点击任意卡片进入详情页，验证内容完整展示，执行点赞/收藏操作后状态即时切换，验证作者关注/自己删帖两种分支

- [ ] T015 [US2] 修改 `frontend/src/pages/FeedDetailPage.tsx`：作者信息栏、标题正文、`.detail-actions` 互动栏（点赞/收藏按钮）应用新 `.btn.active-state` 品牌色高亮样式；同时验证点赞/收藏/关注等异步操作按钮均具备防重复触发的即时状态切换（呼应 spec FR-023）
- [ ] T016 [US2] 修改 `frontend/src/pages/FeedDetailPage.tsx`：初次加载帖子详情时使用 `Skeleton`（variant="line"/"card" 组合）替代原「加载中…」纯文字
- [ ] T017 [US2] 验证 `frontend/src/pages/FeedDetailPage.tsx` 中 `.media-grid` 图片/视频响应式展示（3列缩略图、视频占满行）在桌面与移动端均正确
- [ ] T018 [US2] 验证 `frontend/src/pages/FeedDetailPage.tsx` 删除帖子二次确认流程与关注/取关切换的视觉反馈（`active-state` 类切换）

**Checkpoint**: 首页浏览 + 帖子详情互动均独立可用

---

## Phase 5: User Story 3 - 评论互动（楼中楼） (Priority: P2)

**Goal**: 评论列表、发表评论、子回复展开、评论点赞/删除的视觉与交互升级

**Independent Test**: 在帖子详情页发表评论、回复他人评论、展开子回复、点赞/删除评论，验证全流程视觉反馈正确

- [ ] T019 [US3] 修改 `frontend/src/pages/FeedDetailPage.tsx` 评论输入区：回复目标提示条（「回复 @昵称」+取消）、输入框+发送按钮布局样式
- [ ] T020 [US3] 修改 `frontend/src/pages/FeedDetailPage.tsx` 评论列表：`.comment-item`/`.comment-actions`（点赞高亮态`.liked`）/`.sub-replies`（子回复缩进展示+展开全部按钮）视觉样式，并确认评论删除的二次确认（`confirm()`）交互保留不变
- [ ] T021 [US3] 验证 `frontend/src/pages/FeedDetailPage.tsx` 评论区加载中（骨架或文案）/空评论（「暂无评论」）/滚动分页哨兵三态完整

**Checkpoint**: 评论互动体验独立可用，可与 US1/US2 组合演示完整帖子浏览闭环

---

## Phase 6: User Story 4 - 个人主页与社交关系 (Priority: P2)

**Goal**: 个人主页头部信息、帖子网格（等宽 grid variant）、关注/粉丝列表的视觉升级

**Independent Test**: 点击作者头像进入个人主页，验证头部统计信息、帖子网格展示，点击关注数进入关系列表验证关注/取关操作

- [ ] T022 [US4] 修改 `frontend/src/pages/ProfilePage.tsx`：头部 `.user-row`/`.stats-row`（关注/粉丝/帖子统计，hover 反馈）、关注/编辑资料按钮样式
- [ ] T023 [US4] 修改 `frontend/src/pages/ProfilePage.tsx`：`FeedCardGrid` 调用传入 `variant="grid"`，验证等宽网格在该页正确渲染（依赖 Phase 2 T007/T004）
- [ ] T024 [P] [US4] 修改 `frontend/src/pages/MyLikesCollectsPage.tsx`：`FeedCardGrid` 调用传入 `variant="grid"`，保持我的赞/收藏页视觉与个人主页一致
- [ ] T025 [P] [US4] 修改 `frontend/src/pages/RelationPage.tsx`：关注/粉丝列表行应用新 `.user-row` 视觉语言（头像+昵称+简介+关注按钮布局），补充加载/空数据/错误三态

**Checkpoint**: 个人主页与社交关系浏览体验独立可用

---

## Phase 7: User Story 5 - 发布帖子 (Priority: P2)

**Goal**: 发布表单与编辑资料表单的视觉一致性升级

**Independent Test**: 点击 FAB 进入发布页，上传媒体、填写描述、提交发布，验证表单校验提示、上传进度、提交按钮 loading 态

- [ ] T026 [US5] 修改 `frontend/src/pages/PublishPage.tsx`：标题/描述输入框、媒体上传预览网格 `.upload-preview`、提交按钮（`disabled` +「发布中…」文案）应用新表单视觉样式
- [ ] T027 [P] [US5] 修改 `frontend/src/pages/EditProfilePage.tsx`：表单字段、头像编辑区 `.avatar-field` 样式与 `PublishPage.tsx` 保持一致的视觉语言

**Checkpoint**: 发布与资料编辑流程视觉统一

---

## Phase 8: User Story 6 - 登录与注册 (Priority: P3)

**Goal**: 登录/注册表单页视觉升级，与整体设计语言统一

**Independent Test**: 访问 `/login`/`/register`，验证表单视觉样式、输入焦点态、错误提示、提交按钮 loading 态

- [ ] T028 [P] [US6] 修改 `frontend/src/pages/LoginPage.tsx` 引用的 `.form` 样式（已在 Phase 1 T001 更新 Token 后自动生效），验证输入框focus 态（品牌色描边+浅色光晕）与按钮视觉，同时确认登录按钮 `disabled`+「登录中…」loading 态未被破坏
- [ ] T029 [P] [US6] 修改 `frontend/src/pages/RegisterPage.tsx`，同上验证，确保与登录页视觉完全一致，并确认注册按钮 `disabled`+「注册中…」loading 态未被破坏

**Checkpoint**: 全部 6 个用户故事均可独立演示

---

## Phase 9: Polish & Cross-Cutting Concerns

**Purpose**: 跨故事的收尾验证与合规检查

>以下 T030-T034 为 `/speckit.analyze` 发现的 HIGH 级安全/可访问性质量门补充（C1-C5），均为**回归验证性质**（现状逻辑已实现，本次仅需确认未被视觉重构破坏），不引入新开发工作量。

- [ ] T030 [P] 权限校验回归验证（呼应 spec FR-015 / AuthZ 安全红线）：检查 `frontend/src/pages/FeedDetailPage.tsx`（帖子删除、评论删除）中 `me?.id === author.id` 权限判断在本次视觉重构后未被误删或绕过，非本人内容不得出现删除入口
- [ ] T031 [P] 大整数 ID 精度回归验证（呼应 spec FR-018 / constitution III.1 NON-NEGOTIABLE）：在浏览器 Console 检查 `feed.id`/`user.id`/`comment.id` 等字段经 `frontend/src/api/request.ts` 解析后仍为字符串、未出现精度丢失；确认本次改动未绕开 `request.ts`/`http` 自行 `fetch`/`JSON.parse`
- [ ] T032 [P] 密钥与凭证自检（呼应 spec FR-020 / constitution III.3 NON-NEGOTIABLE）：检查 `frontend/.env*` 文件确认零密钥（仅允许 `VITE_GATEWAY_TARGET`/`VITE_USE_MOCK`），上传凭证仍全部来自后端 STS 接口（`frontend/src/utils/upload.ts`）
- [ ] T033 [P] 401 统一处理回归验证（呼应 spec FR-021 / constitution III.4 NON-NEGOTIABLE）：在 `frontend/src/components/Layout.tsx` 退出登录逻辑改动后，手动使 token 失效触发 401，确认仍由 `frontend/src/api/request.ts` 拦截器统一清token 并跳转 `/login?redirect=...`
- [ ] T034 [P] 可访问性专项验证（呼应 spec FR-027/028/029、SC-009）：对 `frontend/src/components/FeedCardGrid.tsx`（瀑布流卡片 `<Link>`）、`frontend/src/components/Layout.tsx`（导航图标链接）、`frontend/src/components/Skeleton.tsx`（骨架屏容器）逐一验证：语义化标签（`<a>`/`<button>`）、Tab 键可达、`focus-visible` 轮廓可见、所有 `<img>` 均设置 `alt`（内容图给描述，装饰图 `alt=""`）
- [ ] T035 [P] 全面核对 `frontend/DESIGN.md`：确认 §3（Token）、§8（页面布局，新增瀑布流描述）、§9（响应式规则，新增列数断点表）、§10（组件清单）均已同步本次全部改动，无遗漏登记
- [ ] T036 依次执行 `npm run lint`、`npm run typecheck`、`npm run build`（`frontend/` 目录下），修复所有报错直至全部通过
- [ ] T037 [P] 安全自检：在 `frontend/src` 全局搜索确认零 `dangerouslySetInnerHTML`/`eval` 调用；抽查本次修改文件确认无新增硬编码颜色/像素值（未引用 Token）
- [ ] T038 [P] 按 `quickstart.md` 的 6 个验证场景，在 Mock 模式（`VITE_USE_MOCK=true`）下完整走查一遍端到端流程
- [ ] T039 [P] 响应式回归检查：对全部 9 个页面在桌面（>1024px）、平板（768-1024px）、移动端（≤560px）三种视口下检查布局，确认无溢出/错乱/内容截断

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: 无依赖，立即开始
- **Foundational (Phase 2)**: 依赖 Phase 1 完成（需要新Token 值）— **阻塞所有 User Story**
- **User Stories (Phase 3-8)**: 均依赖 Phase 2 完成；之后可按优先级顺序（P1→P1→P2→P2→P2→P3）或并行推进
- **Polish (Phase 9)**: 依赖所有期望交付的 User Story 完成

### User Story Dependencies

- **US1 (P1，首页瀑布流)**: Foundational 完成后可独立开始，无其他故事依赖
- **US2 (P1，帖子详情)**: Foundational 完成后可独立开始；与US1 共享 `FeedCardGrid`/跳转入口但代码文件不同，可并行
- **US3 (P2，评论)**: 依赖 US2 相同文件 `FeedDetailPage.tsx`（不同代码区块），建议 US2 完成后接续，但功能逻辑独立
- **US4 (P2，个人主页)**: 依赖 Phase 2 的 `FeedCardGrid` grid variant（T004/T007），与 US1/US2 无代码冲突，可并行
- **US5 (P2，发布)**: 独立文件，Foundational 完成后即可开始，可并行
- **US6 (P3，登录注册)**: 依赖 Phase 1 Token 更新即可开始，最独立，可最早并行完成

### Within Each User Story

- 涉及同一文件的多个任务（如 US2/US3 均改`FeedDetailPage.tsx`）应按任务列出顺序执行，避免编辑冲突
- 不同文件的任务标记 `[P]` 可并行

### Parallel Opportunities

- Phase 1: T002 可与 T001 并行（不同文件）
- Phase 2: T003/T004/T005/T006/T008/T009/T010 可大量并行（多为不同文件或styles.css 中互不重叠的选择器块）；T007 依赖 T003/T004 提供的 CSS class 名，需在其后进行
- Phase 3-8: US1/US2/US4/US5/US6 五个故事可由不同开发者完全并行推进；US3 建议紧随 US2 之后（同文件）
- Phase 9: T030-T035/T037/T038/T039 可并行；T036 建议在其余任务基本完成后统一跑一次

---

## Parallel Example: Foundational Phase

```bash
# 可并行启动的基础设施任务（不同文件/不重叠的 CSS 选择器）：
Task: "在 styles.css 新增瀑布流布局规则 .waterfall/.waterfall-card"
Task: "在 styles.css 新增等宽网格变体规则 .feed-grid"
Task: "创建 Skeleton 组件 src/components/Skeleton.tsx"
Task: "在 styles.css 新增骨架屏 shimmer 样式"
Task: "重构 Layout.tsx 侧边导航结构"
Task: "在 styles.css 新增侧边导航/顶部搜索栏样式"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. 完成 Phase 1: Setup（Token 基线）
2. 完成 Phase 2: Foundational（瀑布流 CSS + Skeleton + FeedCardGrid variant + 导航升级）—— **关键阻塞阶段**
3. 完成 Phase 3: User Story 1（首页瀑布流）
4. **停下并验证**：按 quickstart.md 场景 1 独立测试首页体验
5. 若满意可视为 MVP 交付演示

### Incremental Delivery

1. Setup + Foundational 完成 → 基础设施就位
2.加入 US1（首页）→ 独立验证 → 可演示 MVP
3. 加入 US2（详情+互动）→ 独立验证 → 演示完整浏览闭环
4. 加入 US3（评论）→ 独立验证 → 演示社交讨论功能
5. 加入 US4（个人主页）→ 独立验证 → 演示社交关系
6. 加入 US5（发布）→ 独立验证 → 演示内容生产
7. 加入 US6（登录注册）→ 独立验证 → 全流程视觉统一收尾
8. Polish 阶段收尾验证全部完成

### Parallel Team Strategy

多人协作时：

1. 团队共同完成 Setup + Foundational
2. Foundational 完成后：
   - 开发者 A：US1（首页）+ US2（详情）+ US3（评论，同文件延续）
   - 开发者 B：US4（个人主页/关系列表）
   - 开发者 C：US5（发布/编辑资料）+ US6（登录注册）
3. 各故事独立完成后在 Polish 阶段统一收尾验证

---

## Notes

- 本次改造**零后端接口变更**，所有任务均为前端视觉/交互层修改
- `[P]` 任务 = 不同文件或同文件中不重叠的选择器/代码块，无依赖冲突
- 涉及 `frontend/src/pages/FeedDetailPage.tsx` 的任务（T015-T021）分属 US2/US3，注意不要相互覆盖已完成的代码区块
- 每个 Checkpoint 处应按 `quickstart.md` 对应场景手动验证一次，再进入下一阶段
- 当前环境缺少 Node.js/npm（见系统提示），T036 起的构建/lint 验证需先安装 Node.js（https://nodejs.org/）才能执行
- 避免：模糊任务描述、同文件任务标记为可并行、破坏其他故事独立性的跨故事强依赖
