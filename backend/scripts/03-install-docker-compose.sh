#!/usr/bin/env bash
#
# 03-install-docker-compose.sh
# 安装 Docker Compose（v2 插件形式），用于启动 deploy/docker-compose.yaml
# 定义的本地基础设施（MySQL/Redis/etcd/RocketMQ）。
#
# 背景：很多 CVM 上通过 yum/dnf 装的 Docker 只有引擎本身，没有自带
# compose 功能（无论是 `docker compose` 子命令还是独立的 `docker-compose`
# 二进制），跑 `make up` 时会报 "unknown command: docker compose" 或
# "unknown shorthand flag: 'f'"，就是这个原因。
#
# 适用系统：TencentOS Server / CentOS / RHEL 系
# 前置条件：已安装 Docker Engine（脚本只补 compose 功能，不装 Docker 本身）
#
# 用法：bash scripts/03-install-docker-compose.sh
#
set -euo pipefail

GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; NC='\033[0m'
info()  { echo -e "${GREEN}[INFO]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*"; }

FALLBACK_VERSION="v2.29.7"   # GitHub API 查询失败时的兜底版本

# ---------- 0. 前置检查 ----------
if ! command -v docker >/dev/null 2>&1; then
    error "未检测到 docker 命令，请先安装 Docker Engine"
    exit 1
fi

if docker compose version >/dev/null 2>&1; then
    info "docker compose 插件已可用: $(docker compose version)"
    info "无需安装，直接运行: make up"
    exit 0
fi

info "未检测到 docker compose，开始安装..."

# ---------- 1. 优先尝试系统包管理器（最省心，自带升级维护） ----------
if command -v dnf >/dev/null 2>&1; then
    info "尝试通过 dnf 安装 docker-compose-plugin..."
    if sudo dnf install -y docker-compose-plugin 2>&1 | tee /tmp/compose-install.log; then
        if docker compose version >/dev/null 2>&1; then
            info "通过 dnf 安装成功: $(docker compose version)"
            exit 0
        fi
    fi
    warn "dnf 安装失败或仓库中没有该包，转为手动下载二进制"
elif command -v yum >/dev/null 2>&1; then
    info "尝试通过 yum 安装 docker-compose-plugin..."
    if sudo yum install -y docker-compose-plugin 2>&1 | tee /tmp/compose-install.log; then
        if docker compose version >/dev/null 2>&1; then
            info "通过 yum 安装成功: $(docker compose version)"
            exit 0
        fi
    fi
    warn "yum 安装失败或仓库中没有该包，转为手动下载二进制"
fi

# ---------- 2. 手动下载官方二进制，安装为 CLI 插件 ----------
ARCH=$(uname -m)
case "${ARCH}" in
    x86_64)  COMPOSE_ARCH="x86_64" ;;
    aarch64) COMPOSE_ARCH="aarch64" ;;
    *) error "不支持的架构: ${ARCH}"; exit 1 ;;
esac

info "查询 docker/compose 最新版本号..."
COMPOSE_VERSION=$(curl -fsSL https://api.github.com/repos/docker/compose/releases/latest 2>/dev/null \
    | grep '"tag_name"' | head -1 | cut -d '"' -f4 || true)
if [ -z "${COMPOSE_VERSION}" ]; then
    warn "查询最新版本失败（可能是 GitHub API 限流），使用兜底版本 ${FALLBACK_VERSION}"
    COMPOSE_VERSION="${FALLBACK_VERSION}"
fi
info "将安装版本: ${COMPOSE_VERSION}"

# 系统级插件目录：所有用户可用，与 root 权限一致
PLUGIN_DIR="/usr/local/lib/docker/cli-plugins"
sudo mkdir -p "${PLUGIN_DIR}"

DOWNLOAD_URL="https://github.com/docker/compose/releases/download/${COMPOSE_VERSION}/docker-compose-linux-${COMPOSE_ARCH}"
info "下载 ${DOWNLOAD_URL}"
TMP_FILE=$(mktemp)
if ! curl -fsSL -o "${TMP_FILE}" "${DOWNLOAD_URL}"; then
    error "下载失败，请检查网络（GitHub 在国内访问可能不稳定），或手动下载后放到 ${PLUGIN_DIR}/docker-compose"
    exit 1
fi

sudo mv "${TMP_FILE}" "${PLUGIN_DIR}/docker-compose"
sudo chmod +x "${PLUGIN_DIR}/docker-compose"

# 同时软链到 /usr/local/bin，兼容团队里可能习惯用独立 `docker-compose` 命令的人
sudo ln -sf "${PLUGIN_DIR}/docker-compose" /usr/local/bin/docker-compose

# ---------- 3. 验证 ----------
echo ""
if docker compose version >/dev/null 2>&1; then
    info "安装成功: $(docker compose version)"
    info "现在可以运行: make up"
else
    error "安装后仍无法识别 docker compose，请检查 Docker 版本（需 20.10+）或手动排查"
    exit 1
fi
