# agent 目录

> 本目录存放 **vibe coding 时约束 AI 的指引文档**。AI 在编写代码、接口文档或进行项目维护前，必须优先阅读本目录下的相关规范。

## 文件说明

| 文件 | 用途 | 何时阅读 |
|------|------|----------|
| [dev-guidelines.md](./dev-guidelines.md) | Go 代码开发规范（文件注释、分层、错误处理、缓存、命名等） | 写任何 `.go` 文件前 |
| [api-writing-guide.md](./api-writing-guide.md) | REST API 文档编写规范（URL、请求响应、分页、错误码、检查清单） | 编写或修改 `docs/design/api-spec/` 下接口文档前 |
| [proto-writing-guide.md](./proto-writing-guide.md) | 内部 gRPC `.proto` 文件编写规范（package、消息、service、字段、枚举、生成规则） | 新增或修改 `api/proto/` 下 proto 前 |
| [go-zero-api-writing-guide.md](./go-zero-api-writing-guide.md) | go-zero 网关 `.api` 文件编写规范（类型、路由、鉴权、生成规则） | 新增或修改 `app/gateway/api/` 下 api 文件前 |
| [gateway-standard.md](./gateway-standard.md) | Gateway 服务开发标准手册（目录结构、API、配置、ServiceContext、Handler/Logic、中间件、鉴权、错误处理、安全、代码生成、测试部署） | 新增或修改 Gateway 服务（`app/gateway/`）代码前 |
| [doc-writing-guide.md](./doc-writing-guide.md) | 文档编写规范（全仓库 `docs/` 下文档的生成、修订与组织统一标准） | 新增或修订 `docs/` 下任何文档前 |
| [bug-summary-sop.md](./bug-summary-sop.md) | Bug 总结文档编写 SOP（排查流程 + 产出模板，归档于 `docs/problem/`） | 用户要求「总结 / 记录某个 bug」并产出文档前 |
| [logic-dataflow-guide.md](./logic-dataflow-guide.md) | Logic 数据流生成提示词（可复制提示词 + 输出模板 + 示例，用于为任意 logic 生成数据流说明） | 需要梳理/生成某个 logic 的数据流文档前 |

## 使用原则

1. **优先于个人能力**：当 AI 的默认写法与本目录规范冲突时，以本目录为准。
2. **与设计文档互补**：本目录约束"怎么写"，`docs/design/` 目录说明"写什么"。两者结合使用。
3. **保持更新**：新增 AI 协作规范时，优先补充到本目录，避免散落在各处。
