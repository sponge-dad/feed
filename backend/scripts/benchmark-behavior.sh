#!/bin/bash
#
# benchmark-behavior.sh
# 行为埋点上报链路压测（见 docs/design/agent/03-behavior-event.md §8）
#
# 验证两个性能目标：
#   1. 上报接口 P99 < 200ms（接口只做校验 + 投递 MQ，不落库）
#   2. 端到端延迟 P99 < 5s（上报 → 指标在 Redis 小时桶可见）
#
# 前置：
#   1. make up 已启动 MySQL/Redis/etcd/RocketMQ
#   2. user-rpc / feed-rpc / interaction-rpc / gateway 已启动
#   3. 已安装 hey：go install github.com/rakyll/hey@latest
#   4. 已安装 jq、python3；端到端阶段还需 redis-cli
#
# 用法：
#   REDIS_PASSWORD='xxx' bash scripts/benchmark-behavior.sh
#
# 环境变量：
#   BASE_URL          网关地址，默认 http://localhost:8080/api/v1
#   REDIS_ADDR        Redis 地址，默认 127.0.0.1:6379
#   REDIS_PASSWORD    Redis 密码（不从配置文件读，避免密钥进脚本）
#   DURATION          压测时长，默认 60s
#   CONCURRENCY       并发数，默认 50
#   BATCH_SIZE        单请求事件条数，默认 50（协议上限）
#   E2E_SAMPLES       端到端延迟采样次数，默认 20
#   SKIP_E2E=1        跳过端到端阶段
#
set -euo pipefail

GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; NC='\033[0m'
info()  { echo -e "${GREEN}[INFO]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*"; }

BASE_URL="${BASE_URL:-http://localhost:8080/api/v1}"
REDIS_ADDR="${REDIS_ADDR:-127.0.0.1:6379}"
DURATION="${DURATION:-60s}"
CONCURRENCY="${CONCURRENCY:-50}"
BATCH_SIZE="${BATCH_SIZE:-50}"
E2E_SAMPLES="${E2E_SAMPLES:-20}"

# 性能目标（毫秒）
TARGET_API_P99_MS=200
TARGET_E2E_P99_MS=5000

# 限流错误码，见 common/errorx/errorx.go
CODE_TOO_MANY_REQ=5

export PATH=$PATH:/root/go/bin
command -v hey     >/dev/null 2>&1 || { error "未安装 hey：export PATH=\$PATH:/root/go/bin && go install github.com/rakyll/hey@latest"; exit 1; }
command -v jq      >/dev/null 2>&1 || { error "未安装 jq"; exit 1; }
command -v python3 >/dev/null 2>&1 || { error "未安装 python3"; exit 1; }

WORK_DIR="/tmp/feed-benchmark"
mkdir -p "${WORK_DIR}"

info "网关地址: ${BASE_URL}"
info "压测时长: ${DURATION}, 并发: ${CONCURRENCY}, 单请求事件数: ${BATCH_SIZE}"

# ------------------------------------------------------------------
# 1. 准备账号与素材
# ------------------------------------------------------------------
BENCH_USER="bench_behavior_1"
BENCH_PASSWORD="Bench123!"

login_or_register() {
    local username="$1" resp token
    resp=$(curl -s -X POST "${BASE_URL}/users/register" \
        -H "Content-Type: application/json" \
        -d "{\"username\":\"${username}\",\"password\":\"${BENCH_PASSWORD}\",\"nickname\":\"BehaviorBench\"}")
    token=$(echo "${resp}" | jq -r '.data.token // empty')

    if [ -z "${token}" ]; then
        resp=$(curl -s -X POST "${BASE_URL}/users/login" \
            -H "Content-Type: application/json" \
            -d "{\"username\":\"${username}\",\"password\":\"${BENCH_PASSWORD}\"}")
        token=$(echo "${resp}" | jq -r '.data.token // empty')
    fi

    [ -n "${token}" ] || { error "获取 token 失败: ${resp}"; exit 1; }
    echo "${token}"
}

info "准备测试账号..."
TOKEN=$(login_or_register "${BENCH_USER}")

info "获取可用 feed..."
FEED_IDS=$(curl -s "${BASE_URL}/feeds/timeline?type=recommend&page_size=20" \
    -H "Authorization: Bearer ${TOKEN}" | jq -r '.data.list[]?.id // empty')

if [ -z "${FEED_IDS}" ]; then
    warn "推荐流为空，尝试新建一条 feed 作为压测素材"
    FEED_IDS=$(curl -s -X POST "${BASE_URL}/feeds" \
        -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" \
        -d '{"content":"behavior benchmark seed","media_type":"video","media_urls":["https://example.com/v.mp4"]}' \
        | jq -r '.data.id // empty')
fi

[ -n "${FEED_IDS}" ] || { error "没有可用的 feed，请先执行 scripts/seed-feeds.sh"; exit 1; }
FEED_ID=$(echo "${FEED_IDS}" | head -1)
info "使用 feed_id=${FEED_ID}"

# ------------------------------------------------------------------
# 2. 构造请求体
#
# 注意：hey 只能重复发送同一个 body，因此 request_id 固定。
# 这会让所有 EXPOSE 命中同一个去重键，故这里只压 PLAY —— 压测目标是接口
# 吞吐与延迟，而非去重逻辑；混入被去重的事件会让下游计数失真、难以解读。
# ------------------------------------------------------------------
PAYLOAD_FILE="${WORK_DIR}/behavior_payload.json"
NOW_MS=$(python3 -c 'import time; print(int(time.time()*1000))')

python3 - "$PAYLOAD_FILE" "$FEED_ID" "$BATCH_SIZE" "$NOW_MS" <<'PY'
import json, sys

path, feed_id, batch, now_ms = sys.argv[1], int(sys.argv[2]), int(sys.argv[3]), int(sys.argv[4])
events = [{
    "request_id": f"bench-{i}",
    "feed_id": feed_id,
    "action_type": "PLAY",
    "position": i,
    "watch_duration_ms": 1500,
    "media_duration_ms": 15000,
    "timestamp": now_ms,
} for i in range(batch)]

with open(path, "w") as f:
    json.dump({"events": events}, f)
PY

# ------------------------------------------------------------------
# 3. 限流探测
#
# 默认 300 条/分钟/用户，压测会瞬间打满并全部返回 5（请求过于频繁），
# 得到的延迟数据毫无意义。这里先探测再决定是否继续。
# ------------------------------------------------------------------
info "探测限流配置..."
PROBE=$(curl -s -X POST "${BASE_URL}/feeds/behaviors" \
    -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" \
    --data-binary "@${PAYLOAD_FILE}")
PROBE_CODE=$(echo "${PROBE}" | jq -r '.code // 0')

if [ "${PROBE_CODE}" = "${CODE_TOO_MANY_REQ}" ]; then
    error "上报已被限流（code=${CODE_TOO_MANY_REQ}）。"
    error "压测前请调高 app/gateway/etc/gateway.yaml 的 Behavior.RateLimitPerUserPerMin"
    error "（例如 10000000）并重启 gateway，压测后务必改回 300。"
    exit 1
fi

if [ "${PROBE_CODE}" != "0" ]; then
    error "上报探测失败: ${PROBE}"
    exit 1
fi
info "探测通过: $(echo "${PROBE}" | jq -c '.data')"

# ------------------------------------------------------------------
# 4. 阶段一：上报接口吞吐与延迟
# ------------------------------------------------------------------
info "阶段一：POST /feeds/behaviors 压测中（${DURATION}）..."
API_REPORT="${WORK_DIR}/behavior_api.txt"

hey -z "${DURATION}" -c "${CONCURRENCY}" -m POST \
    -H "Authorization: Bearer ${TOKEN}" \
    -H "Content-Type: application/json" \
    -D "${PAYLOAD_FILE}" \
    "${BASE_URL}/feeds/behaviors" | tee "${API_REPORT}"

# hey 的延迟单位是秒，转成毫秒后与目标对比
API_P99_MS=$(awk '/99% in/ {printf "%.1f", $3 * 1000}' "${API_REPORT}")
if [ -n "${API_P99_MS}" ]; then
    if python3 -c "import sys; sys.exit(0 if ${API_P99_MS} < ${TARGET_API_P99_MS} else 1)"; then
        info "接口 P99 = ${API_P99_MS}ms，达标（目标 < ${TARGET_API_P99_MS}ms）"
    else
        warn "接口 P99 = ${API_P99_MS}ms，未达标（目标 < ${TARGET_API_P99_MS}ms）"
    fi
fi

if [ "${SKIP_E2E:-0}" = "1" ]; then
    info "已跳过端到端阶段（SKIP_E2E=1）"
    exit 0
fi

# ------------------------------------------------------------------
# 5. 阶段二：端到端延迟（上报 → Redis 小时桶可见）
#
# 只测到 Redis 可见为止：落库由 flush 定时任务批量执行（默认 60s 一轮），
# 属于异步汇总而非实时链路，不计入端到端延迟目标。
# ------------------------------------------------------------------
if ! command -v redis-cli >/dev/null 2>&1; then
    warn "未安装 redis-cli，跳过端到端延迟测量"
    exit 0
fi
if [ -z "${REDIS_PASSWORD:-}" ]; then
    warn "未设置 REDIS_PASSWORD 环境变量，跳过端到端延迟测量"
    exit 0
fi

redis_host="${REDIS_ADDR%%:*}"
redis_port="${REDIS_ADDR##*:}"
# 密码经环境变量传给 redis-cli，避免出现在进程命令行里被 ps 看到
redis_get_play() {
    REDISCLI_AUTH="${REDIS_PASSWORD}" redis-cli -h "${redis_host}" -p "${redis_port}" \
        --no-auth-warning HGET "$1" play 2>/dev/null
}

info "阶段二：端到端延迟采样 ${E2E_SAMPLES} 次..."
E2E_FILE="${WORK_DIR}/behavior_e2e.txt"
> "${E2E_FILE}"

for i in $(seq 1 "${E2E_SAMPLES}"); do
    now_ms=$(python3 -c 'import time; print(int(time.time()*1000))')
    stat_hour=$(date '+%Y%m%d%H')
    metrics_key="feed:metrics:h:${FEED_ID}:${stat_hour}"

    before=$(redis_get_play "${metrics_key}")
    before=${before:-0}

    body=$(python3 -c "
import json,sys
print(json.dumps({'events':[{
    'request_id': 'bench-e2e-$i',
    'feed_id': ${FEED_ID},
    'action_type': 'PLAY',
    'position': 1,
    'watch_duration_ms': 1500,
    'media_duration_ms': 15000,
    'timestamp': ${now_ms},
}]}))")

    start_ms=$(python3 -c 'import time; print(int(time.time()*1000))')
    resp=$(curl -s -X POST "${BASE_URL}/feeds/behaviors" \
        -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" \
        -d "${body}")
    if [ "$(echo "${resp}" | jq -r '.code // 0')" != "0" ]; then
        warn "第 ${i} 次上报失败: ${resp}"
        continue
    fi

    # 轮询直到计数增长，上限 10s
    elapsed=""
    for _ in $(seq 1 200); do
        current=$(redis_get_play "${metrics_key}")
        current=${current:-0}
        if [ "${current}" -gt "${before}" ]; then
            elapsed=$(python3 -c "import time; print(int(time.time()*1000) - ${start_ms})")
            break
        fi
        sleep 0.05
    done

    if [ -z "${elapsed}" ]; then
        warn "第 ${i} 次采样超时（>10s），指标未可见"
        continue
    fi
    echo "${elapsed}" >> "${E2E_FILE}"
done

python3 - "${E2E_FILE}" "${TARGET_E2E_P99_MS}" <<'PY'
import sys

path, target = sys.argv[1], int(sys.argv[2])
with open(path) as f:
    samples = sorted(int(line) for line in f if line.strip())

if not samples:
    print("[WARN] 端到端采样为空，无法评估")
    sys.exit(0)

def pct(p):
    rank = max(1, -(-len(samples) * p // 100))
    return samples[rank - 1]

n = len(samples)
print(f"\n端到端延迟（上报 → Redis 小时桶可见），样本 {n} 条：")
print(f"  P50 = {pct(50)}ms")
print(f"  P95 = {pct(95)}ms")
print(f"  P99 = {pct(99)}ms")
print(f"  Max = {samples[-1]}ms")

p99 = pct(99)
status = "达标" if p99 < target else "未达标"
print(f"  → P99 {status}（目标 < {target}ms）")
PY

info "压测完成，报告位于 ${WORK_DIR}/"
