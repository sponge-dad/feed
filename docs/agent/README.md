# agent 目录

> 本目录存放 **vibe coding 时约束 AI 的指引文档**。AI 在编写代码、接口文档或进行项目维护前，必须优先阅读本目录下的相关规范。

## 文件说明

| 文件 | 用途 | 何时阅读 |
|------|------|----------|
| [dev-guidelines.md](./dev-guidelines.md) | Go 代码开发规范（文件注释、分层、错误处理、缓存、命名等） | 写任何 `.go` 文件前 |
| [api-writing-guide.md](./api-writing-guide.md) | REST API 文档编写规范（URL、请求响应、分页、错误码、检查清单） | 编写或修改 `docs/design/api-spec/` 下接口文档前 |
| [proto-writing-guide.md](./proto-writing-guide.md) | 内部 gRPC `.proto` 文件编写规范（package、消息、service、字段、枚举、生成规则） | 新增或修改 `api/proto/` 下 proto 前 |
| [go-zero-api-writing-guide.md](./go-zero-api-writing-guide.md) | go-zero 网关 `.api` 文件编写规范（类型、路由、鉴权、生成规则） | 新增或修改 `app/gateway/api/` 下 api 文件前 |

## 使用原则

1. **优先于个人能力**：当 AI 的默认写法与本目录规范冲突时，以本目录为准。
2. **与设计文档互补**：本目录约束"怎么写"，`docs/design/` 目录说明"写什么"。两者结合使用。
3. **保持更新**：新增 AI 协作规范时，优先补充到本目录，避免散落在各处。
