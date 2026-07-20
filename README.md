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
├── deploy/                # 部署
│   ├── sql/               # 各服务建表脚本
│   ├── docker-compose.yaml # 本地/CVM开发环境基础设施（MySQL/Redis/etcd/RocketMQ）
│   └── k8s/               # K8s 部署配置（待补充）
├── scripts/               # 初始化脚本
└── Makefile
```

## 快速开始

> 前置条件：已安装 Go 1.21+、Docker Engine。系统 TencentOS Server / CentOS / RHEL 系。
>
> 注意 1：很多 CVM 上通过 yum/dnf 装的 Docker 只有引擎本身，**不带 compose 功能**
> （`docker compose version` 会报 `unknown command`）。运行 `make up` 前先执行
> `make install-compose` 补装，Makefile 里的 up/down 等命令会自动探测用
> `docker compose`（v2插件）还是 `docker-compose`（v1二进制），两者都没装时会
> 提示你先跑这个命令，不会报出难懂的错误。
>
> 注意 2：如果 CVM 上的 Docker 服务是 `disabled`/`inactive` 状态（`systemctl status
> docker` 能看到），先 `sudo systemctl start docker && sudo systemctl enable docker`。
> 这种情况常见于**同时也是 K8s 节点**的机器（K8s 1.24+ 默认用 containerd 做运行时，
> docker.service 装了但没启用）。
>
> 注意 3：**如果这台机器本身是 K8s 集群节点（尤其是 master）**，K8s 控制平面自带的
> etcd 会占用宿主机的 `2379` 端口，与本项目 `deploy/docker-compose.yaml` 里 etcd
> 容器的默认端口冲突。当前配置已把开发用 etcd 的宿主机映射端口改成了 **2479**
> （容器内部仍是 2379，不影响 compose 网络内部通信），`app/user/rpc/etc/user.yaml`
> 等各服务配置里的 `Etcd.Hosts` 也要保持用 `127.0.0.1:2479`，两边必须一致，
> 否则 RPC 服务会误连到 K8s 集群自己的 etcd 上，导致服务发现异常（表现可能是
> "看起来连上了但服务发现不到彼此"，比报错更难排查）。

### 1. 安装工具链 + 生成项目骨架

```bash
# 安装工具链（protoc / goctl / 插件）
bash scripts/00-install-tools.sh
source ~/.bashrc          # 使 goctl 命令生效

# 初始化项目骨架（go.mod + 目录）
bash scripts/01-init-project.sh

# 用 goctl 生成各服务 gRPC 骨架
bash scripts/02-gen-services.sh
```

或使用 Makefile：

```bash
make install-tools   # 安装工具链
make init            # 初始化骨架
make gen             # 生成 gRPC 骨架
make help            # 查看所有命令
```

### 2. 启动基础设施（MySQL / Redis / etcd / RocketMQ）

开发环境的中间件统一用 Docker Compose 管理，配置见 `deploy/docker-compose.yaml`：

```bash
make install-compose   # 机器上没有 docker compose 时先装一下（幂等，装过会自动跳过）
make up          # 一键启动 MySQL/Redis/etcd/RocketMQ
make ps           # 查看容器状态
make logs         # 查看日志（Ctrl+C 退出，不影响容器运行）
make down         # 停止（保留数据，下次 up 数据还在）
make down-clean   # 停止并清空所有数据（重新开始用这个）
```

MySQL 首次启动会自动执行 `deploy/sql/` 下所有 `.sql` 建表脚本（仅首次建库时生效，
后续新增的表需要手动执行 SQL）。RocketMQ 管理台启动后可访问
`http://<CVM_IP>:9877` 查看 Topic/消息堆积情况。

各服务 `etc/*.yaml` 里默认配置的连接地址（`127.0.0.1:3306`、`127.0.0.1:6379`、
`127.0.0.1:2479` 等）和上述 Compose 暴露的端口是对齐的，本机跑服务无需额外改配置。
etcd 用的是 2479 而非默认的 2379，原因见上面"注意 3"。

### 3. 启动某个 RPC 服务

```bash
cd app/user/rpc && go run user.go -f etc/user.yaml
```

## 开发进度

- [x] 需求定稿
- [x] 技术选型
- [x] 微服务拆分设计（docs/service-design.md）
- [x] 数据模型设计（docs/data-model.md）
- [x] REST API 契约（docs/api/）
- [x] 项目骨架 + 公共代码 + 初始化脚本
- [x] Docker Compose 本地联调环境（MySQL/Redis/etcd/RocketMQ）
- [x] User 服务实现（见 `app/user/rpc/README.md`）
- [x] Relation 服务实现（见 `app/relation/rpc/README.md`）
- [ ] Feed 服务实现
- [ ] Comment 服务实现
- [ ] Interaction 服务实现
- [ ] API Gateway 实现
- [ ] K8s 部署
