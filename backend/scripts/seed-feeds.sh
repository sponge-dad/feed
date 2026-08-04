#!/bin/bash
#
# seed-feeds.sh
# 为指定用户批量注入视频 / 图文帖子（开发环境数据种子）
#
# 前置：
#   1. make up 已启动 MySQL/Redis/etcd/RocketMQ
#   2. user.rpc / relation.rpc / feed.rpc / gateway 均已启动
#   3. scripts/cos-env.sh 中已配置 COS_SECRET_ID / COS_SECRET_KEY
#
# 用法：
#   bash scripts/seed-feeds.sh                       # 默认 spongebob，200 视频 + 200 图文
#   USER_NAME=alice VIDEOS=50 IMAGES=50 bash scripts/seed-feeds.sh
#   DRY_RUN=1 bash scripts/seed-feeds.sh             # 仅预检不写数据
#
set -euo pipefail

cd "$(dirname "$0")/.."

USER_NAME="${USER_NAME:-spongebob}"
VIDEOS="${VIDEOS:-200}"
IMAGES="${IMAGES:-200}"
CONCURRENCY="${CONCURRENCY:-8}"
BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
DRY_RUN="${DRY_RUN:-0}"

GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'

# COS 密钥仅来自环境变量，未注入时尝试加载本地未入库的 cos-env.sh
if [[ -z "${COS_SECRET_ID:-}" || -z "${COS_SECRET_KEY:-}" ]]; then
  if [[ -f scripts/cos-env.sh ]]; then
    # shellcheck disable=SC1091
    source scripts/cos-env.sh
  fi
fi
if [[ -z "${COS_SECRET_ID:-}" || -z "${COS_SECRET_KEY:-}" ]]; then
  echo -e "${YELLOW}缺少 COS 凭证，请先 export COS_SECRET_ID / COS_SECRET_KEY${NC}"
  exit 1
fi

ARGS=(-user "$USER_NAME" -videos "$VIDEOS" -images "$IMAGES" -concurrency "$CONCURRENCY" -base-url "$BASE_URL")
if [[ "$DRY_RUN" == "1" ]]; then
  ARGS+=(-dry-run)
fi

echo -e "${GREEN}==> seedfeed user=${USER_NAME} videos=${VIDEOS} images=${IMAGES} concurrency=${CONCURRENCY}${NC}"
go run ./cmd/seedfeed "${ARGS[@]}"
