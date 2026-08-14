# Content 服务设计文档目录

> 本目录存放 Content 微服务（端口 9007）的设计方案。

## 文档索引

| 文件 | 主题 | 一句话说明 |
|------|------|-----------|
| [dataflow.md](./dataflow.md) | 全部 logic 数据流 | 内容画像（单查/批量/重试）、结构化检索、创作者纠错反馈的入口、校验、主流程、数据源、失败降级、副作用、ASCII 图 |

## 与项目其他文档的关系

- 整体架构：`../architecture.md`
- 服务拆分与端口约定：`../service-design.md`
- 全局数据模型：`../data-model.md`
- 内容分析方案：`../agent/04-content-analysis.md`
- 编码规范：`../../agent/dev-guidelines.md`
- 错误码：`../../../common/errorx/errorx.go`
