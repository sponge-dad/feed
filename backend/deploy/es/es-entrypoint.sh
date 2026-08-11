#!/usr/bin/env bash
#
# Feed 开发环境 Elasticsearch 容器入口脚本。
#
# 职责：在官方 entrypoint 之前「一次性」安装中文分词插件 analysis-ik（幂等）。
# mapping 中 title / transcript / ocr_text 等文本字段依赖 ik_max_word / ik_smart，
# 插件缺失时创建 feed_content_v1 索引会报 400。
#
# 说明：
#   - IK 插件版本必须与 ES 版本严格一致（此处 ES 8.13.4 ↔ analysis-ik 8.13.4），
#     升级 ES 镜像版本时记得同步修改 IK_VERSION。
#   - 插件安装需要访问 GitHub releases；若开发机网络受限导致安装失败，
#     ES 仍会正常启动（不阻断），但 init-index.sh 建索引时会给出明确提示，
#     需手动下载插件放入 plugins/analysis-ik 后重启容器。
set -e

ES_VERSION="${ES_VERSION:-8.13.4}"
IK_VERSION="${IK_VERSION:-8.13.4}"
PLUGIN_DIR="/usr/share/elasticsearch/plugins/analysis-ik"
IK_ZIP="https://github.com/medcl/elasticsearch-analysis-ik/releases/download/v${IK_VERSION}/elasticsearch-analysis-ik-${IK_VERSION}.zip"

if [ ! -d "$PLUGIN_DIR" ]; then
  echo "==> [es-entrypoint] installing elasticsearch-analysis-ik v${IK_VERSION} ..."
  if gosu elasticsearch elasticsearch-plugin install -b "$IK_ZIP" >/dev/null 2>&1; then
    echo "==> [es-entrypoint] analysis-ik installed"
  else
    echo "!! [es-entrypoint] analysis-ik install FAILED (可能无法访问 GitHub)。" >&2
    echo "!! 请手动下载 ${IK_ZIP} 解压到容器 ${PLUGIN_DIR} 后重启 feed-es 容器。" >&2
    echo "!! ES 将正常启动，但 create index 需要 ik 插件，先别执行 init-index.sh。" >&2
  fi
else
  echo "==> [es-entrypoint] analysis-ik already present, skip install"
fi

exec /usr/local/bin/docker-entrypoint.sh "$@"
