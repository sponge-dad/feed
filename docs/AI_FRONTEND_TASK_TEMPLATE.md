# AI 前端页面开发任务模板

> 在开始编码前，**必须**先阅读 `AGENTS.md` 与 `DESIGN.md`。
> **禁止**新增未经规范定义的颜色、字号、间距、圆角和阴影。如确需，先在 `DESIGN.md` 与 `src/styles.css` 的 `:root` 登记 Design Token。

---

## 一、任务信息

- **本次页面目标**：
- **涉及路由（新增 / 修改）**：
- **涉及接口（来自 `src/api/*` 的函数名）**：
- **需要复用的组件 / class**（Layout / Avatar / FeedCardGrid / `.btn` / `.input` / `.card` / `.tabs` / `.section-title` / `.text-*` 等）：
- **需要处理的页面状态**：加载中 / 空数据 / 错误 / 禁用 / 按钮 loading
- **响应式要求**：桌面（>560px）与移动端（≤560px）各自表现
- **不允许修改的范围**（路由 / 接口结构 / 其他页面 / 全局样式）：
- **验证命令**（如 `npm run lint && npm run typecheck && npm run build`）：
- **最终输出格式**：修改文件清单 + 是否新增了 Token / 公共组件 / 设计规则

## 二、执行前检查（必须逐项确认）

- [ ] 已阅读 `AGENTS.md`、`DESIGN.md` 与相关 Design Token
- [ ] 已搜索 `components/` 与 `styles.css` 现有公共组件 / class
- [ ] 已确认**不会重复实现**已有组件 / class
- [ ] 已确认**不会引入新的视觉规则**（新字号/间距/圆角/颜色须先登记 Token）
- [ ] 已确认**不会修改无关业务代码**（最小范围原则）

## 三、执行后检查（必须逐项确认）

- [ ] 已执行格式化、ESLint、typecheck、build 且通过
- [ ] 桌面与移动端（≤560px）布局均正常
- [ ] 加载中 / 空数据 / 错误 三态齐全
- [ ] 异步按钮具备 `loading` 与 `disabled`
- [ ] 危险操作具备二次确认
- [ ] 键盘可操作主要交互、`focus-visible` 可见
- [ ] 无新增硬编码颜色 / 字号 / 间距 / 圆角
- [ ] 输出修改文件清单，并说明是否新增 Token / 组件 / 规则

## 四、常见问题红线

1. 颜色一律引用 `--color-*` Token，禁止写 `#xxxxxx` / `rgb()`。
2. 字号引用 `--text-*`；间距引用 `--space-*`；圆角引用 `--radius-*`。
3. 输入用 `.input` / `.textarea`，按钮用 `.btn`，卡片用 `.card`，勿手写等价样式。
4. 删除等危险操作禁止再用原生 `confirm()`（已知技术债），新代码应规划 `ConfirmDialog`。
5. 不得为单页修改 `styles.css` 全局规则；局部需求用现有或新增 class，并同步 `DESIGN.md`。
