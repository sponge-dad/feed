# Implementation Plan: 小红书风格内容社区前端

**Branch**: `001-modern-content-feed-ui` | **Date**: 2026-08-07 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/001-modern-content-feed-ui/spec.md`

## Summary

在现有 feed-web（React 18 + TS + Vite）前端基础上，对视觉与交互进行整体改造，打造类小红书风格的内容社区体验：首页改为瀑布流双列/多列图片流，卡片、导航、按钮、表单统一升级为更精致的视觉语言（浅灰底+ 白卡片 + 品牌红强调色+ 轻量动效），同时保持现有路由结构、数据契约（`*.api`/`*.proto`）、状态管理与安全底线不变。本次是**纯前端视觉与交互重构**，不新增后端接口，不改变现有组件对外 props 接口的核心语义（仅扩展可选项）。

## Technical Context

**Language/Version**: TypeScript 5.6（严格模式），React 18.3

**Primary Dependencies**: Vite 5、react-router-dom v6、zustand v5、axios v1 + json-bigint、cos-js-sdk-v5（均为现有依赖，不新增）

**Storage**: N/A（前端无本地持久化存储，仅 localStorage 存 JWT token，经 zustand persist）

**Testing**: 无自动化测试框架（现状）；验证方式为 `npm run build`（tsc 严格检查）+ `npm run lint`（ESLint）+ 手动多端视觉验证 + Mock 模式（`VITE_USE_MOCK=true`）端到端走查

**Target Platform**: 现代浏览器 Web（桌面 Chrome/Edge/Safari/Firefox 最新两个大版本；移动端 iOS Safari 14+、Android Chrome 90+）

**Project Type**: Web application（monorepo 中的 `frontend/` 子项目，对接既有 `backend/` gRPC + Gateway 服务）

**Performance Goals**: 首屏瀑布流渲染 < 2s（标准网络）；分页加载新内容 < 500ms 视觉反馈；点赞/收藏/关注等互动操作 < 100ms 乐观更新反馈

**Constraints**: 不引入任何新UI 组件库或 CSS 方案（继续单一全局 `src/styles.css` + Design Token）；不新增后端接口；不破坏 Mock 模式；不新增运行时依赖（如需新增开发依赖如 stylelint 规则，需在 plan 中说明并保持最小化）

**Scale/Scope**: 覆盖现有全部 9 个页面（Login/Register/Home/FeedDetail/Profile/EditProfile/Relation/MyLikesCollects/Publish）+ 3 个公共组件（Layout/FeedCardGrid/Avatar）+ 全局样式系统（styles.css + DESIGN.md）

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

依据 `.specify/memory/constitution.md`（feed-web 前端项目宪法 v1.0.0）逐条检查：

|宪法条款 | 检查结果 | 说明 |
|---------|---------|------|
| I. Design Token 唯一来源 | ✅ PASS | 本次改造全部视觉值（新增品牌色调整、瀑布流布局参数等）将登记进 `styles.css :root` 并同步 `DESIGN.md`，不允许业务代码硬编码 |
| II. 组件复用优先 | ✅ PASS | `FeedCardGrid`/`Avatar`/`Layout` 保持组件边界，仅内部实现升级为瀑布流；新增视觉元素（渐变遮罩、骨架屏等）如需跨页复用则提取为组件并登记 DESIGN.md |
| III. 前端安全底线 | ✅ PASS | 不改动 `request.ts` 大整数解析逻辑；不引入 `dangerouslySetInnerHTML`/`eval`；不改动 COS 上传凭证机制；401处理逻辑不变 |
| IV. 三态齐全 | ✅ PASS | 现有 Loading/Empty/Error 三态覆盖将在瀑布流改版后保留并强化（骨架屏替代纯文字loading） |
| V. 响应式设计 | ✅ PASS | 瀑布流断点设计遵循移动优先，≤560px 强制收敛为2 列，与现有断点体系（`--bp-sm/md/lg`）保持一致 |
| VI. 可访问性 | ✅ PASS | 卡片使用 `<a>`（Link）语义，保留 `focus-visible`，图片 `alt`，遵循 `prefers-reduced-motion` |
| 技术栈约束 | ✅ PASS | 不引入 Redux/MobX/Tailwind/SCSS/第三方 UI 库；不升级 React/Vite 主版本 |

**结论**：无违规项，无需填写Complexity Tracking。

## Project Structure

### Documentation (this feature)

```text
frontend/specs/001-modern-content-feed-ui/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── checklists/
│   └── requirements.md  # Spec quality checklist (from /speckit.specify)
└── tasks.md             # Phase 2 output (/speckit.tasks — NOT created by /speckit.plan)
```

无 `contracts/` 目录：本功能不新增或修改任何后端 API 契约，前端仅消费既有 `backend/app/gateway/api/*.api` 定义的接口，UI 契约变化（组件 props）在 `data-model.md` 中以「组件契约」章节记录，不单独建 `contracts/`。

### Source Code (repository root)

```text
frontend/
├── src/
│   ├── styles.css              # [MODIFY] Design Token 升级 + 瀑布流/卡片/导航新样式
│   ├── App.tsx                 # [NO CHANGE] 路由结构不变
│   ├── components/
│   │   ├── Layout.tsx          # [MODIFY]侧边导航视觉升级（图标+分组+用户信息卡）
│   │   ├── FeedCardGrid.tsx    # [MODIFY] 新增 variant 支持瀑布流(waterfall)与网格(grid)两种布局
│   │   ├── Avatar.tsx          # [NO CHANGE] 保持现有实现
│   │   └── Skeleton.tsx        # [NEW]骨架屏组件（登记入 DESIGN.md10.3 缺失组件补齐项）
│   ├── pages/
│   │   ├── HomePage.tsx        # [MODIFY]瀑布流容器 + 分类 Tab 视觉升级
│   │   ├── FeedDetailPage.tsx  # [MODIFY] 沉浸式媒体展示 + 互动栏视觉升级
│   │   ├── ProfilePage.tsx     # [MODIFY] 个人主页头部视觉升级
│   │   ├── LoginPage.tsx       # [MODIFY] 表单视觉升级
│   │   ├── RegisterPage.tsx    # [MODIFY] 表单视觉升级
│   │   ├── PublishPage.tsx     # [MODIFY] 发布表单视觉升级
│   │   ├── EditProfilePage.tsx # [MODIFY] 表单视觉升级（保持一致性）
│   │   ├── RelationPage.tsx    # [MODIFY] 列表行视觉升级
│   │   └── MyLikesCollectsPage.tsx # [MODIFY] 复用 FeedCardGrid grid variant
│   ├── types/                  # [NO CHANGE] 数据契约不变
│   ├── api/                    # [NO CHANGE] 接口不变
│   ├── store/                  # [NO CHANGE]
│   ├── hooks/useCursorList.ts  # [NO CHANGE] 分页逻辑不变
│   └── mock/                   # [NO CHANGE] Mock 数据契约不变，需验证新 UI 与现有 mock 数据兼容
├── DESIGN.md# [MODIFY] 同步登记新增 Token、组件、瀑布流布局规则
└── AGENTS.md                    # [NO CHANGE，如新增约束再补充]
```

**Structure Decision**: 复用 `frontend/` monorepo 子项目现有目录结构（`src/{types,api,store,hooks,components,pages,mock}`），不新建顶层目录。仅新增 1 个组件文件（`components/Skeleton.tsx`，用于加载态骨架屏，满足宪法 IV 三态齐全 + 更精致的 Loading 体验）；其余改动均为对既有文件的**视觉与交互增强**，不改变文件组织方式，符合宪法「最小范围修改」原则（AGENTS.md）。

## Complexity Tracking

*无违规项，本节留空。*
