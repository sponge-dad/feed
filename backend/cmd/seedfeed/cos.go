// cos.go 封装工具所需的最小 COS 能力：上传、服务端复制、存在性判断与源视频探测。
package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	cossdk "github.com/tencentyun/cos-go-sdk-v5"
)

// cosClient 是带永久密钥的 COS 客户端（仅供本地/开发环境的数据注入使用）。
type cosClient struct {
	cli  *cossdk.Client
	host string
}

// newCosClient 基于网关同一份桶配置构造客户端。
func newCosClient(c cosConf) (*cosClient, error) {
	u, err := url.Parse(strings.TrimSuffix(c.BaseURL, "/"))
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("非法的 Cos.BaseURL: %s", c.BaseURL)
	}
	cli := cossdk.NewClient(&cossdk.BaseURL{BucketURL: u}, &http.Client{
		Timeout: 120 * time.Second,
		Transport: &cossdk.AuthorizationTransport{
			SecretID:  c.SecretID,
			SecretKey: c.SecretKey,
		},
	})
	return &cosClient{cli: cli, host: u.Host}, nil
}

// put 上传对象内容。
func (c *cosClient) put(ctx context.Context, key string, data []byte, contentType string) error {
	opt := &cossdk.ObjectPutOptions{
		ObjectPutHeaderOptions: &cossdk.ObjectPutHeaderOptions{ContentType: contentType},
	}
	_, err := c.cli.Object.Put(ctx, key, bytes.NewReader(data), opt)
	return err
}

// copy 走 COS 服务端复制生成新对象，不消耗本地带宽。
func (c *cosClient) copy(ctx context.Context, destKey, srcKey string) error {
	_, _, err := c.cli.Object.Copy(ctx, destKey, c.host+"/"+srcKey, nil)
	return err
}

// exists 判断对象是否存在。
func (c *cosClient) exists(ctx context.Context, key string) (bool, error) {
	return c.cli.Object.IsExist(ctx, key)
}

// videoExts 允许作为视频源的后缀，与网关上传白名单保持一致。
var videoExts = []string{".mp4", ".mov", ".webm"}

// detectVideoSource 自动挑选一个可用的视频源对象：优先该用户目录，其次全环境视频目录，取体积最大的一个。
func (c *cosClient) detectVideoSource(ctx context.Context, env string, uid int64) (string, error) {
	prefixes := []string{
		fmt.Sprintf("%s/video/%d/", env, uid),
		fmt.Sprintf("%s/video/", env),
	}
	for _, prefix := range prefixes {
		res, _, err := c.cli.Bucket.Get(ctx, &cossdk.BucketGetOptions{Prefix: prefix, MaxKeys: 1000})
		if err != nil {
			return "", fmt.Errorf("列举 COS 对象失败 prefix=%s: %w", prefix, err)
		}
		best, bestSize := "", int64(-1)
		for _, o := range res.Contents {
			if !hasAnySuffix(strings.ToLower(o.Key), videoExts) {
				continue
			}
			if o.Size > bestSize {
				best, bestSize = o.Key, o.Size
			}
		}
		if best != "" {
			return best, nil
		}
	}
	return "", fmt.Errorf("未找到可用的视频源对象，请先上传一个视频，或用 -video-src 指定 COS key")
}

func hasAnySuffix(s string, suffixes []string) bool {
	for _, suf := range suffixes {
		if strings.HasSuffix(s, suf) {
			return true
		}
	}
	return false
}
