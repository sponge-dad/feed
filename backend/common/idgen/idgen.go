// Package idgen 基于 Snowflake 算法的分布式唯一 ID 生成器。
//
// 64 bit = 1(符号) + 41(时间戳ms) + 10(机器ID) + 12(序列号)
//   - 全局唯一
//   - 趋势递增（对 MySQL 主键索引友好）
//   - 每毫秒每节点可生成 4096 个 ID
//
// 机器 ID 由 K8s Pod 序号或配置分配，取值范围 0~1023。
//
// 用法：
//
//	idgen.Init(1)          // 服务启动时初始化，传入机器ID
//	id := idgen.Next()     // 生成一个唯一ID
package idgen

import (
	"sync"

	"github.com/bwmarrin/snowflake"
)

var (
	node *snowflake.Node
	once sync.Once
	err  error
)

// Init 初始化 Snowflake 节点。machineID 取值 0~1023，同一集群内各实例必须不同。
func Init(machineID int64) error {
	once.Do(func() {
		node, err = snowflake.NewNode(machineID)
	})
	return err
}

// Next 生成一个唯一 ID。若未显式 Init，则默认使用机器ID=1。
func Next() int64 {
	if node == nil {
		_ = Init(1)
	}
	return node.Generate().Int64()
}

// NextString 生成字符串形式的唯一 ID。
func NextString() string {
	if node == nil {
		_ = Init(1)
	}
	return node.Generate().String()
}
