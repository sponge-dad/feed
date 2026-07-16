#!/usr/bin/env bash
#
# 00-install-tools.sh
# 安装 Feed 项目开发所需工具链：protoc / goctl / protoc-gen-go / protoc-gen-go-grpc
#
# 适用系统：TencentOS Server / CentOS / RHEL 系
# 前置条件：已安装 Go 1.21+（脚本不负责装 Go）
#
# 用法：bash scripts/00-install-tools.sh
#
set -euo pipefail

# ---------- 配置 ----------
PROTOC_VERSION="25.3"          # protoc 版本（官方预编译二进制）
GOCTL_VERSION="latest"         # goctl 版本
GO_PROXY="https://goproxy.cn,direct"   # 国内代理，加速依赖下载

# ---------- 颜色输出 ----------
GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; NC='\033[0m'
info()  { echo -e "${GREEN}[INFO]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*"; }

# ---------- 0. 检查 Go ----------
info "检查 Go 环境..."
if ! command -v go >/dev/null 2>&1; then
    error "未检测到 Go，请先安装 Go 1.21+ 后再运行本脚本"
    exit 1
fi
GO_VER=$(go version | awk '{print $3}')
info "Go 版本: ${GO_VER}"

# 配置 GOPROXY（加速）与 GOBIN
go env -w GOPROXY="${GO_PROXY}"
GOBIN="$(go env GOPATH)/bin"
info "GOBIN: ${GOBIN}"

# 确保 GOBIN 在 PATH 中（写入 ~/.bashrc，幂等）
if ! echo "$PATH" | grep -q "${GOBIN}"; then
    warn "GOBIN 不在 PATH 中，追加到 ~/.bashrc"
    echo "export PATH=\$PATH:${GOBIN}" >> ~/.bashrc
    export PATH="$PATH:${GOBIN}"
fi

# ---------- 1. 安装 protoc（官方预编译二进制） ----------
info "安装 protoc v${PROTOC_VERSION}..."
if command -v protoc >/dev/null 2>&1 && protoc --version | grep -q "${PROTOC_VERSION}"; then
    info "protoc v${PROTOC_VERSION} 已安装，跳过"
else
    ARCH=$(uname -m)
    case "${ARCH}" in
        x86_64)  PROTOC_ARCH="x86_64" ;;
        aarch64) PROTOC_ARCH="aarch_64" ;;
        *) error "不支持的架构: ${ARCH}"; exit 1 ;;
    esac

    TMP_DIR=$(mktemp -d)
    PROTOC_ZIP="protoc-${PROTOC_VERSION}-linux-${PROTOC_ARCH}.zip"
    PROTOC_URL="https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/${PROTOC_ZIP}"

    info "下载 ${PROTOC_URL}"
    # 需要 unzip，若无则安装
    if ! command -v unzip >/dev/null 2>&1; then
        info "安装 unzip..."
        sudo dnf install -y unzip || sudo yum install -y unzip
    fi

    curl -fsSL -o "${TMP_DIR}/${PROTOC_ZIP}" "${PROTOC_URL}"
    unzip -o -q "${TMP_DIR}/${PROTOC_ZIP}" -d "${TMP_DIR}/protoc"
    sudo cp "${TMP_DIR}/protoc/bin/protoc" /usr/local/bin/protoc
    sudo cp -r "${TMP_DIR}/protoc/include/"* /usr/local/include/
    sudo chmod +x /usr/local/bin/protoc
    rm -rf "${TMP_DIR}"
    info "protoc 安装完成: $(protoc --version)"
fi

# ---------- 2. 安装 Go 插件 ----------
info "安装 protoc-gen-go..."
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest

info "安装 protoc-gen-go-grpc..."
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# ---------- 3. 安装 goctl ----------
info "安装 goctl（go-zero 代码生成器）..."
go install github.com/zeromicro/go-zero/tools/goctl@${GOCTL_VERSION}

# ---------- 4. 验证 ----------
echo ""
info "==================== 安装结果 ===================="
printf "  %-22s %s\n" "protoc:"            "$(protoc --version 2>/dev/null || echo '未安装')"
printf "  %-22s %s\n" "goctl:"             "$(goctl --version 2>/dev/null || echo '未安装, 请 source ~/.bashrc')"
printf "  %-22s %s\n" "protoc-gen-go:"     "$([ -f "${GOBIN}/protoc-gen-go" ] && echo '已安装' || echo '未安装')"
printf "  %-22s %s\n" "protoc-gen-go-grpc:" "$([ -f "${GOBIN}/protoc-gen-go-grpc" ] && echo '已安装' || echo '未安装')"
info "=================================================="
echo ""
warn "如果 goctl 命令找不到，请执行: source ~/.bashrc"
info "工具链安装完成，下一步运行: bash scripts/01-init-project.sh"
