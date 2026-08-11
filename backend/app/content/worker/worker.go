// worker.go
//
// 职责：Content Worker 独立进程（与 Content RPC 分离，隔离 FFmpeg 资源消耗）。
// 消费 feed-created / feed-deleted，执行视频内容分析流水线并写画像与 ES 索引。
// Prometheus metrics：9109（FR-026）。
package main

import (
	"flag"
	"net/http"

	"github.com/sponge-dad/feed/app/content/worker/internal/config"
	"github.com/sponge-dad/feed/app/content/worker/internal/consumer"
	"github.com/sponge-dad/feed/app/content/worker/internal/svc"
	feedevent "github.com/sponge-dad/feed/common/event/feed"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
)

var configFile = flag.String("f", "etc/content-worker.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)
	c.Fill()
	// 日志初始化（独立子目录 logs/content-worker）。
	logx.MustSetup(c.Log)

	sctx, err := svc.NewServiceContext(c)
	if err != nil {
		panic(err)
	}

	// 注册两个消费者。
	fc := consumer.NewFeedCreatedConsumer(sctx)
	fd := consumer.NewFeedDeletedConsumer(sctx)
	if err := sctx.Consumer.Subscribe(feedevent.TopicFeedCreated, fc.Handle); err != nil {
		panic(err)
	}
	if err := sctx.Consumer.Subscribe(feedevent.TopicFeedDeleted, fd.Handle); err != nil {
		panic(err)
	}
	if err := sctx.Consumer.Start(); err != nil {
		panic(err)
	}
	defer sctx.Consumer.Shutdown()

	// Prometheus metrics（FR-026：content-worker:9109）。
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		addr := "0.0.0.0:9109"
		logx.Infof("content-worker metrics listening on %s", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			logx.Errorf("metrics server error: %v", err)
		}
	}()

	logx.Infof("content-worker started, consuming topics: %s, %s",
		feedevent.TopicFeedCreated, feedevent.TopicFeedDeleted)
	select {}
}
