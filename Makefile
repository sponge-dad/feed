# ===================================================================
# Feed 分布式系统 Makefile
# ===================================================================

MODULE      := github.com/sponge-dad/feed
SERVICES    := user relation feed comment interaction
BUILD_DIR   := ./bin

GREEN := \033[0;32m
NC    := \033[0m

# 自动探测可用的 compose 调用方式：优先用 `docker compose`（v2插件），
# 找不到则退回独立的 `docker-compose`（v1二进制），两者都没有则留空，
# 留空时 up/down 等目标会提示先运行 install-compose。
# 用 shell 函数在 Makefile 加载时就探测一次，避免每个目标里重复判断。
DOCKER_COMPOSE := $(shell \
	if docker compose version >/dev/null 2>&1; then echo "docker compose"; \
	elif command -v docker-compose >/dev/null 2>&1; then echo "docker-compose"; \
	else echo ""; fi)

.PHONY: help
help: ## 显示帮助
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

# ---------------- 环境准备 ----------------
.PHONY: install-tools
install-tools: ## 安装工具链（protoc/goctl/插件）
	bash scripts/00-install-tools.sh

.PHONY: init
init: ## 初始化项目骨架
	bash scripts/01-init-project.sh

.PHONY: gen
gen: ## 用 goctl 生成服务骨架
	bash scripts/02-gen-services.sh

# ---------------- 依赖 ----------------
.PHONY: tidy
tidy: ## 整理 go 依赖
	go mod tidy

# ---------------- gateway api ----------------
.PHONY: api
api: ## 重新生成 gateway HTTP 代码（types/handler/routes）
	cd app/gateway && goctl api go -api api/gateway.api -dir . --style=goZero
	@# goctl 会在根目录生成默认入口与小写 ServiceContext 脚手架，
	@# 本项目入口固定为 cmd/api/gateway.go、依赖容器为 internal/svc/serviceContext.go，删除冗余文件
	@rm -f app/gateway/gateway.go app/gateway/internal/svc/servicecontext.go
	gofmt -w app/gateway

# ---------------- proto ----------------
.PHONY: proto
proto: ## 重新生成所有 proto 代码
	@for svc in $(SERVICES); do \
		echo "$(GREEN)generating $$svc proto...$(NC)"; \
		goctl rpc protoc api/proto/$$svc/$$svc.proto \
			--go_out=app/$$svc/rpc \
			--go-grpc_out=app/$$svc/rpc \
			--zrpc_out=app/$$svc/rpc --style=goZero; \
	done

# ---------------- 构建 ----------------
.PHONY: build
build: ## 编译所有服务（仅编译已实际生成 rpc 骨架的服务，跳过尚未创建的）
	@mkdir -p $(BUILD_DIR)
	@for svc in $(SERVICES); do \
		if [ -f app/$$svc/rpc/$$svc.go ]; then \
			echo "$(GREEN)building $$svc-rpc...$(NC)"; \
			go build -o $(BUILD_DIR)/$$svc-rpc ./app/$$svc/rpc; \
		else \
			echo "skip $$svc: app/$$svc/rpc/$$svc.go not found yet"; \
		fi; \
	done
	@if [ -f app/gateway/cmd/api/gateway.go ]; then \
		go build -o $(BUILD_DIR)/gateway ./app/gateway/cmd/api; \
	fi

# ---------------- 测试 ----------------
.PHONY: test
test: ## 运行测试
	go test -race ./...

.PHONY: fmt
fmt: ## 格式化代码
	gofmt -w .

# ---------------- 部署 ----------------
# 说明：DOCKER_COMPOSE 变量在文件头部自动探测得到，兼容 `docker compose`（v2插件）
# 和 `docker-compose`（v1独立二进制）两种写法。如果你的机器两者都没装，
# 运行 `make install-compose` 补装（脚本会优先走系统包管理器，失败则下载官方二进制）。
.PHONY: install-compose
install-compose: ## 安装 docker compose（机器上缺失时用这个）
	bash scripts/03-install-docker-compose.sh

.PHONY: up
up: check-compose ## 启动基础设施（MySQL/Redis/etcd/RocketMQ）
	$(DOCKER_COMPOSE) -f deploy/docker-compose.yaml up -d

.PHONY: down
down: check-compose ## 停止基础设施（保留数据）
	$(DOCKER_COMPOSE) -f deploy/docker-compose.yaml down

.PHONY: down-clean
down-clean: check-compose ## 停止基础设施并清空所有数据（重新开始）
	$(DOCKER_COMPOSE) -f deploy/docker-compose.yaml down -v

.PHONY: ps
ps: check-compose ## 查看基础设施容器状态
	$(DOCKER_COMPOSE) -f deploy/docker-compose.yaml ps

.PHONY: logs
logs: check-compose ## 查看基础设施日志
	$(DOCKER_COMPOSE) -f deploy/docker-compose.yaml logs -f

.PHONY: check-compose
check-compose:
ifeq ($(DOCKER_COMPOSE),)
	@echo "未检测到 docker compose，请先运行: make install-compose"
	@exit 1
endif
