# Relation 服务设计文档目录

> 本目录存放 Relation 微服务（端口 9002）的设计方案。

## 文档索引

| 文件 | 主题 | 一句话说明 |
|------|------|-----------|
| [dataflow.md](./dataflow.md) | 全部 logic 数据流 | 关注/取关/关注与粉丝列表/是否关注/大V判定的入口、校验、主流程、数据源、失败降级、副作用、ASCII 图 |

## 与项目其他文档的关系

- 整体架构：`../architecture.md`
- 服务拆分与端口约定：`../service-design.md`
- 全局数据模型：`../data-model.md`
- 编码规范：`../../agent/dev-guidelines.md`
- 错误码：`../../../common/errorx/errorx.go`
