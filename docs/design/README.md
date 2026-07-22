# design 目录

> 本目录存放 **Feed 项目的系统设计文档**，包括整体架构、服务拆分、数据模型、接口契约等实现细节。是理解"系统如何设计"的核心参考资料。

## 文件说明

| 文件 | 内容 |
|------|------|
| [architecture.md](./architecture.md) | 分布式 Feed 流系统完整技术方案（架构总览、数据流、推拉结合、部署、开发阶段规划） |
| [service-design.md](./service-design.md) | 微服务拆分方案、各服务职责、服务间调用关系、RocketMQ Topic 归属 |
| [data-model.md](./data-model.md) | MySQL 表结构、Redis 数据结构、数据归属说明 |
| [api-spec/](./api-spec/) | 对外 REST API 契约总纲与各模块接口定义 |
| [feed/](./feed/) | Feed 服务（9003）分模块设计方案：数据模型、帖子管理、三种 Timeline、缓存、MQ、测试 |
| [interaction/](./interaction/) | Interaction 服务（9005）分模块设计方案：点赞/收藏、计数/状态、用户列表、缓存一致性、MQ、测试 |

## 与设计相关的代码目录

| 目录 | 说明 |
|------|------|
| `api/proto/` | 内部 gRPC Proto 契约，与 `service-design.md` 中服务接口对应 |
| `deploy/sql/` | 数据库初始化脚本，与 `data-model.md` 中表结构对应 |
| `app/{service}/` | 各微服务实现，与 `service-design.md` 中职责划分对应 |

## 阅读顺序建议

1. 先读 `architecture.md`，了解整体方案。
2. 再读 `service-design.md`，理解服务拆分与调用关系。
3. 最后读 `data-model.md` 与 `api-spec/`，掌握具体数据与接口契约。
