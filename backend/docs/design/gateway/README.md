# Gateway 服务设计文档目录

> 本目录存放 Gateway（HTTP 网关, 端口 8080）的设计方案。

## 文档索引

| 文件 | 主题 | 一句话说明 |
|------|------|-----------|
| [dataflow.md](./dataflow.md) | 全部 logic 数据流 | 用户/Feed/评论/互动/关系/内容画像各模块 BFF 聚合逻辑的入口、校验、主流程、数据源、失败降级、副作用、ASCII 图 |

## 与项目其他文档的关系

- 整体架构：`../architecture.md`
- 服务拆分与端口约定：`../service-design.md`
- 对外 API 契约：`../api-spec/http.md`
- 编码规范：`../../agent/dev-guidelines.md`
- 错误码：`../../../common/errorx/errorx.go`
