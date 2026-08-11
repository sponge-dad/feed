#!/usr/bin/env bash
#
# 初始化 Elasticsearch 的 feed_content_v1 索引与读写别名（幂等，可重复执行）。
#
# 用法：
#   bash deploy/es/init-index.sh                 # 用默认 ES_ADDR=http://127.0.0.1:9200
#   ES_ADDR=http://<host>:9200 bash deploy/es/init-index.sh
#
# 约定（见 docs/design/agent/05-content-search.md §3）：
#   索引      feed_content_v1
#   读别名    feed_content       （Content RPC SearchContent 使用）
#   写别名    feed_content_write （Content Worker IndexProfile 使用，模型升级重建时切别名）
set -euo pipefail

ES_ADDR="${ES_ADDR:-http://127.0.0.1:9200}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MAPPING_FILE="$SCRIPT_DIR/feed_content_mapping.json"
INDEX_NAME="feed_content_v1"
READ_ALIAS="feed_content"
WRITE_ALIAS="feed_content_write"

echo "==> ES_ADDR = ${ES_ADDR}"

# 1. ES 是否可用
if ! curl -sf "${ES_ADDR}/_cluster/health" >/dev/null 2>&1; then
  echo "!! Elasticsearch 不可达（${ES_ADDR}）。请先启动基础设施：make up" >&2
  exit 1
fi

# 2. 检查中文分词插件 analysis-ik
if curl -sf "${ES_ADDR}/_cat/plugins?h=component" 2>/dev/null | grep -q "analysis-ik"; then
  echo "==> analysis-ik 插件已安装"
else
  echo "!! analysis-ik 插件未安装：title/transcript 等 ik 分词字段将无法建索引。" >&2
  echo "!! 请参考 deploy/es/es-entrypoint.sh 的提示安装插件后重试。" >&2
  exit 1
fi

# 3. 幂等创建索引（已存在则跳过）
if curl -sf -o /dev/null "${ES_ADDR}/${INDEX_NAME}"; then
  echo "==> 索引 ${INDEX_NAME} 已存在，跳过创建"
else
  echo "==> 创建索引 ${INDEX_NAME} ..."
  curl -sf -X PUT "${ES_ADDR}/${INDEX_NAME}" \
    -H 'Content-Type: application/json' \
    --data-binary "@${MAPPING_FILE}" >/dev/null
  echo "==> 索引 ${INDEX_NAME} 创建完成"
fi

# 4. 绑定读写别名（幂等：别名已存在则跳过）
add_alias() {
  local alias_name="$1"
  local alias_path
  alias_path="$(curl -sf "${ES_ADDR}/_alias/${alias_name}" 2>/dev/null || true)"
  if [ -n "$alias_path" ] && echo "$alias_path" | grep -q "${INDEX_NAME}"; then
    echo "==> 别名 ${alias_name} 已绑定 ${INDEX_NAME}，跳过"
    return 0
  fi
  if [ -n "$alias_path" ] && [ "$alias_path" != "{}" ]; then
    echo "!! 别名 ${alias_name} 已被其他索引占用，拒绝覆盖（请先处理旧索引）" >&2
    return 1
  fi
  echo "==> 绑定别名 ${alias_name} -> ${INDEX_NAME}"
  curl -sf -X POST "${ES_ADDR}/_aliases" \
    -H 'Content-Type: application/json' \
    -d "{\"actions\":[{\"add\":{\"index\":\"${INDEX_NAME}\",\"alias\":\"${alias_name}\"}}]}" >/dev/null
}

add_alias "${READ_ALIAS}"
add_alias "${WRITE_ALIAS}"

echo ""
echo "==> 完成。索引与别名状态："
curl -sf "${ES_ADDR}/_cat/indices/${INDEX_NAME}?v&h=index,status,health,docs.count" || true
curl -sf "${ES_ADDR}/_cat/aliases?v&h=alias,index" || true
