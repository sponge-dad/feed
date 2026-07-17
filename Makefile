# ===================================================================
# Feed 分布式系统 Makefile
# ===================================================================

MODULE      := github.com/sponge-dad/feed
SERVICES    := user relation feed comment interaction
BUILD_DIR   := ./bin

GREEN := \033[0;32m
NC    := \033[0m

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
# 注：用 `docker compose`（Docker 官方内置子命令）而不是独立的 `docker-compose` 二进制，
# 后者在较新的 Docker 安装方式下可能不存在。
.PHONY: up
up: ## 启动基础设施（MySQL/Redis/etcd/RocketMQ）
	docker compose -f deploy/docker-compose.yaml up -d

.PHONY: down
down: ## 停止基础设施（保留数据）
	docker compose -f deploy/docker-compose.yaml down

.PHONY: down-clean
down-clean: ## 停止基础设施并清空所有数据（重新开始）
	docker compose -f deploy/docker-compose.yaml down -v

.PHONY: ps
ps: ## 查看基础设施容器状态
	docker compose -f deploy/docker-compose.yaml ps

.PHONY: logs
logs: ## 查看基础设施日志
	docker compose -f deploy/docker-compose.yaml logs -f
