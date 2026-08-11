// Package config 定义 Content Worker 进程的配置结构。
package config

import (
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/zrpc"
)

// Config Content Worker 配置（独立进程，与 Content RPC 分离，隔离 FFmpeg 资源消耗）。
type Config struct {
	// Log 日志配置（统一规范：file + json + 独立子目录）。
	Log logx.LogConf
	// Mysql 业务库连接（feed_content）。
	Mysql struct {
		DataSource string
	}
	// CacheRedis go-zero 缓存配置；第一个节点同时用作业务级 Redis（分析锁）。
	CacheRedis cache.CacheConf
	// RocketMQ 消费者配置。
	RocketMQ struct {
		NameServer   []string
		ConsumeGroup string // 分析消费组：content-analysis-consumer
	}
	// FeedRpc 查 Feed 详情（媒体地址、类型、作者）。
	FeedRpc zrpc.RpcClientConf
	// Elasticsearch 检索索引（写别名 feed_content_write）。
	Elasticsearch struct {
		Addr string
	}
	// Media FFmpeg/下载安全与资源上限（见 docs/design/agent/04-content-analysis.md §4）。
	Media struct {
		// FFmpegPath ffmpeg 二进制绝对路径（不从 PATH 查找，防替换）。
		FFmpegPath string
		// FFprobePath ffprobe 二进制绝对路径。
		FFprobePath string
		// AllowedMediaHosts 媒体下载域名白名单（SSRF 防护）。
		AllowedMediaHosts []string
		// TempDir 任务临时目录根路径，每任务独立子目录，完成后清理。
		TempDir string
		// MaxConcurrency 并发分析任务数（FFmpeg 进程数上限）。
		MaxConcurrency int
		// KeyFrameMax 关键帧数量上限。
		KeyFrameMax int
		// MaxVideoBytes 视频大小上限（默认 200MB）。
		MaxVideoBytes int64
		// MaxVideoDurationSec 视频时长上限（默认 600s，ffprobe 探测）。
		MaxVideoDurationSec int64
		// FFmpegTimeoutSec 单条 ffmpeg 命令超时（默认 120s）。
		FFmpegTimeoutSec int
		// TranscriptMaxChars 字幕全文截断上限（默认 4000）。
		TranscriptMaxChars int
		// MaxRetry 单任务级重试上限（默认 3）。
		MaxRetry int
		// ModelVersion 当前模型版本（media_hash+model_version 判重）。
		ModelVersion string
		// EmbeddingDim 向量维度（ES dense_vector dims，默认 1024）。
		EmbeddingDim int
		// EmbeddingEndpoint 可选：文本向量化服务（多模态/embedding）。
		EmbeddingEndpoint string
		// CategoryWhitelist 类目白名单（模型输出必须映射到白名单，非法映射为「其他」）。
		CategoryWhitelist []string
		// 外部接入服务端点（为空时回退 fake 实现，便于无凭证环境/CI 运行）。
		ASREndpoint    string
		ASRAPIKey      string
		OCREndpoint    string
		OCRAPIKey      string
		VisionEndpoint string
	}
	// ARK 多模态模型（标签生成 / embedding），API Key 走环境变量。
	ARK struct {
		APIKey string // 从环境变量注入
		Model  string
	}
}

// Fill 为未配置的字段填充默认值。
func (c *Config) Fill() {
	m := &c.Media
	if m.TempDir == "" {
		m.TempDir = "/var/tmp/feedmind"
	}
	if m.MaxConcurrency <= 0 {
		m.MaxConcurrency = 2
	}
	if m.KeyFrameMax <= 0 {
		m.KeyFrameMax = 20
	}
	if m.MaxVideoBytes <= 0 {
		m.MaxVideoBytes = 200 * 1024 * 1024
	}
	if m.MaxVideoDurationSec <= 0 {
		m.MaxVideoDurationSec = 600
	}
	if m.FFmpegTimeoutSec <= 0 {
		m.FFmpegTimeoutSec = 120
	}
	if m.TranscriptMaxChars <= 0 {
		m.TranscriptMaxChars = 4000
	}
	if m.MaxRetry <= 0 {
		m.MaxRetry = 3
	}
	if m.EmbeddingDim <= 0 {
		m.EmbeddingDim = 1024
	}
	if len(m.CategoryWhitelist) == 0 {
		m.CategoryWhitelist = []string{
			"户外旅行", "美食", "健身", "知识科普", "科技数码",
			"生活方式", "萌宠", "汽车", "游戏", "其他",
		}
	}
}
