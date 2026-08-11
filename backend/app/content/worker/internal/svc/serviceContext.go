// Package svc 定义 Content Worker 进程的依赖装配（与 Content RPC 分离）。
package svc

import (
	"context"
	"errors"
	"time"

	feedClient "github.com/sponge-dad/feed/app/feed/rpc/feedclient"
	"github.com/sponge-dad/feed/app/content/model"
	"github.com/sponge-dad/feed/app/content/search"
	"github.com/sponge-dad/feed/app/content/worker/internal/asr"
	"github.com/sponge-dad/feed/app/content/worker/internal/config"
	"github.com/sponge-dad/feed/app/content/worker/internal/media"
	"github.com/sponge-dad/feed/app/content/worker/internal/ocr"
	"github.com/sponge-dad/feed/app/content/worker/internal/pipeline"
	"github.com/sponge-dad/feed/app/content/worker/internal/vision"
	"github.com/sponge-dad/feed/common/interceptors"
	"github.com/sponge-dad/feed/common/mq"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"github.com/zeromicro/go-zero/zrpc"
)

// ES 写别名（见 docs/design/agent/05-content-search.md §3）。
const ESWriteAlias = "feed_content_write"

// ServiceContext Content Worker 上下文。
type ServiceContext struct {
	Config               config.Config
	Redis                *redis.Redis
	ContentProfilesModel model.FeedContentProfilesModel
	Es                   *search.Client
	FeedRpc              feedClient.Feed
	Consumer             *mq.Consumer
	Pipeline             *pipeline.Pipeline
}

// NewServiceContext 装配生产环境依赖。
func NewServiceContext(c config.Config) (*ServiceContext, error) {
	if len(c.CacheRedis) == 0 {
		return nil, errors.New("CacheRedis config is empty")
	}
	conn := sqlx.NewMysql(c.Mysql.DataSource)
	rds := redis.MustNewRedis(c.CacheRedis[0].RedisConf)
	es, err := search.NewClient(c.Elasticsearch.Addr, "feed_content", ESWriteAlias)
	if err != nil {
		return nil, err
	}
	consumer, err := mq.NewConsumer(c.RocketMQ.NameServer, c.RocketMQ.ConsumeGroup)
	if err != nil {
		return nil, err
	}
	ffmpeg := &media.FFmpeg{
		Path:      c.Media.FFmpegPath,
		Timeout:   time.Duration(c.Media.FFmpegTimeoutSec) * time.Second,
		Exec:      media.OSExecutor{Timeout: time.Duration(c.Media.FFmpegTimeoutSec) * time.Second},
		MaxFrames: c.Media.KeyFrameMax,
	}
	dl := &media.Downloader{MaxBytes: c.Media.MaxVideoBytes}

	// 外部接入：配置了端点才用真实 HTTP 实现，否则回退 fake（无凭证环境/CI 可运行）。
	asrClient := buildASR(c)
	ocrClient := buildOCR(c)
	visionClient := buildVision(c)

	pipe := pipeline.New(&c, model.NewFeedContentProfilesModel(conn, c.CacheRedis), es,
		ffmpeg, dl, asrClient, ocrClient, visionClient, logx.WithContext(context.Background()))

	return &ServiceContext{
		Config:               c,
		Redis:                rds,
		ContentProfilesModel: model.NewFeedContentProfilesModel(conn, c.CacheRedis),
		Es:                   es,
		FeedRpc: feedClient.NewFeed(zrpc.MustNewClient(c.FeedRpc,
			zrpc.WithUnaryClientInterceptor(interceptors.UnaryClientRequestID))),
		Consumer: consumer,
		Pipeline: pipe,
	}, nil
}

func buildASR(c config.Config) asr.Client {
	if c.Media.ASREndpoint != "" {
		return asr.NewHTTPClient(c.Media.ASREndpoint, c.Media.ASRAPIKey)
	}
	logx.Infow("ASR endpoint not configured, using fake client", logx.Field("warn", true))
	return &asr.FakeClient{}
}

func buildOCR(c config.Config) ocr.Client {
	if c.Media.OCREndpoint != "" {
		return ocr.NewHTTPClient(c.Media.OCREndpoint, c.Media.OCRAPIKey)
	}
	logx.Infow("OCR endpoint not configured, using fake client", logx.Field("warn", true))
	return &ocr.FakeClient{}
}

func buildVision(c config.Config) vision.Client {
	if c.Media.VisionEndpoint != "" {
		return vision.NewHTTPClient(c.Media.VisionEndpoint, c.ARK.APIKey, c.ARK.Model)
	}
	logx.Infow("vision endpoint not configured, using fake client", logx.Field("warn", true))
	return &vision.FakeClient{}
}
