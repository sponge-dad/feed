#!/usr/bin/env bash
#
# 02-gen-services.sh
# 用 goctl 从 proto 文件生成各微服务的 gRPC 骨架代码
#
# 前置：已运行 00-install-tools.sh 和 01-init-project.sh
# 用法：bash scripts/02-gen-services.sh
#
set -euo pipefail

GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; NC='\033[0m'
info()  { echo -e "${GREEN}[INFO]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*"; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "${SCRIPT_DIR}")"
cd "${PROJECT_ROOT}"
info "项目根目录: ${PROJECT_ROOT}"

# 检查 goctl
if ! command -v goctl >/dev/null 2>&1; then
    error "未找到 goctl，请先运行 00-install-tools.sh 并执行 source ~/.bashrc"
    exit 1
fi

SERVICES=(user relation feed comment interaction)

# ---------- 1. 生成 rpc 服务骨架 ----------
for svc in "${SERVICES[@]}"; do
    PROTO_FILE="api/proto/${svc}/${svc}.proto"
    if [ ! -f "${PROTO_FILE}" ]; then
        warn "跳过 ${svc}：proto 文件不存在 (${PROTO_FILE})"
        continue
    fi

    info "生成 ${svc} 服务的 gRPC 骨架..."
    # goctl rpc protoc: 生成 rpc 服务、逻辑层、pb代码
    #   --zrpc_out 指定输出目录
    #   -m 表示按 proto 中的 message 拆分文件（可选）
    goctl rpc protoc "${PROTO_FILE}" \
        --go_out="app/${svc}/rpc" \
        --go-grpc_out="app/${svc}/rpc" \
        --zrpc_out="app/${svc}/rpc" \
        --style=goZero \
        || warn "${svc} 生成出现问题，请检查 proto 语法"
done

# ---------- 2. 生成 gateway HTTP 服务骨架 ----------
GATEWAY_API="app/gateway/api/gateway.api"
if [ -f "${GATEWAY_API}" ]; then
    info "生成 gateway HTTP 骨架..."
    goctl api go --api "${GATEWAY_API}" --dir "app/gateway" --style=goZero \
        || warn "gateway 生成出现问题"
else
    warn "跳过 gateway：${GATEWAY_API} 不存在（后续步骤定义）"
fi

# ---------- 3. 整理依赖 ----------
info "go mod tidy..."
go mod tidy || warn "go mod tidy 出现问题，可稍后手动执行"

echo ""
info "==================== 生成完成 ===================="
info "各服务骨架已生成到 app/ 目录"
info "下一步：填充业务逻辑（logic 层）"
info "=================================================="
