// Command uptest 是 COS 临时密钥直传的冒烟测试：用服务端永久密钥签发 STS 临时凭证，
// 再用临时凭证把一张 1x1 测试 PNG 直传到桶，校验后删除，保持桶干净。
//
// 用法：
//   source scripts/cos-env.sh && go run ./app/gateway/internal/pkg/cos/uptest            # 上传+自清理验证
//   source scripts/cos-env.sh && COS_UPTEST_CLEANUP=1 go run ./app/gateway/internal/pkg/cos/uptest  # 清理 dev/_uptest/ 下所有对象
//
// 密钥仅来自环境变量 COS_SECRET_ID / COS_SECRET_KEY，禁止硬编码。
package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/url"
	"os"
	"time"

	cosSdk "github.com/tencentyun/cos-go-sdk-v5"

	"github.com/sponge-dad/feed/app/gateway/internal/config"
	"github.com/sponge-dad/feed/app/gateway/internal/pkg/cos"
)

// 桶配置（非密钥，与 gateway.yaml 保持一致）。
const (
	bucket  = "feed-1250000000-1317318750"
	region  = "ap-guangzhou"
	baseURL = "https://feed-1250000000-1317318750.cos.ap-guangzhou.myqcloud.com"
	envTag  = "dev"
)

func main() {
	secretID := os.Getenv("COS_SECRET_ID")
	secretKey := os.Getenv("COS_SECRET_KEY")
	if secretID == "" || secretKey == "" {
		fmt.Println("ERR: 环境变量 COS_SECRET_ID / COS_SECRET_KEY 未设置，请先 `source scripts/cos-env.sh`")
		os.Exit(1)
	}

	bucketURL, err := url.Parse(baseURL)
	if err != nil {
		fmt.Printf("ERR: 解析 BaseURL 失败: %v\n", err)
		os.Exit(1)
	}
	permClient := cosSdk.NewClient(&cosSdk.BaseURL{BucketURL: bucketURL}, &http.Client{
		Transport: &cosSdk.AuthorizationTransport{
			SecretID:  secretID,
			SecretKey: secretKey,
		},
	})

	if os.Getenv("COS_UPTEST_CLEANUP") == "1" {
		runCleanup(permClient)
		return
	}
	runUploadTest(secretID, secretKey, permClient)
}

// runUploadTest 验证「临时密钥直传」链路：签发 STS -> 临时凭证 PUT -> 校验 -> 删除。
func runUploadTest(secretID, secretKey string, permClient *cosSdk.Client) {
	conf := config.CosConf{
		Bucket:       bucket,
		Region:       region,
		SecretId:     secretID,
		SecretKey:    secretKey,
		Env:          envTag,
		StsDuration:  3600,
		SignDuration: 600,
		BaseURL:      baseURL,
	}
	client := cos.MustNew(conf)

	// 1) 生成唯一测试 key，申请限定到该 key 前缀的 STS 临时凭证
	key := fmt.Sprintf("%s/_uptest/%d.png", envTag, time.Now().UnixNano())
	cred, err := client.Issue(key)
	if err != nil {
		fmt.Printf("ERR: 签发 STS 临时凭证失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("OK: 已签发 STS 临时凭证 (TmpSecretID=%s..., 到期=%d)\n",
		truncate(cred.TmpSecretID), cred.ExpiredTime)

	// 2) 生成 1x1 红色 PNG
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		fmt.Printf("ERR: 生成测试图失败: %v\n", err)
		os.Exit(1)
	}

	// 3) 用临时凭证直传（模拟客户端行为）
	tmpClient := cosSdk.NewClient(&cosSdk.BaseURL{BucketURL: permClient.BaseURL.BucketURL}, &http.Client{
		Transport: &cosSdk.AuthorizationTransport{
			SecretID:     cred.TmpSecretID,
			SecretKey:    cred.TmpSecretKey,
			SessionToken: cred.SessionToken,
		},
	})
	content := buf.Bytes()
	if _, err := tmpClient.Object.Put(context.Background(), key, bytes.NewReader(content), nil); err != nil {
		fmt.Printf("ERR: 临时密钥直传失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("OK: 已用临时密钥直传 %s (size=%d bytes)\n", key, len(content))

	// 4) 校验与清理：用永久密钥尝试 HEAD/DELETE（需子账号具备相应权限）。
	exist, herr := permClient.Object.IsExist(context.Background(), key)
	if herr != nil {
		fmt.Printf("INFO: 永久密钥 HEAD 校验被拒: %v\n", herr)
	} else if exist {
		fmt.Println("OK: 永久密钥校验对象存在成功")
	}

	if _, derr := permClient.Object.Delete(context.Background(), key); derr != nil {
		fmt.Printf("WARN: 删除失败（请确认子账号有 cos:DeleteObject 权限）: %v\n", derr)
	} else {
		fmt.Printf("OK: 已清理测试对象 %s\n", key)
	}
	fmt.Println("DONE: COS 临时密钥直传链路可用")
}

// runCleanup 列出并删除 dev/_uptest/ 前缀下的所有对象（用于清理遗留测试文件）。
func runCleanup(permClient *cosSdk.Client) {
	prefix := envTag + "/_uptest/"
	opt := &cosSdk.BucketGetOptions{Prefix: prefix, MaxKeys: 1000}
	resp, _, err := permClient.Bucket.Get(context.Background(), opt)
	if err != nil {
		fmt.Printf("ERR: 列举 %s 失败: %v\n", prefix, err)
		os.Exit(1)
	}
	if len(resp.Contents) == 0 {
		fmt.Printf("OK: %s 下无对象，无需清理\n", prefix)
		return
	}
	for _, obj := range resp.Contents {
		if _, derr := permClient.Object.Delete(context.Background(), obj.Key); derr != nil {
			fmt.Printf("WARN: 删除 %s 失败: %v\n", obj.Key, derr)
			continue
		}
		fmt.Printf("OK: 已删除 %s\n", obj.Key)
	}
	fmt.Printf("DONE: 共清理 %d 个测试对象\n", len(resp.Contents))
}

func truncate(s string) string {
	if len(s) <= 6 {
		return s
	}
	return s[:6]
}
