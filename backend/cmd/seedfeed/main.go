// Command seedfeed 是开发环境的数据种子工具：为指定用户批量注入视频 / 图文帖子。
//
// 注入链路与真实客户端完全一致，不直接改库：
//  1. 读取 user.yaml 的 MySQL 连接，按用户名/昵称参数化查询定位目标用户；
//  2. 读取 gateway.yaml 的 JWT 密钥，为该用户签发与登录等价的 access token；
//  3. 生成媒体并上传 COS（图片/封面本地生成后 PUT，视频用 COS 服务端复制已有样片）；
//  4. 调用网关 POST /api/v1/feeds 发布，完整走鉴权、COS 归属校验、RPC、MQ 链路。
//
// 用法：
//
//	source scripts/cos-env.sh
//	go run ./cmd/seedfeed -user spongebob -videos 200 -images 200
//
// 敏感信息（COS SecretId/SecretKey）仅来自环境变量，工具不落任何明文。
package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bwmarrin/snowflake"

	"github.com/sponge-dad/feed/common/jwtx"
)

var (
	flagGatewayConf = flag.String("gateway-conf", "app/gateway/etc/gateway.yaml", "网关配置文件路径（读取 JWT 密钥与 COS 配置）")
	flagUserConf    = flag.String("user-conf", "app/user/rpc/etc/user.yaml", "User 服务配置文件路径（读取 MySQL 连接）")
	flagBaseURL     = flag.String("base-url", "http://127.0.0.1:8080", "网关基础地址")
	flagUser        = flag.String("user", "spongebob", "目标用户的 username 或 nickname")
	flagVideos      = flag.Int("videos", 200, "注入的视频帖数量")
	flagImages      = flag.Int("images", 200, "注入的图文帖数量")
	flagMaxImages   = flag.Int("max-images", 6, "单条图文帖的最大图片数（实际取 1~N 随机）")
	flagConcurrency = flag.Int("concurrency", 8, "并发工作协程数")
	flagVideoSrc    = flag.String("video-src", "", "视频源对象 key；留空则自动探测该用户已有视频")
	flagSeed        = flag.Int64("seed", 20260731, "随机种子，保证内容可复现")
	flagNodeID      = flag.Int64("node-id", 512, "生成对象 key 用的雪花节点 ID，需与线上服务错开")
	flagDryRun      = flag.Bool("dry-run", false, "只做预检（用户、COS、网关连通性），不写入任何数据")
	flagTimeout     = flag.Duration("timeout", 2*time.Hour, "整体超时时间")
)

func main() {
	flag.Parse()
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "seedfeed: %v\n", err)
		os.Exit(1)
	}
}

// task 描述一条待注入的帖子。
type task struct {
	index    int
	feedType int32 // 1=图文 2=视频
}

// seeder 聚合注入过程所需的全部依赖。
type seeder struct {
	cos      *cosClient
	pub      *publisher
	node     *snowflake.Node
	env      string
	uid      int64
	videoSrc string
	baseSeed int64
	maxImgs  int
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), *flagTimeout)
	defer cancel()

	if *flagVideos < 0 || *flagImages < 0 {
		return fmt.Errorf("videos/images 不能为负数")
	}
	if *flagMaxImages < 1 {
		return fmt.Errorf("max-images 至少为 1")
	}

	gw, err := loadGatewayConf(*flagGatewayConf)
	if err != nil {
		return err
	}
	dsn, err := loadUserDSN(*flagUserConf)
	if err != nil {
		return err
	}

	u, err := resolveUser(ctx, dsn, *flagUser)
	if err != nil {
		return err
	}
	fmt.Printf("目标用户: id=%d username=%s nickname=%s\n", u.ID, u.Username, u.Nickname)

	expireHour := int(gw.Auth.AccessExpire / 3600)
	if expireHour <= 0 {
		expireHour = 1
	}
	token, err := jwtx.NewManager(gw.Auth.AccessSecret, expireHour).Generate(u.ID, u.Username)
	if err != nil {
		return fmt.Errorf("签发 token 失败: %w", err)
	}

	cc, err := newCosClient(gw.Cos)
	if err != nil {
		return err
	}

	videoSrc := *flagVideoSrc
	if *flagVideos > 0 {
		if videoSrc == "" {
			videoSrc, err = cc.detectVideoSource(ctx, gw.Cos.Env, u.ID)
			if err != nil {
				return err
			}
		}
		ok, err := cc.exists(ctx, videoSrc)
		if err != nil {
			return fmt.Errorf("检查视频源对象失败: %w", err)
		}
		if !ok {
			return fmt.Errorf("视频源对象不存在: %s", videoSrc)
		}
		fmt.Printf("视频源对象: %s\n", videoSrc)
	}

	node, err := snowflake.NewNode(*flagNodeID)
	if err != nil {
		return fmt.Errorf("初始化雪花节点失败: %w", err)
	}

	pub := newPublisher(*flagBaseURL, token)
	if err := pub.ping(ctx); err != nil {
		return err
	}

	s := &seeder{
		cos:      cc,
		pub:      pub,
		node:     node,
		env:      gw.Cos.Env,
		uid:      u.ID,
		videoSrc: videoSrc,
		baseSeed: *flagSeed,
		maxImgs:  *flagMaxImages,
	}

	if *flagDryRun {
		fmt.Printf("[dry-run] 预检通过：将为 %s 注入 %d 条视频帖 + %d 条图文帖（图片 1~%d 张/条）\n",
			u.Nickname, *flagVideos, *flagImages, s.maxImgs)
		t, d := videoContent(1, rand.New(rand.NewSource(s.baseSeed+1)))
		fmt.Printf("[dry-run] 视频文案示例: %s\n%s\n", t, d)
		t, d = imageContent(1, rand.New(rand.NewSource(s.baseSeed+2)))
		fmt.Printf("[dry-run] 图文文案示例: %s\n%s\n", t, d)
		return nil
	}

	tasks := buildTasks(*flagVideos, *flagImages)
	return s.execute(ctx, tasks, *flagConcurrency)
}

// buildTasks 交错排列视频与图文任务，使注入后的时间线两类内容混合分布。
func buildTasks(videos, images int) []task {
	tasks := make([]task, 0, videos+images)
	vi, ii := 0, 0
	for vi < videos || ii < images {
		// 按剩余比例决定下一条取哪类，避免尾部出现长段同类内容。
		takeVideo := ii >= images || (vi < videos && (videos-vi) >= (images-ii))
		if takeVideo {
			vi++
			tasks = append(tasks, task{index: vi, feedType: 2})
		} else {
			ii++
			tasks = append(tasks, task{index: ii, feedType: 1})
		}
	}
	return tasks
}

// execute 以固定并发度执行注入任务，并输出进度与失败明细。
func (s *seeder) execute(ctx context.Context, tasks []task, concurrency int) error {
	if concurrency < 1 {
		concurrency = 1
	}
	total := len(tasks)
	fmt.Printf("开始注入：共 %d 条，并发 %d\n", total, concurrency)

	var (
		done    int64
		okCnt   int64
		failCnt int64
		mu      sync.Mutex
		errs    []string
	)
	start := time.Now()

	ch := make(chan task)
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					fmt.Fprintf(os.Stderr, "worker panic recovered: %v\n", r)
				}
			}()
			for t := range ch {
				err := withRetry(ctx, 3, func() error { return s.doTask(ctx, t) })
				if err != nil {
					atomic.AddInt64(&failCnt, 1)
					mu.Lock()
					if len(errs) < 10 {
						errs = append(errs, fmt.Sprintf("type=%d idx=%d: %v", t.feedType, t.index, err))
					}
					mu.Unlock()
				} else {
					atomic.AddInt64(&okCnt, 1)
				}
				if n := atomic.AddInt64(&done, 1); n%20 == 0 || int(n) == total {
					fmt.Printf("进度 %d/%d 成功=%d 失败=%d 耗时=%s\n",
						n, total, atomic.LoadInt64(&okCnt), atomic.LoadInt64(&failCnt),
						time.Since(start).Truncate(time.Second))
				}
			}
		}()
	}

loop:
	for _, t := range tasks {
		select {
		case <-ctx.Done():
			break loop
		case ch <- t:
		}
	}
	close(ch)
	wg.Wait()

	fmt.Printf("完成：成功 %d 条，失败 %d 条，总耗时 %s\n",
		okCnt, failCnt, time.Since(start).Truncate(time.Second))
	if failCnt > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "  失败样例: %s\n", e)
		}
		return fmt.Errorf("存在 %d 条注入失败", failCnt)
	}
	return nil
}

// doTask 完成单条帖子的媒体上传与发布。
func (s *seeder) doTask(ctx context.Context, t task) error {
	rnd := rand.New(rand.NewSource(s.baseSeed + int64(t.feedType)*1_000_003 + int64(t.index)))
	if t.feedType == 2 {
		return s.publishVideo(ctx, t.index, rnd)
	}
	return s.publishImage(ctx, t.index, rnd)
}

// publishVideo 上传封面 + 复制视频对象后发布视频帖。
func (s *seeder) publishVideo(ctx context.Context, idx int, rnd *rand.Rand) error {
	coverKey := s.fileKey("cover", "png")
	cover, err := renderCard(idx, rnd, kindVideo)
	if err != nil {
		return err
	}
	if err := s.cos.put(ctx, coverKey, cover, "image/png"); err != nil {
		return fmt.Errorf("上传封面失败: %w", err)
	}

	videoKey := s.fileKey("video", "mp4")
	if err := s.cos.copy(ctx, videoKey, s.videoSrc); err != nil {
		return fmt.Errorf("复制视频对象失败: %w", err)
	}

	title, desc := videoContent(idx, rnd)
	_, err = s.pub.createFeed(ctx, 2, title, desc, []string{videoKey}, coverKey)
	return err
}

// publishImage 上传 1~N 张图片后发布图文帖，封面取首图。
func (s *seeder) publishImage(ctx context.Context, idx int, rnd *rand.Rand) error {
	n := 1 + rnd.Intn(s.maxImgs)
	keys := make([]string, 0, n)
	for i := 0; i < n; i++ {
		key := s.fileKey("image", "png")
		data, err := renderCard(idx*100+i, rnd, kindImage)
		if err != nil {
			return err
		}
		if err := s.cos.put(ctx, key, data, "image/png"); err != nil {
			return fmt.Errorf("上传图片失败: %w", err)
		}
		keys = append(keys, key)
	}

	title, desc := imageContent(idx, rnd)
	_, err := s.pub.createFeed(ctx, 1, title, desc, keys, keys[0])
	return err
}

// fileKey 生成与网关 buildFileKey 完全一致的对象键：{env}/{biz}/{uid}/{yyyyMMdd}/{id}.{ext}
func (s *seeder) fileKey(biz, ext string) string {
	return fmt.Sprintf("%s/%s/%d/%s/%s.%s",
		s.env, biz, s.uid, time.Now().Format("20060102"), s.node.Generate().String(), ext)
}

// withRetry 对可重试错误做指数退避重试。
func withRetry(ctx context.Context, attempts int, fn func() error) error {
	var err error
	for i := 0; i < attempts; i++ {
		if err = fn(); err == nil {
			return nil
		}
		if !retryable(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(i+1) * 500 * time.Millisecond):
		}
	}
	return err
}
