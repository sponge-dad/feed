#!/bin/bash
#
# benchmark-user.sh
# User 服务（经 Gateway REST）压力测试脚本
#
# 前置：
#   1. make up 已启动 MySQL/Redis/etcd
#   2. user-rpc / relation-rpc / gateway 已启动
#   3. 已安装 hey：go install github.com/rakyll/hey@latest
#   4. 已安装 jq
#
# 用法：
#   bash scripts/benchmark-user.sh
#
set -euo pipefail

GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
info()  { echo -e "${GREEN}[INFO]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }

BASE_URL="http://localhost:8080/api/v1"
DURATION="60s"
CONCURRENCY="100"

# 检查依赖
export PATH=$PATH:/root/go/bin
command -v hey >/dev/null 2>&1 || { echo "未安装 hey，请执行: export PATH=\$PATH:/root/go/bin && go install github.com/rakyll/hey@latest"; exit 1; }
command -v jq >/dev/null 2>&1 || { echo "未安装 jq，请安装后重试"; exit 1; }

info "Gateway 地址: ${BASE_URL}"
info "压测时长: ${DURATION}, 并发: ${CONCURRENCY}"

# ------------------------------------------------------------------
# 1. 预热：准备测试账号
# ------------------------------------------------------------------
info "准备测试账号..."

TEST_USER_PREFIX="bench_user_"
TEST_PASSWORD="Bench123!"
TEST_NICKNAME="Benchmarker"
REGISTER_COUNT=100

mkdir -p /tmp/feed-benchmark
LOGIN_PAYLOADS="/tmp/feed-benchmark/login_payloads.txt"
REGISTER_PAYLOADS="/tmp/feed-benchmark/register_payloads.txt"
ME_TOKENS="/tmp/feed-benchmark/me_tokens.txt"
> "${LOGIN_PAYLOADS}"
> "${REGISTER_PAYLOADS}"
> "${ME_TOKENS}"

for i in $(seq 1 ${REGISTER_COUNT}); do
    username="${TEST_USER_PREFIX}${i}"
    resp=$(curl -s -X POST "${BASE_URL}/users/register" \
        -H "Content-Type: application/json" \
        -d "{\"username\":\"${username}\",\"password\":\"${TEST_PASSWORD}\",\"nickname\":\"${TEST_NICKNAME}\"}")

    token=$(echo "${resp}" | jq -r '.data.token // empty')
    code=$(echo "${resp}" | jq -r '.code // empty')

    if [ -n "${token}" ]; then
        # 注册成功
        :
    elif [ "${code}" = "10001" ]; then
        # 用户已存在，直接登录取 token
        resp=$(curl -s -X POST "${BASE_URL}/users/login" \
            -H "Content-Type: application/json" \
            -d "{\"username\":\"${username}\",\"password\":\"${TEST_PASSWORD}\"}")
        token=$(echo "${resp}" | jq -r '.data.token // empty')
        if [ -z "${token}" ]; then
            warn "用户已存在但登录 ${username} 失败: ${resp}"
            continue
        fi
    else
        warn "注册 ${username} 失败: ${resp}"
        continue
    fi

    echo "{\"username\":\"${username}\",\"password\":\"${TEST_PASSWORD}\"}" >> "${LOGIN_PAYLOADS}"
    echo "${token}" >> "${ME_TOKENS}"
done

registered=$(wc -l < "${LOGIN_PAYLOADS}")
info "成功准备 ${registered} 个测试账号"

if [ "${registered}" -eq 0 ]; then
    echo "没有可用测试账号，退出"
    exit 1
fi

first_username=$(head -1 "${LOGIN_PAYLOADS}" | jq -r '.username')
first_token=$(head -1 "${ME_TOKENS}")
first_user_id=$(curl -s "${BASE_URL}/users/me" -H "Authorization: Bearer ${first_token}" | jq -r '.data.id')
if [ -z "${first_user_id}" ] || [ "${first_user_id}" = "null" ]; then
    warn "无法获取当前用户 ID"
fi

# ------------------------------------------------------------------
# 2. 压测：注册接口
# ------------------------------------------------------------------
# hey 的 -D 会把整个文件作为单个 body，不适合需要不同用户名的注册压测。
# 这里用 shell 并发 worker 持续生成随机用户名并调用注册接口。
info "压测 POST /users/register (CPU 密集型：bcrypt)..."
> /tmp/feed-benchmark/register_ok.txt
> /tmp/feed-benchmark/register_fail.txt
register_end=$(($(date +%s) + 60))
for i in $(seq 1 ${CONCURRENCY}); do
    (
        while [ "$(date +%s)" -lt "${register_end}" ]; do
            suffix=$(date +%s%N | sha256sum | head -c 10)
            username="${TEST_USER_PREFIX}reg_${suffix}_${i}_$(date +%s%N)"
            resp=$(curl -s -X POST "${BASE_URL}/users/register" \
                -H "Content-Type: application/json" \
                -d "{\"username\":\"${username}\",\"password\":\"${TEST_PASSWORD}\",\"nickname\":\"${TEST_NICKNAME}\"}")
            code=$(echo "${resp}" | jq -r '.code // empty')
            if [ "${code}" = "0" ]; then
                echo "ok" >> /tmp/feed-benchmark/register_ok.txt
            else
                echo "fail" >> /tmp/feed-benchmark/register_fail.txt
            fi
        done
    ) &
done
wait
register_ok=$(wc -l < /tmp/feed-benchmark/register_ok.txt)
register_fail=$(wc -l < /tmp/feed-benchmark/register_fail.txt)
info "register 完成：成功 ${register_ok}，失败 ${register_fail}"

# ------------------------------------------------------------------
# 3. 压测：登录接口
# ------------------------------------------------------------------
# 使用固定用户名压测 login，重点测试 bcrypt compare 性能。
info "压测 POST /users/login (CPU 密集型：bcrypt 校验)..."
hey -z "${DURATION}" -c "${CONCURRENCY}" -m POST -T "application/json" \
    -d "{\"username\":\"${first_username}\",\"password\":\"${TEST_PASSWORD}\"}" \
    "${BASE_URL}/users/login" > /tmp/feed-benchmark/login.txt

cat /tmp/feed-benchmark/login.txt

# ------------------------------------------------------------------
# 4. 压测：获取用户信息（需 JWT）
# ------------------------------------------------------------------
info "压测 GET /users/{id} (缓存友好)..."
if [ -n "${first_user_id}" ] && [ "${first_user_id}" != "null" ]; then
    hey -z "${DURATION}" -c "${CONCURRENCY}" -q 2000 \
        -H "Authorization: Bearer ${first_token}" \
        "${BASE_URL}/users/${first_user_id}" > /tmp/feed-benchmark/get_user.txt
    cat /tmp/feed-benchmark/get_user.txt
else
    warn "跳过 GET /users/{id} 压测"
fi

# ------------------------------------------------------------------
# 5. 压测：获取当前用户（需 JWT）
# ------------------------------------------------------------------
info "压测 GET /users/me (JWT 解析 + 可选 Relation 聚合)..."
hey -z "${DURATION}" -c "${CONCURRENCY}" -q 2000 \
    -H "Authorization: Bearer ${first_token}" \
    "${BASE_URL}/users/me" > /tmp/feed-benchmark/get_me.txt

cat /tmp/feed-benchmark/get_me.txt

# ------------------------------------------------------------------
# 6. 汇总
# ------------------------------------------------------------------
info "压测结果保存在 /tmp/feed-benchmark/"
ls -l /tmp/feed-benchmark/

info "建议同时观察："
echo "  - 服务日志中的慢请求与错误数"
echo "  - MySQL: SHOW PROCESSLIST; 慢查询日志"
echo "  - Redis: redis-cli -a mUd0ZLc312DPJ4Acaf4PnIoF INFO stats; 命中率"
echo "  - CPU/内存: top / htop"
