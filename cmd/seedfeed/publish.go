// publish.go 通过网关 HTTP 接口发布帖子，保证注入数据与真实用户行为完全同链路。
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// publisher 持有登录态并调用网关发帖接口。
type publisher struct {
	base  string
	token string
	hc    *http.Client
}

func newPublisher(baseURL, token string) *publisher {
	return &publisher{
		base:  strings.TrimSuffix(baseURL, "/"),
		token: token,
		hc: &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        64,
				MaxIdleConnsPerHost: 64,
			},
		},
	}
}

// apiError 表示一次网关调用失败，区分传输层状态码与业务错误码以决定是否重试。
type apiError struct {
	status  int
	code    int
	message string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("gateway http=%d code=%d msg=%s", e.status, e.code, e.message)
}

// retryable 判断错误是否值得重试：业务参数/权限类错误不重试，网络与 5xx 重试。
func retryable(err error) bool {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae.status >= 500 || ae.status == http.StatusTooManyRequests
	}
	return true
}

// envelope 是网关统一响应体。
type envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type createFeedReq struct {
	FeedType    int32    `json:"feed_type"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	MediaUrls   []string `json:"media_urls"`
	CoverURL    string   `json:"cover_url"`
}

type createFeedData struct {
	Feed struct {
		ID int64 `json:"id"`
	} `json:"feed"`
}

// ping 校验网关可达且登录态有效。
func (p *publisher) ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.base+"/api/v1/feeds/timeline?tab=recommend&page_size=1", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	resp, err := p.hc.Do(req)
	if err != nil {
		return fmt.Errorf("网关不可达 %s: %w", p.base, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("网关拒绝登录态（http=%d），请确认 gateway.yaml 的 Auth.AccessSecret 与运行中的网关一致", resp.StatusCode)
	}
	return nil
}

// createFeed 发布一条帖子，返回帖子 ID。
func (p *publisher) createFeed(ctx context.Context, feedType int32, title, desc string, media []string, cover string) (int64, error) {
	body, err := json.Marshal(&createFeedReq{
		FeedType:    feedType,
		Title:       title,
		Description: desc,
		MediaUrls:   media,
		CoverURL:    cover,
	})
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.base+"/api/v1/feeds", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.token)

	resp, err := p.hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return 0, &apiError{status: resp.StatusCode, message: truncate(string(raw), 200)}
	}

	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return 0, fmt.Errorf("解析响应失败: %w, body=%s", err, truncate(string(raw), 200))
	}
	if env.Code != 0 {
		return 0, &apiError{status: resp.StatusCode, code: env.Code, message: env.Message}
	}

	var data createFeedData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return 0, fmt.Errorf("解析帖子数据失败: %w", err)
	}
	return data.Feed.ID, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
