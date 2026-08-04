#!/usr/bin/env bash
#
# 01-init-project.sh
# 初始化 Feed monorepo 项目：go.mod + 目录结构
#
# 用法：bash scripts/01-init-project.sh
#
set -euo pipefail

# ---------- 配置 ----------
MODULE="github.com/sponge-dad/feed"   # go module 名，与 GitHub 仓库一致
GO_VERSION="1.23"                      # go.mod 声明的最低版本

GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
info() { echo -e "${GREEN}[INFO]${NC} $*"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }

# 切换到项目根目录（脚本所在目录的上一级）
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "${SCRIPT_DIR}")"
cd "${PROJECT_ROOT}"
info "项目根目录: ${PROJECT_ROOT}"

# ---------- 1. 初始化 go.mod ----------
if [ -f "go.mod" ]; then
    warn "go.mod 已存在，跳过 go mod init"
else
    info "初始化 go.mod (module=${MODULE})"
    go mod init "${MODULE}"
fi

# ---------- 2. 创建目录结构 ----------
info "创建目录结构..."

# 六个微服务，每个都有 rpc（gRPC），gateway 额外有 api（HTTP）
SERVICES=(user relation feed comment interaction)

for svc in "${SERVICES[@]}"; do
    mkdir -p "app/${svc}/cmd/rpc"      # gRPC 服务入口
    mkdir -p "app/${svc}/model"        # 数据库 model
    mkdir -p "app/${svc}/rpc/pb"       # proto 生成的 pb 代码
done

# 网关：只有 HTTP API，聚合调用各 rpc
mkdir -p "app/gateway/cmd/api"

# 公共代码
mkdir -p common/response      # 统一响应结构
mkdir -p common/errorx        # 统一错误码
mkdir -p common/jwtx          # JWT 工具
mkdir -p common/idgen         # Snowflake ID
mkdir -p common/ipx           # IP 定位
mkdir -p common/mq            # RocketMQ 封装

# 部署相关
mkdir -p deploy/sql           # 建表 SQL
mkdir -p deploy/k8s           # K8s 编排
mkdir -p deploy/config        # 各环境配置

# proto 定义（内部 gRPC 契约）
mkdir -p api/proto/user
mkdir -p api/proto/relation
mkdir -p api/proto/feed
mkdir -p api/proto/comment
mkdir -p api/proto/interaction

info "目录结构创建完成"

# ---------- 3. 打印目录树 ----------
echo ""
info "==================== 目录结构 ===================="
if command -v tree >/dev/null 2>&1; then
    tree -d -L 3 app common deploy api 2>/dev/null || true
else
    find app common deploy api -type d 2>/dev/null | sort | sed 's|[^/]*/|  |g'
fi
info "=================================================="
echo ""
info "项目骨架初始化完成，下一步运行: bash scripts/02-gen-services.sh"
