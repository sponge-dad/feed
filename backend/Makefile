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
	@for svc in $(SERVICES) content agent; do \
		if [ ! -f api/proto/$$svc/$$svc.proto ]; then \
			echo "skip $$svc: api/proto/$$svc/$$svc.proto not found yet"; \
			continue; \
		fi; \
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

# ---------------- 运行服务（后台） ----------------
# 日志目录规范：每个服务独立子目录 $(LOG_DIR)/<服务名>/，与各服务 etc/*.yaml 里的
# Log.Path 保持一致，避免多服务/多次启动互相覆盖。目录内两类文件：
#   - access.log / error.log / severe.log / slow.log / stat.log：go-zero logx 按 Log 配置写出的结构化日志（按天滚动，保留7天并压缩）
#   - stdout.log：进程自身 stdout/stderr（例如 logx 初始化之前的致命错误、未捕获panic 堆栈），仅作兜底
LOG_DIR := ./logs

# run：后台启动 5 个 RPC 服务 + 网关。用 setsid 使进程脱离当前 shell 会话，
# 即使 make 退出 / 终端关闭也不会被杀。
# 注意：必须在 backend/ 目录下执行（各 yaml 里 Log.Path 是相对路径），否则日志会落到别处。
# 前置：先 make build 生成 ./bin 下二进制；基础设施需已 make up。
.PHONY: run
run: ## 后台启动全部 RPC 服务 + 网关（需先 make build，日志见 logs/<服务名>/）
	@for s in $(SERVICES); do \
		mkdir -p $(LOG_DIR)/$$s; \
		echo "$(GREEN)starting $$s-rpc...$(NC)"; \
		setsid $(BUILD_DIR)/$$s-rpc -f app/$$s/rpc/etc/$$s.yaml > $(LOG_DIR)/$$s/stdout.log 2>&1 < /dev/null & \
	done
	@mkdir -p $(LOG_DIR)/gateway
	@echo "$(GREEN)starting gateway...$(NC)"
	@setsid $(BUILD_DIR)/gateway -f app/gateway/etc/gateway.yaml > $(LOG_DIR)/gateway/stdout.log 2>&1 < /dev/null &
	@echo "services started -> status: make status | logs: make svclogs"

# run-svc：后台启动单个自定义服务。用法 make run-svc svc=<服务名>
#   svc 可为 user/relation/feed/comment/interaction（RPC）或 gateway（网关）
# 若对应二进制尚未编译，会自动 make build 该服务；日志落在 logs/<服务名>/。
# 注意：必须在 backend/ 目录下执行（各 yaml 里 Log.Path 是相对路径）。
.PHONY: run-svc
run-svc: ## 后台启动单个服务：make run-svc svc=<user|relation|feed|comment|interaction|gateway>
	@if [ -z "$(svc)" ]; then echo "用法: make run-svc svc=<服务名>"; exit 1; fi; \
	case "$(svc)" in \
		user|relation|feed|comment|interaction) \
			if [ ! -x "$(BUILD_DIR)/$(svc)-rpc" ]; then echo "$(svc)-rpc 未编译，正在编译..."; go build -o $(BUILD_DIR)/$(svc)-rpc ./app/$(svc)/rpc; fi; \
			mkdir -p $(LOG_DIR)/$(svc); \
			echo "$(GREEN)starting $(svc)-rpc...$(NC)"; \
			setsid $(BUILD_DIR)/$(svc)-rpc -f app/$(svc)/rpc/etc/$(svc).yaml > $(LOG_DIR)/$(svc)/stdout.log 2>&1 < /dev/null & \
			;; \
		gateway) \
			if [ ! -x "$(BUILD_DIR)/gateway" ]; then echo "gateway 未编译，正在编译..."; go build -o $(BUILD_DIR)/gateway ./app/gateway/cmd/api; fi; \
			mkdir -p $(LOG_DIR)/gateway; \
			echo "$(GREEN)starting gateway...$(NC)"; \
			setsid $(BUILD_DIR)/gateway -f app/gateway/etc/gateway.yaml > $(LOG_DIR)/gateway/stdout.log 2>&1 < /dev/null & \
			;; \
		*) echo "未知服务: $(svc)，可选: $(SERVICES) gateway"; exit 1; \
	esac; \
	echo "started -> status: make status | logs: make svclogs"

# stop-svc：停止单个服务，与 run-svc 对应。
.PHONY: stop-svc
stop-svc: ## 停止单个服务：make stop-svc svc=<user|relation|feed|comment|interaction|gateway>
	@if [ -z "$(svc)" ]; then echo "用法: make stop-svc svc=<服务名>"; exit 1; fi; \
	case "$(svc)" in \
		user|relation|feed|comment|interaction) pkill -9 -f "$(svc)-rpc" 2>/dev/null || true; echo "$(svc)-rpc stopped";; \
		gateway) pkill -9 -f "bin/gateway" 2>/dev/null || true; echo "gateway stopped";; \
		*) echo "未知服务: $(svc)"; exit 1;; \
	esac

# run-svcs：一次性后台启动多个服务。用法 make run-svcs svcs=feed,gateway
# 支持逗号或空格分隔，内部逐个调用 run-svc（自动编译缺失二进制、日志落 logs/<服务名>/）。
.PHONY: run-svcs
run-svcs: ## 后台启动多个服务：make run-svcs svcs=<feed,gateway,...>
	@if [ -z "$(svcs)" ]; then echo "用法: make run-svcs svcs=<服务名,逗号或空格分隔>"; exit 1; fi; \
	IFS=', ' read -ra __svcs <<< "$(svcs)"; \
	for s in "$${__svcs[@]}"; do \
		echo "-> $$s"; \
		$(MAKE) run-svc svc=$$s || exit 1; \
	done; \
	echo "all requested services started -> status: make status | logs: make svclogs"

# run-content / run-content-worker / run-agent：后台启动 Agent 特性新增服务（独立进程）。
# 前置：make build 已编译对应二进制；基础设施已 make up；yaml 中 ${ENV} 占位需先在环境配置。
# 日志规范与 run-svc 一致：logs/<服务名>/stdout.log。
.PHONY: run-content
run-content: ## 后台启动 Content RPC（9007，需先 make build，日志见 logs/content/）
	@if [ ! -x "$(BUILD_DIR)/content-rpc" ]; then echo "content-rpc 未编译，正在编译..."; go build -o $(BUILD_DIR)/content-rpc ./app/content/rpc; fi; \
	mkdir -p $(LOG_DIR)/content; \
	echo "$(GREEN)starting content-rpc...$(NC)"; \
	setsid $(BUILD_DIR)/content-rpc -f app/content/rpc/etc/content.yaml > $(LOG_DIR)/content/stdout.log 2>&1 < /dev/null &

.PHONY: run-content-worker
run-content-worker: ## 后台启动 Content Worker（独立进程，消费 feed-created/feed-deleted 分析，日志见 logs/content-worker/）
	@if [ ! -x "$(BUILD_DIR)/content-worker" ]; then echo "content-worker 未编译，正在编译..."; go build -o $(BUILD_DIR)/content-worker ./app/content/worker; fi; \
	mkdir -p $(LOG_DIR)/content-worker; \
	echo "$(GREEN)starting content-worker...$(NC)"; \
	setsid $(BUILD_DIR)/content-worker -f app/content/worker/etc/content-worker.yaml > $(LOG_DIR)/content-worker/stdout.log 2>&1 < /dev/null &

.PHONY: run-agent
run-agent: ## 后台启动 Agent RPC（9006，需先 make build，日志见 logs/agent/）
	@if [ ! -x "$(BUILD_DIR)/agent-rpc" ]; then echo "agent-rpc 未编译，正在编译..."; go build -o $(BUILD_DIR)/agent-rpc ./app/agent/rpc; fi; \
	mkdir -p $(LOG_DIR)/agent; \
	echo "$(GREEN)starting agent-rpc...$(NC)"; \
	setsid $(BUILD_DIR)/agent-rpc -f app/agent/rpc/etc/agent.yaml > $(LOG_DIR)/agent/stdout.log 2>&1 < /dev/null &

.PHONY: stop-content
stop-content: ## 停止 Content RPC 与 Content Worker
	@pkill -9 -f "$(BUILD_DIR)/content-rpc" 2>/dev/null || true
	@pkill -9 -f "$(BUILD_DIR)/content-worker" 2>/dev/null || true
	@echo "content services stopped"

.PHONY: stop-agent
stop-agent: ## 停止 Agent RPC
	@pkill -9 -f "$(BUILD_DIR)/agent-rpc" 2>/dev/null || true
	@echo "agent stopped"

# stop-svcs：一次性停止多个服务，与 run-svcs 对应。
.PHONY: stop-svcs
stop-svcs: ## 停止多个服务：make stop-svcs svcs=<feed,gateway,...>
	@if [ -z "$(svcs)" ]; then echo "用法: make stop-svcs svcs=<服务名,...>"; exit 1; fi; \
	IFS=', ' read -ra __svcs <<< "$(svcs)"; \
	for s in "$${__svcs[@]}"; do $(MAKE) stop-svc svc=$$s; done; \
	echo "done"

.PHONY: stop
stop: ## 停止全部后台运行的服务
	@for s in $(SERVICES); do pkill -9 -f "$$s-rpc" 2>/dev/null || true; done
	@pkill -9 -f "bin/gateway" 2>/dev/null || true
	@echo "services stopped"

.PHONY: restart
restart: stop run ## 重启全部服务

.PHONY: status
status: ## 查看服务运行状态与端口占用
	@ps aux | grep -E "$(BUILD_DIR)/(user|relation|feed|comment|interaction)-rpc|$(BUILD_DIR)/gateway" | grep -v grep || echo "no service running"
	@echo "--- listening ports ---"
	@for p in 8080 9001 9002 9003 9004 9005 9006 9007 9108 9109 9110; do \
		if (exec 3<>/dev/tcp/127.0.0.1/$$p) 2>/dev/null; then echo "$$p OPEN"; exec 3>&-; else echo "$$p free"; fi; \
	done

.PHONY: svclogs
svclogs: ## 跟踪全部服务日志（Ctrl+C 退出）
	@tail -f $(LOG_DIR)/*/*.log

.PHONY: clean-logs
clean-logs: ## 清空全部服务日志（谨慎使用，先make stop 再执行更安全）
	@rm -rf $(LOG_DIR)/*
	@echo "logs cleared"

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
