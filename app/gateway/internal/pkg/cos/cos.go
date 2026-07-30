// Package cos 封装腾讯云 COS 相关能力：STS 临时上传凭证签发、下载签名 URL 生成。
// 详见 docs/design/oss/00-overview.md。密钥仅来自配置（生产来自环境变量），禁止硬编码。
package cos

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	sts "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/sts/v20180813"
	cosSdk "github.com/tencentyun/cos-go-sdk-v5"

	"github.com/sponge-dad/feed/app/gateway/internal/config"
)

// Client 封装 COS 访问能力，被 Gateway ServiceContext 持有。
// 业务层只需调用 Issue/SignGet，所有「如何调腾讯云」收敛在此，便于测试与密钥集中管理。
type Client struct {
	conf      config.CosConf
	stsClient *sts.Client
	cosClient *cosSdk.Client
}

// New 根据配置构造 COS 客户端，初始化 STS 与签名两个底层客户端。
func New(conf config.CosConf) (*Client, error) {
	credential := common.NewCredential(conf.SecretId, conf.SecretKey)
	stsClient, err := sts.NewClient(credential, conf.Region, nil)
	if err != nil {
		return nil, fmt.Errorf("init sts client: %w", err)
	}

	base, err := url.Parse(conf.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse cos base url %q: %w", conf.BaseURL, err)
	}
	cosClient := cosSdk.NewClient(&cosSdk.BaseURL{BucketURL: base}, &http.Client{
		Transport: &cosSdk.AuthorizationTransport{
			SecretID:  conf.SecretId,
			SecretKey: conf.SecretKey,
		},
	})

	return &Client{conf: conf, stsClient: stsClient, cosClient: cosClient}, nil
}

// MustNew 同 New，但失败直接 panic，供 ServiceContext 与其他 Must* 风格保持一致。
func MustNew(conf config.CosConf) *Client {
	c, err := New(conf)
	if err != nil {
		panic(err)
	}
	return c
}

// Issue 申请限定到指定 file_key 前缀的 STS 临时上传凭证。
// 凭证仅允许 cos:PutObject 到该 key 前缀（即用户目录下的唯一对象），有效期取 conf.StsDuration。
func (c *Client) Issue(key string) (*Credential, error) {
	req := sts.NewGetFederationTokenRequest()
	req.Name = common.StringPtr("feed-upload")
	req.DurationSeconds = common.Uint64Ptr(uint64(c.stsDuration()))
	req.Policy = common.StringPtr(c.buildPutPolicy(key))

	resp, err := c.stsClient.GetFederationToken(req)
	if err != nil {
		return nil, fmt.Errorf("sts GetFederationToken: %w", err)
	}
	if resp.Response == nil || resp.Response.Credentials == nil {
		return nil, fmt.Errorf("sts GetFederationToken: empty credentials")
	}

	cred := resp.Response.Credentials
	expired := int64(0)
	if resp.Response.ExpiredTime != nil {
		expired = int64(*resp.Response.ExpiredTime)
	}
	return &Credential{
		TmpSecretID:  deref(cred.TmpSecretId),
		TmpSecretKey: deref(cred.TmpSecretKey),
		SessionToken: deref(cred.Token),
		ExpiredTime:  expired,
	}, nil
}

// SignGet 生成指定 key 的下载预签名 URL，dur 为有效期（秒）；dur<=0 时用 conf.SignDuration。
func (c *Client) SignGet(key string, dur int64) (string, error) {
	if dur <= 0 {
		dur = c.signDuration()
	}
	u, err := c.cosClient.Object.GetPresignedURL(
		context.Background(), http.MethodGet, key,
		c.conf.SecretId, c.conf.SecretKey,
		time.Duration(dur)*time.Second, nil,
	)
	if err != nil {
		return "", fmt.Errorf("cos GetPresignedURL: %w", err)
	}
	return u.String(), nil
}

// Credential 是 STS 临时凭证的扁平结构，便于直接映射给 UploadTokenResp。
type Credential struct {
	TmpSecretID  string
	TmpSecretKey string
	SessionToken string
	ExpiredTime  int64
}

// stsDuration 返回 STS 有效期（秒），未配置时使用默认 3600。
func (c *Client) stsDuration() int64 {
	if c.conf.StsDuration <= 0 {
		return 3600
	}
	return c.conf.StsDuration
}

// signDuration 返回下载签名 URL 有效期（秒），未配置时使用默认 600。
func (c *Client) signDuration() int64 {
	if c.conf.SignDuration <= 0 {
		return 600
	}
	return c.conf.SignDuration
}

// buildPutPolicy 构造限定到指定 key 前缀的 CAM 策略（仅允许 cos:PutObject）。
// resource 形如 qcs::cos:{region}:uid/{appid}:{bucket}/{key}*，其中 appid 取自桶名后缀。
func (c *Client) buildPutPolicy(key string) string {
	return fmt.Sprintf(policyTemplate,
		c.conf.Region, c.appid(), c.conf.Bucket, key)
}

const policyTemplate = `{
  "version": "2.0",
  "statement": [
    {
      "effect": "allow",
      "action": ["cos:PutObject"],
      "resource": ["qcs::cos:%s:uid/%s:%s/%s*"]
    }
  ]
}`

// appid 从桶名（{name}-{appid}）提取 APPID，用于 CAM 资源 ARN。
func (c *Client) appid() string {
	idx := strings.LastIndex(c.conf.Bucket, "-")
	if idx < 0 {
		return ""
	}
	return c.conf.Bucket[idx+1:]
}

// deref 安全解引用字符串指针，nil 时返回空串。
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
