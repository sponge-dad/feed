# Data Model:小红书风格内容社区前端

**Feature**: [spec.md](./spec.md) | **Date**: 2026-08-07

本功能**不改变**后端数据实体与API 契约（详见 spec Assumptions）。本文档记录两类内容：
1. 现有领域实体的引用说明（供实现阶段查阅字段，不重复定义）
2. 新增/变更的**前端组件契约**（UI props，本次改造涉及）

## 1. 现有领域实体（引用，不变更）

以下实体定义已存在于 `src/types/*.ts`，本次改造仅调整其**渲染方式**，字段结构保持不变：

| 实体 | 文件 | 用途 |
|------|------|------|
| `FeedCard` | `src/types/feed.ts` | 瀑布流/网格卡片展示数据（id, feed_type, title, cover_url, author, stats, interaction, created_at, source） |
| `FeedDetail` | `src/types/feed.ts` | 帖子详情页数据 |
| `FeedAuthor` / `FeedAuthorDetail` | `src/types/feed.ts` | 卡片作者简要信息 / 详情页作者信息（含关注状态） |
| `CommentEntry` / `CommentReply` | `src/types/comment.ts` | 评论楼中楼数据 |
| `UserDetail` | `src/types/user.ts` | 个人主页数据 |
| `User` | `src/types/user.ts` | 登录态用户简要信息 |

**验证规则**（不变）：均由后端保证，前端仅做展示层空值兜底（如`cover_url` 为空显示占位色，`nickname` 为空显示默认文案）。

## 2. 新增/变更的组件契约（UI Contracts）

### 2.1 `FeedCardGrid`（变更 — 新增可选 prop）

```typescript
interface FeedCardGridProps {
  items: FeedCard[];
  loading: boolean;
  hasMore: boolean;
  sentinelRef: React.RefObject<HTMLDivElement | null>;
  /**
   * 布局变体：
   * - 'waterfall'（默认）：瀑布流布局，用于首页信息流。列数随视口自适应，
   *   卡片高度随图片实际比例自适应（CSS column 布局）。
   * - 'grid'：等宽网格布局，用于个人主页/我的赞收藏页，视觉更规整、行对齐。
   */
  variant?: 'waterfall' | 'grid';
}
```

**变更点**: 新增 `variant`（可选，默认值 `'waterfall'`），现有 3 处调用方（`HomePage.tsx`）不传时行为不变（等同于当前实现升级为瀑布流），`ProfilePage.tsx`/`MyLikesCollectsPage.tsx`需显式传入 `variant="grid"` 以保持个人主页的规整网格视觉。

**向后兼容性**:✅ 完全兼容 — 新 prop 为可选，不影响现有类型检查。

### 2.2 `Skeleton`（新增组件）

```typescript
interface SkeletonProps {
  /** 骨架屏形状：'card' 瀑布流卡片占位 / 'line' 单行文字占位 / 'circle' 头像占位 */
  variant: 'card' | 'line' | 'circle';
  /** 宽度（CSS 值，如 '100%'、'120px'）；'circle' 变体需同时传 width 作为直径 */
  width?: string | number;
  /** 高度（CSS 值）；'card' 变体默认按4:3 或随机比例模拟瀑布流错落感 */
  height?: string | number;
}
```

**用途**: 首页/个人主页首次加载时，替代纯文字「加载中…」，展示 6-8 个骨架卡片，提升加载态视觉质感（响应 spec SC-001 首屏体验要求）。

**登记要求**: 按宪法 II 条，新组件必须登记入 `DESIGN.md` 第10.1 节公共组件清单，说明用途、变体、复用场景。

### 2.3 `Layout`（变更 — 内部结构调整，对外契约不变）

`Layout` 组件**无 props**（现状），本次改造仅调整其内部 JSX 结构与样式类名（侧边导航新增图标、分组、底部用户信息卡样式），不涉及对外契约变化，故不在此定义新接口，仅在实现阶段调整内部渲染逻辑。

## 3. Design Token 变更记录（非组件契约，但影响所有组件的视觉契约）

以下 Token 值调整需求（详见 research.md §5），实施时须先更新 `DESIGN.md` 再落地 `styles.css`：

| Token | 现值 | 新值 | 影响范围 |
|-------|------|------|---------|
| `--color-brand` | `#e5484d` | `#ff2442` | 全局品牌色引用处 |
| `--color-brand-hover` | `#d63d42` | 需重新计算（加深）| hover 态 |
| `--color-brand-active` | `#c5363b` | 需重新计算（更深）| active 态 |
| `--radius-lg`（卡片圆角） | `10px` | `12px` | `.card`, `.waterfall-card` 等 |
| 新增 `--color-bg-hover` | 无 | 新增 token（用于导航 hover 态背底） | 侧边导航、卡片列表行 |

**新增布局 Token（瀑布流专用）**:

| Token/规则 | 值 | 说明 |
|-----------|-----|------|
| 瀑布流列数（≥1200px） | 5 列 | `column-count: 5` |
| 瀑布流列数（900-1200px） | 4 列 | |
| 瀑布流列数（640-900px） | 3 列 | |
| 瀑布流列数（<640px） | 2 列 |移动端强制两列（符合 spec FR-025 移动端两列要求） |
| 瀑布流列间距 | `var(--space-3)`（12px） | 复用现有间距 token，不新增 |

## 4. 状态流转（State Transitions）— 复用现状，无变更

互动状态（点赞/收藏/关注）的乐观更新逻辑保持现状不变（`FeedDetailPage.tsx`/`ProfilePage.tsx` 中的 `toggleLike`/`toggleCollect`/`toggleFollow` 函数逻辑不改，仅其触发的按钮视觉升级）。
