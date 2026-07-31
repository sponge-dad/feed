// Stylelint 配置：基于 stylelint-config-standard，仅做安全、低噪的检查。
// 设计原则：聚焦「无效 CSS / 重复属性 / 明显错误」，避免对历史代码强推风格导致大量报错。
//
// 不启用 color-no-hex：:root 中 Design Token 定义合法需要十六进制；
// 业务 class 的硬编码颜色由人工 + DESIGN.md 规范约束（见 AGENTS.md）。
// 不强制 modern 颜色记法（color-function-notation / alpha-value-notation）：
// 避免把现有 rgba(0,0,0,.45) 等全部改写为 rgb() 引发的噪声。
// !important 仅在 reduced-motion 无障碍复位中使用，故此处放行（见 DESIGN.md §17 红线说明）。
export default {
  extends: ['stylelint-config-standard'],
  rules: {
    'declaration-no-important': null,
    'no-descending-specificity': null,
    'color-function-notation': null,
    'alpha-value-notation': null,
  },
};
