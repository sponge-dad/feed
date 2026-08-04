package interceptors

// gRPC Metadata 键定义。
// 禁止在业务代码中硬编码这些字符串，统一从此处引用。

const (
	// MDKeyRequestID 请求标识 metadata 键。
	// 由 Gateway 中间件生成，经 Client 拦截器注入 outgoing metadata，
	// Server 拦截器从 incoming metadata 提取后写入 ctx + 绑定日志字段。
	MDKeyRequestID = "x-request-id"

	// MDKeyAgentRunID Agent 运行标识 metadata 键。
	// 由 Agent RPC 调用时注入，用于关联一次 Agent 诊断运行的全链路。
	MDKeyAgentRunID = "x-agent-run-id"
)
