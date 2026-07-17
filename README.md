# Feed 分布式 Feed 流系统

一个类抖音/小红书的分布式 Feed 流系统，基于 go-zero 微服务框架。

## 技术栈

| 类别 | 选型 |
|------|------|
| 语言 | Go 1.23+ |
| 微服务框架 | go-zero |
| 服务通信 | gRPC + Protobuf |
| 对外协议 | HTTP REST（网关） |
| 数据库 | MySQL 8.0（主从） |
| 缓存 | Redis 7 |
| 消息队列 | RocketMQ |
| 对象存储 | 腾讯云 COS + CDN |
| 容器编排 | Kubernetes（3 CVM 节点） |

## 服务架构

| 服务 | 职责 | 端口 |
|------|------|------|
| gateway | 对外 HTTP 网关，鉴权/限流/聚合 | 8080 |
| user | 注册登录、用户信息、城市定位 | 9001 |
| relation | 关注取关、粉丝列表、大V判定 | 9002 |
| feed | 发帖删帖、推荐/关注/同城三种流 | 9003 |
| comment | 楼中楼评论 | 9004 |
| interaction | 点赞、收藏、计数 | 9005 |

## 目录结构

```
feed/
├── docs/                  # 设计文档
│   ├── service-design.md  # 微服务拆分方案
│   ├── data-model.md      # 数据模型（MySQL + Redis）
│   ├── dev-guidelines.md  # 开发规范（注释/分层/错误处理约定，写代码前必读）
│   └── api/               # REST API 定义（各模块）
├── api/proto/             # 内部 gRPC proto 契约
├── app/                   # 各微服务代码
│   ├── user/ relation/ feed/ comment/ interaction/  # gRPC 服务
│   └── gateway/           # HTTP 网关
├── common/                # 跨服务公共代码
│   ├── response/          # 统一响应结构
│   ├── errorx/            # 统一错误码
│   ├── jwtx/              # JWT 工具
│   ├── idgen/             # Snowflake ID
│   ├── ipx/               # IP 定位
│   └── mq/                # RocketMQ 封装
├── deploy/                # 部署（SQL/K8s/config）
├── scripts/               # 初始化脚本
└── Makefile
```

## 快速开始

> 前置条件：已安装 Go 1.21+。系统 TencentOS Server / CentOS / RHEL 系。

在 CVM 上按顺序执行三个脚本即可完成环境搭建和骨架生成：

```bash
# 1. 安装工具链（protoc / goctl / 插件）
bash scripts/00-install-tools.sh
source ~/.bashrc          # 使 goctl 命令生效

# 2. 初始化项目骨架（go.mod + 目录）
bash scripts/01-init-project.sh

# 3. 用 goctl 生成各服务 gRPC 骨架
bash scripts/02-gen-services.sh
```

或使用 Makefile：

```bash
make install-tools   # = 步骤1
make init            # = 步骤2
make gen             # = 步骤3
make help            # 查看所有命令
```

## 开发进度

- [x] 需求定稿
- [x] 技术选型
- [x] 微服务拆分设计（docs/service-design.md）
- [x] 数据模型设计（docs/data-model.md）
- [x] REST API 契约（docs/api/）
- [x] 项目骨架 + 公共代码 + 初始化脚本
- [ ] User 服务实现
- [ ] Relation 服务实现
- [ ] Feed 服务实现
- [ ] Comment 服务实现
- [ ] Interaction 服务实现
- [ ] API Gateway 实现
- [ ] Docker Compose 本地联调
- [ ] K8s 部署
