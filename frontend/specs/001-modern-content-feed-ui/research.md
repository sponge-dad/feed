# Research: 小红书风格内容社区前端

**Feature**: [spec.md](./spec.md) | **Date**: 2026-08-07

本文档解决 Technical Context 中的未知项与关键设计决策的调研结论。当前 Technical Context 无`NEEDS CLARIFICATION` 标记（技术栈已由现有项目锁定），本文档聚焦于**视觉/交互实现方案的选型依据**。

## 1.瀑布流布局实现方式

**Decision**: 使用 CSS `column-count` + `break-inside: avoid` 实现纯 CSS 瀑布流，不引入 JS 瀑布流库（如 `react-masonry-css`、`masonic`）。

**Rationale**:
- 项目宪法明确禁止引入新的样式方案/UI 库；CSS Multi-column 布局是浏览器原生特性，零依赖。
- 现代浏览器（Chrome/Safari/Firefox/Edge 最新两版）对 `column-count` 支持良好，移动端 iOS Safari 14+/Android Chrome 90+ 均完整支持。
- 数据是从 API顺序返回的列表（非预知每张图片的宽高比），CSS column 布局按源顺序纵向填充，视觉效果与小红书列表流一致；相比 JS 瀑布流库（需要提前测量每个元素高度做绝对定位），CSS 方案更简单、性能更好、无布局抖动。

**Alternatives considered**:
- **JS 瀑布流库（react-masonry-css / masonic）**：需要新增运行时依赖，违反宪法「不引入新依赖」约束；且需要测量图片真实高度才能做绝对定位排列，实现复杂度高于收益。
- **CSS Grid + `grid-auto-flow: dense`**：Grid 的 dense 模式仍需要显式指定每个item 的 row-span，无法根据图片实际高度自动计算，不适合动态高度卡片。
- **原生 CSS `column-count`（选定）**：唯一不需要JS 参与、不需要预知尺寸即可实现视觉瀑布流的方案。已知限制：项目内DOM 顺序与视觉顺序在多列flow 下可能不完全保持"从左到右逐行"（column 布局是先填满第一列再填第二列），但对纯浏览型Feed 场景该差异不影响用户体验（小红书客户端本身也是先填列后错位排列）。

## 2.骨架屏（Skeleton）实现

**Decision**: 新增轻量 `Skeleton` 组件，仅用 CSS 渐变动画（`background-position` shimmer 效果）实现，不引入第三方骨架屏库。

**Rationale**: 现有 DESIGN.md 10.3 节已将 `Skeleton` 列为"待建组件"，本次改造首页加载体验需要它替代纯文字 loading，属于计划内的合理新增，需登记 DESIGN.md。CSS-only 实现零依赖，且已有 `--motion-*`/`--ease-standard` token 可复用于动效时长。

**Alternatives considered**: 引入 `react-loading-skeleton` — 违反"不引入新 UI 库"约束，且需求简单（矩形色块 + shimmer），没必要引入依赖。

## 3. 图片懒加载与占位

**Decision**: 继续使用原生 `<img loading="lazy">` + 占位背景色（`--color-bg-placeholder`），不引入 `IntersectionObserver` 自定义懒加载或图片 CDN 裁剪参数拼接。

**Rationale**: 现有代码已使用 `loading="lazy"`，浏览器原生支持覆盖率高；后端返回的图片 URL 来自 COS+CDN，前端不负责裁剪（已在 spec Assumptions 中声明范围外）。保持现状最小化改动。

**Alternatives considered**: 图片 CDN 参数拼接裁剪（如 `?imageView2/...`）— 需要后端/CDN 侧确认参数规则，超出本次前端视觉改造范围，列为未来优化项。

## 4. 卡片点击态与路由跳转方式

**Decision**: 瀑布流卡片继续使用 `<Link>`（react-router-dom）包裹整卡，不使用 `onClick` + `useNavigate` 编程式跳转。

**Rationale**: 符合宪法可访问性原则（语义化 `<a>` 标签，支持 Ctrl/Cmd+Click 新开标签页、右键菜单等浏览器原生行为），且与现有 `FeedCardGrid` 实现一致，零学习成本。

## 5. 颜色与视觉语言基准

**Decision**: 品牌主色由 `#e5484d` 调整为更贴近小红书视觉印象的 `#ff2442`；背景色体系、圆角、间距保持 4px/8px 基准网格不变，仅数值微调（如卡片圆角从 10px 调整为 12px，按钮从方形圆角改为全圆角 pill 形态以贴近目标视觉）。

**Rationale**: spec 要求"好看、设计语言统一、类小红书"，品牌色是视觉识别的核心；圆角/间距延续现有 4px 基准体系是保持"统一"的关键约束（宪法 I 条禁止无Token 的随意数值）。所有调整值必须在 DESIGN.md 登记后才能在 `styles.css` 实现。

**Alternatives considered**: 完全保留现有 `#e5484d` 品牌色 — 可行但视觉上与"类小红书"的期望有差距；引入多个品牌色 — 违反宪法"单一品牌色"原则，不采纳。

## 6. 现有组件契约兼容性

**Decision**: `FeedCardGrid` 新增可选 prop `variant?: 'waterfall' | 'grid'`（默认 `'waterfall'`），向后兼容现有调用方（`HomePage`/`ProfilePage`/`MyLikesCollectsPage`）；不修改 `items`/`loading`/`hasMore`/`sentinelRef` 现有必填 props的类型与语义。

**Rationale**: 保持组件对外契约的向后兼容，是最小改动范围原则的直接体现；`variant` 是纯视觉分支（瀑布流 vs 等宽网格），不影响数据流。

## 7. Mock 模式兼容性验证方式

**Decision**: 改造过程中始终通过 `VITE_USE_MOCK=true` 启动本地验证，确保新UI 与 `src/mock/index.ts` 现有 mock 数据结构无缝对接，不需要修改 mock 数据（因为本次不改变数据契约）。

**Rationale**: spec Assumption 中已声明"改造不破坏 Mock 体验"；由于类型层完全不变，只是渲染层变化，理论上无需改mock，仅需以此方式做端到端验证。

---

## Summary of Resolved Unknowns

无 `NEEDS CLARIFICATION` 项遗留。所有技术决策已确定，可进入 Phase 1（Design & Contracts）。
