# docs 目录

> Feed 项目文档总索引。所有设计文档与 AI 协作规范均位于此目录。新增或调整文档时，请同步更新对应子目录 `README.md` 与本文档，并遵循 [文档编写规范](./agent/doc-writing-guide.md)。

---

## 目录结构

```
docs/
├── agent/       # AI 与开发规范（编码、API、proto、网关、文档编写）
└── design/      # 方案与设计（架构、服务设计、数据模型、API 契约）
    ├── api-spec/    # 对外 REST API 契约
    ├── feed/        # Feed 服务设计
    └── interaction/ # Interaction 服务设计
```

## 文档索引

### agent/ — AI 与开发规范

| 文档 | 用途 |
|------|------|
| [dev-guidelines.md](./agent/dev-guidelines.md) | Go 代码开发规范（文件注释、分层、错误处理、缓存、命名等） |
| [api-writing-guide.md](./agent/api-writing-guide.md) | REST API 文档编写规范（URL、请求响应、分页、错误码、检查清单） |
| [proto-writing-guide.md](./agent/proto-writing-guide.md) | 内部 gRPC `.proto` 文件编写规范 |
| [go-zero-api-writing-guide.md](./agent/go-zero-api-writing-guide.md) | go-zero 网关 `.api` 文件编写规范 |
| [gateway-standard.md](./agent/gateway-standard.md) | Gateway 服务开发标准手册 |
| [doc-writing-guide.md](./agent/doc-writing-guide.md) | **文档编写规范（全仓库文档生成/修订统一标准）** |

### design/ — 方案与设计

| 文档 | 用途 |
|------|------|
| [architecture.md](./design/architecture.md) | 系统总体架构 |
| [service-design.md](./design/service-design.md) | 服务拆分与调用关系 |
| [data-model.md](./design/data-model.md) | 数据模型 |
| [api-spec/README.md](./design/api-spec/README.md) | 对外 REST API 契约总览 |
| [feed/README.md](./design/feed/README.md) | Feed 服务设计 |
| [interaction/README.md](./design/interaction/README.md) | Interaction 服务设计 |

### 待归置（散落文档）

以下文档尚未按 [文档编写规范](./agent/doc-writing-guide.md) 归类，留待后续结构轮次处理：

| 文档 | 说明 | 建议归属 |
|------|------|----------|
| [relation-service-test-plan.md](./relation-service-test-plan.md) | Relation 服务测试方案 | `docs/design/relation/` |
| [user-service-test-plan.md](./user-service-test-plan.md) | User 服务测试方案 | `docs/design/user/` |
| [user-service-bcrypt-optimization.md](./user-service-bcrypt-optimization.md) | User 服务 bcrypt 优化记录 | `docs/design/user/` |

## 关联文档

- [项目总规范](../AGENTS.md)
