#!/usr/bin/env bash
# ===================================================================
# COS 密钥环境变量（本地开发用）
# 1) 复制本文件为 cos-env.sh：cp scripts/cos-env.example.sh scripts/cos-env.sh
# 2) 把下面两行替换成你的真实 SecretId / SecretKey（来自腾讯云 CAM 子账号）
# 3) 在本终端执行：source scripts/cos-env.sh
# 4) 之后启动/重启网关，进程会继承这些环境变量，gateway.yaml 里的
#    ${COS_SECRET_ID} / ${COS_SECRET_KEY} 才能被正确替换。
#
# 安全：本文件只放占位符，可提交；但 cos-env.sh（含真密钥）禁止提交！
# ===================================================================
export COS_SECRET_ID="在此填入你的SecretId"
export COS_SECRET_KEY="在此填入你的SecretKey"
