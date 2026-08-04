#!/bin/bash
#
# benchmark-relation.sh
# Relation RPC 服务压力测试脚本
#
# 前置：
#   1. make up 已启动 MySQL/Redis/etcd
#   2. 已安装 ghz：export PATH=\$PATH:/root/go/bin && go install github.com/bojand/ghz/cmd/ghz@latest
#   3. Python3 用于快速生成压测数据与结果汇总
#
# 用法：
#   bash scripts/benchmark-relation.sh
#
set -euo pipefail

export PATH=$PATH:/root/go/bin
command -v ghz >/dev/null 2>&1 || { echo "未安装 ghz，请执行: export PATH=\$PATH:/root/go/bin && go install github.com/bojand/ghz/cmd/ghz@latest"; exit 1; }
command -v python3 >/dev/null 2>&1 || { echo "需要 python3 用于生成压测数据"; exit 1; }

GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
info()  { echo -e "${GREEN}[INFO]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PROTO_DIR="${PROJECT_ROOT}/api/proto"
PROTO_FILE="${PROTO_DIR}/relation/relation.proto"
HOST="127.0.0.1:9003"
DURATION="15s"
OUT_DIR="/tmp/feed-benchmark/relation"
mkdir -p "${OUT_DIR}"

info "Relation RPC 地址: ${HOST}"
info "压测时长: ${DURATION}"

# ------------------------------------------------------------------
# 1. 检查服务可达
# ------------------------------------------------------------------
info "检查 Relation RPC 服务..."
if ! ghz --insecure --proto="${PROTO_FILE}" --import-paths="${PROTO_DIR}" --call=relation.Relation/GetFans \
    -d '{"user_id":80000001,"page":1,"page_size":20}' -n 1 "${HOST}" >/dev/null 2>&1; then
    echo "无法连接到 ${HOST}，请先启动 relation rpc 测试服务："
    echo "  go run app/relation/rpc/relation.go -f app/relation/rpc/etc/relation-test.yaml"
    exit 1
fi
info "服务可达"

# ------------------------------------------------------------------
# 2. 数据准备：大 V 用户与粉丝
# ------------------------------------------------------------------
VIP_USER_ID=80000001
FANS_START=90000001
FANS_COUNT=10000

info "准备大 V 用户 ${VIP_USER_ID} 的粉丝数据（${FANS_COUNT} 条）..."
mysql -h127.0.0.1 -uroot -proot -e "DELETE FROM feed_relation_test.relations WHERE followee_id = ${VIP_USER_ID};" 2>/dev/null || true

# 使用 Python 生成 SQL 文件，再批量导入，避免 shell 循环和依赖 mysql 驱动
FANS_SQL_FILE="${OUT_DIR}/fans_data.sql"
python3 -c "
vip=${VIP_USER_ID}
id_base = 7000000000000000000
batch_size = 1000
with open('${FANS_SQL_FILE}', 'w') as f:
    for batch in range(10):
        rows = []
        for i in range(1, batch_size + 1):
            follower = 90000000 + batch * batch_size + i
            idx = id_base + batch * batch_size + i
            rows.append('(%d, %d, %d, 1752998400)' % (idx, follower, vip))
        f.write('INSERT INTO relations (id, follower_id, followee_id, created_at) VALUES ' + ','.join(rows) + ';\n')
"
mysql -h127.0.0.1 -uroot -proot feed_relation_test < "${FANS_SQL_FILE}" 2>/dev/null

fans_in_db=$(mysql -h127.0.0.1 -uroot -proot -N -e "SELECT COUNT(*) FROM feed_relation_test.relations WHERE followee_id = ${VIP_USER_ID};" 2>/dev/null | tr -d '[:space:]')
info "大 V 粉丝数（DB）: ${fans_in_db}"

# 清理并预热 Redis 缓存
docker exec feed-redis redis-cli -a mUd0ZLc312DPJ4Acaf4PnIoF --no-auth-warning DEL user:follow:${VIP_USER_ID} user:fans:${VIP_USER_ID} user:fans_count:${VIP_USER_ID} >/dev/null 2>&1 || true
docker exec feed-redis redis-cli -a mUd0ZLc312DPJ4Acaf4PnIoF --no-auth-warning EVAL "local keys = redis.call('keys', ARGV[1]); for i=1,#keys do redis.call('del', keys[i]); end; return #keys" 0 'cache:relations:*' >/dev/null 2>&1 || true
info "预热 GetFans 缓存..."
ghz --insecure --proto="${PROTO_FILE}" --import-paths="${PROTO_DIR}" --call=relation.Relation/GetFans \
    -d "{\"user_id\":${VIP_USER_ID},\"page\":1,\"page_size\":20}" -n 1 "${HOST}" >/dev/null 2>&1

# ------------------------------------------------------------------
# 3. 生成 Follow 压测数据文件（使用 Python，避免 shell 循环）
# ------------------------------------------------------------------
FOLLOW_DATA_FILE="${OUT_DIR}/follow_data.json"
FOLLOW_START=91000001
FOLLOW_COUNT=50000
info "生成 Follow 压测请求数据（${FOLLOW_COUNT} 条）..."
python3 -c "
start=${FOLLOW_START}
count=${FOLLOW_COUNT}
vip=${VIP_USER_ID}
with open('${FOLLOW_DATA_FILE}', 'w') as f:
    for i in range(count):
        f.write('{\"follower_id\":%d,\"followee_id\":%d}\n' % (start + i, vip))
"

# ------------------------------------------------------------------
# 4. 压测：Follow 写接口
# ------------------------------------------------------------------
info "压测 Follow（写接口，100 并发，${DURATION}）..."
ghz --insecure --proto="${PROTO_FILE}" --import-paths="${PROTO_DIR}" --call=relation.Relation/Follow \
    --data-file="${FOLLOW_DATA_FILE}" -z "${DURATION}" -c 100 \
    --name="relation_follow" --tags='{"env":"local","type":"write"}' \
    -O json -o "${OUT_DIR}/follow.json" "${HOST}" >/dev/null 2>&1

# ------------------------------------------------------------------
# 5. 压测：GetFans 读接口（缓存命中）
# ------------------------------------------------------------------
info "压测 GetFans（读接口，缓存命中，100 并发，${DURATION}）..."
ghz --insecure --proto="${PROTO_FILE}" --import-paths="${PROTO_DIR}" --call=relation.Relation/GetFans \
    -d "{\"user_id\":${VIP_USER_ID},\"page\":1,\"page_size\":20}" -z "${DURATION}" -c 100 \
    --name="relation_getfans_cached" --tags='{"env":"local","type":"read","cache":"hit"}' \
    -O json -o "${OUT_DIR}/getfans_cached.json" "${HOST}" >/dev/null 2>&1

# ------------------------------------------------------------------
# 6. 压测：GetFans 读接口（缓存未命中/DB 回源）
# ------------------------------------------------------------------
info "清理缓存，压测 GetFans DB 回源..."
docker exec feed-redis redis-cli -a mUd0ZLc312DPJ4Acaf4PnIoF --no-auth-warning DEL user:fans:${VIP_USER_ID} user:fans_count:${VIP_USER_ID} >/dev/null 2>&1 || true
ghz --insecure --proto="${PROTO_FILE}" --import-paths="${PROTO_DIR}" --call=relation.Relation/GetFans \
    -d "{\"user_id\":${VIP_USER_ID},\"page\":1,\"page_size\":20}" -z "${DURATION}" -c 50 \
    --name="relation_getfans_db" --tags='{"env":"local","type":"read","cache":"miss"}' \
    -O json -o "${OUT_DIR}/getfans_db.json" "${HOST}" >/dev/null 2>&1

# ------------------------------------------------------------------
# 7. 压测：IsFollow 批量读接口
# ------------------------------------------------------------------
info "压测 IsFollow（批量读接口，100 并发，${DURATION}）..."
ghz --insecure --proto="${PROTO_FILE}" --import-paths="${PROTO_DIR}" --call=relation.Relation/IsFollow \
    -d "{\"follower_id\":${FANS_START},\"followee_ids\":[${VIP_USER_ID},80000002,80000003,80000004,80000005]}" \
    -z "${DURATION}" -c 100 \
    --name="relation_isfollow" --tags='{"env":"local","type":"read"}' \
    -O json -o "${OUT_DIR}/isfollow.json" "${HOST}" >/dev/null 2>&1

# ------------------------------------------------------------------
# 8. 汇总输出
# ------------------------------------------------------------------
info "压测结果文件："
ls -l "${OUT_DIR}"

info "汇总指标："
printf "%-25s %-10s %-12s %-12s %-12s %-12s\n" "接口" "并发" "RPS" "P50(ms)" "P99(ms)" "错误率"
python3 <<PY
import json, os, glob
out_dir = "${OUT_DIR}"
files = {"follow": "follow.json", "getfans_cached": "getfans_cached.json", "getfans_db": "getfans_db.json", "isfollow": "isfollow.json"}
for name, fname in files.items():
    path = os.path.join(out_dir, fname)
    if not os.path.exists(path):
        print(f"{name:25} {'-':>10} {'-':>12} {'-':>12} {'-':>12} {'-':>12}")
        continue
    with open(path) as f:
        d = json.load(f)
    total = d.get("count", 0)
    ok = d.get("statusCodeDistribution", {}).get("OK", 0)
    err_rate = round((total - ok) * 100 / total, 2) if total else 0
    rps = round(d.get("rps", 0), 2)
    lat = d.get("latencyDistribution", [])
    p50 = round(lat[2]["latency"]/1e6, 2) if len(lat) > 2 else 0
    p99 = round(lat[6]["latency"]/1e6, 2) if len(lat) > 6 else 0
    c = d.get("options", {}).get("concurrency", [100])
    cc = c[0] if isinstance(c, list) else c
    print(f"{name:25} {cc:>10} {rps:>12} {p50:>12} {p99:>12} {err_rate:>11}%")
PY

info "建议同时观察："
echo "  - 服务日志: tail -f /tmp/relation-test.log"
echo "  - MySQL: SHOW PROCESSLIST; SELECT COUNT(*) FROM feed_relation_test.relations;"
echo "  - Redis: docker exec feed-redis redis-cli -a mUd0ZLc312DPJ4Acaf4PnIoF INFO stats"
echo "  - CPU/内存: top / htop"
